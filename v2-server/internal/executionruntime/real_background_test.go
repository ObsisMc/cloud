//go:build linux

package executionruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/agentruntime"
	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
	"github.com/wanglongan587/cloud/v2-server/internal/projectrepos"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// backgroundJobDelay is how long the Agent's background job waits before it writes the late
// file; the test watches the checkout for a little longer than this after End.
const backgroundJobDelay = 5 * time.Second

// TestRealEndKillsTheAgentsBackgroundJob is the v2 evidence that the pinned Node (desktop#802)
// kills a tool job the Agent left running in the background before it reports the session
// stopped, so nothing writes the checkout after End returns: not the reclaim of a deleted
// conversation, nor a later node in the same run directory, can race it.
func TestRealEndKillsTheAgentsBackgroundJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	f, driverRoot, project := backgroundJobRuntime(t, ctx, backgroundJobFixture{})

	ref, err := f.service.Start(ctx, f.scope, execution.StartRequest{Caller: execution.Caller{Kind: execution.CallerChat, ID: "background-conversation", AttemptID: "first"}, Target: execution.Target{ProjectID: project.ID}, ExecutorID: "opencode:fixture", Prompt: execution.TextPrompt("Start the requested background job with the shell tool.")})
	if err != nil {
		t.Fatal(err)
	}
	result, _ := observeTurn(t, f, ref, 0)
	requireRealFixtureResult(t, result)
	// The turn is over and the session kept: the job must still be running, or End proves nothing.
	pidFile := findCheckoutFile(t, driverRoot, "ora-v2-job.pid")
	pid := readJobPID(t, pidFile)
	if !jobAlive(pid) {
		t.Fatalf("background job %d was not running when the session ended", pid)
	}
	if err := f.service.End(ctx, f.scope, ref.SessionID); err != nil {
		t.Fatal(err)
	}
	if jobAlive(pid) {
		t.Fatalf("background job %d outlived End: Node reported the session stopped while its job ran", pid)
	}
	// Watch past the job's own deadline, so a job that escaped the kill shows up as its write.
	late := filepath.Join(filepath.Dir(pidFile), "ora-v2-late.txt")
	deadline := time.Now().Add(backgroundJobDelay + 2*time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(late); err == nil {
			t.Fatalf("background job wrote %s after End", late)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// backgroundJobRuntime starts the execution service on the real Node with model standing in for
// the Agent's model, and seeds the Demo project; everything it starts is stopped at cleanup.
func backgroundJobRuntime(t *testing.T, ctx context.Context, model nodeservices.Provider) (*runtimeFixture, string, store.Project) {
	t.Helper()
	assets := os.Getenv("V2_NODE_ASSETS_DIR")
	if assets == "" {
		t.Skip("set V2_NODE_ASSETS_DIR for real Node/Agent background job evidence")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, "vb-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	f := runtimeDatabase(t, model)
	if err := f.service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	f.ctx = ctx
	repositories, err := projectrepos.New(f.db, projectrepos.Options{Root: filepath.Join(root, "r")})
	if err != nil {
		t.Fatal(err)
	}
	project, err := repositories.SeedDemo(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.CommandContext(ctx, "git", "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	driverRoot := filepath.Join(root, "a")
	t.Cleanup(func() {
		if t.Failed() {
			logRealDiagnostics(t, driverRoot)
		}
	})
	driver, err := agentruntime.New(agentruntime.Options{
		Root: driverRoot, AssetsDirectory: assets,
		GitBackend: filepath.Join(strings.TrimSpace(string(gitPath)), "git-http-backend"),
		Bindings:   map[string]nodeservices.Binding{"fixture": f.models["fixture"].Binding},
		ResolveProject: func(ctx context.Context, scope execution.Scope, id string) (agentruntime.Project, error) {
			project, err := f.db.GetProject(ctx, scope, id)
			return agentruntime.Project{MirrorPath: project.MirrorPath, Commit: project.Commit, SnapshotRef: project.SnapshotRef}, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = New(ctx, Config{Store: f.db, Models: f.models, Agent: driver})
	if err != nil {
		driver.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := f.service.Close(cleanup); err != nil {
			t.Error(err)
		}
		if err := driver.Close(cleanup); err != nil {
			t.Error(err)
		}
	})

	return f, driverRoot, project
}

// backgroundJobFixture asks the real Agent's shell tool to leave a job running in the
// background that records its PID and writes a late file after backgroundJobDelay; once the
// tool returns it answers like FixtureProvider. Only a turn whose user message asks for a
// background job gets the tool call; any other is answered like FixtureProvider at once, also in
// a conversation an earlier turn of which asked.
type backgroundJobFixture struct {
	// delay, when set, replaces backgroundJobDelay.
	delay time.Duration
	// loop, when set, makes the job append a line to the late file every 200 ms until it is
	// killed, instead of writing it once after delay, so a job that survives shows as growth.
	loop bool
	// hold, when set, keeps the answer after the tool result pending until hold closes or the
	// request ends, so the turn is still running while the job does.
	hold <-chan struct{}
	// answered, when set, is called before answering a conversation that asks for no job.
	answered func()
}

func (fixture backgroundJobFixture) Complete(w http.ResponseWriter, r *http.Request, model nodeservices.Model) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	tool := ""
	for _, advertised := range req.Tools {
		if advertised.Function.Name == "bash" {
			tool = advertised.Function.Name
		}
	}
	// The latest user message is the turn's own prompt; earlier ones belong to finished turns.
	asked := false
	for _, message := range req.Messages {
		if message.Role == "user" {
			asked = bytes.Contains(message.Content, []byte("background job"))
		}
	}
	toolReturned := len(req.Messages) != 0 && req.Messages[len(req.Messages)-1].Role == "tool"
	if asked && toolReturned && fixture.hold != nil {
		select {
		case <-fixture.hold:
		case <-r.Context().Done():
			return
		}
	}
	if !asked && fixture.answered != nil {
		fixture.answered()
	}
	if tool == "" || !asked || toolReturned {
		r.Body = io.NopCloser(bytes.NewReader(body))
		nodeservices.FixtureProvider{}.Complete(w, r, model)
		return
	}
	delay := fixture.delay
	if delay == 0 {
		delay = backgroundJobDelay
	}
	// sh -c makes $$ the job's own PID. Its output goes nowhere, so the tool returns at once
	// and leaves the job behind, as an Agent starting a development server would.
	command := "sh -c 'echo $$ > ora-v2-job.pid; sleep " + strconv.Itoa(int(delay/time.Second)) + "; echo late > ora-v2-late.txt' >/dev/null 2>&1 &"
	if fixture.loop {
		command = "sh -c 'echo $$ > ora-v2-job.pid; while :; do echo tick >> ora-v2-late.txt; sleep 0.2; done' >/dev/null 2>&1 &"
	}
	args, _ := json.Marshal(map[string]string{"command": command, "description": "Start a background job that writes a file later"})
	message := map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"index": 0, "id": "call_v2_background", "type": "function", "function": map[string]string{"name": tool, "arguments": string(args)}}}}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	for _, choice := range []any{map[string]any{"index": 0, "delta": message, "finish_reason": nil}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}} {
		data, _ := json.Marshal(map[string]any{"id": "v2-background", "object": "chat.completion.chunk", "model": model.ID, "choices": []any{choice}})
		_, _ = w.Write(append(append([]byte("data: "), data...), "\n\n"...))
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

// findCheckoutFile returns the one file named name under root, outside Git directories.
func findCheckoutFile(t *testing.T, root, name string) string {
	t.Helper()
	found := checkoutFiles(t, root, name)
	if len(found) != 1 {
		t.Fatalf("found %d files named %s, want 1: %v", len(found), name, found)
	}
	return found[0]
}

// checkoutFiles lists the files named name under root, outside Git directories. Entries that
// vanish during the walk are skipped: Node may be writing the tree.
func checkoutFiles(t *testing.T, root, name string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && entry.Name() == name {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// readJobPID waits briefly for the job to record its PID: the tool may return before the
// detached shell has run its first command.
func readJobPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && parseErr == nil && pid > 0 {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("job PID file %s = %q, err=%v", path, data, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// jobAlive reports whether pid is still the background job and has not exited: a zombie is
// already dead, and a reused PID no longer runs the job's command line.
func jobAlive(pid int) bool {
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	cmdline, err := os.ReadFile(filepath.Join(proc, "cmdline"))
	if err != nil || !bytes.Contains(cmdline, []byte("ora-v2-late.txt")) {
		return false
	}
	stat, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return false
	}
	end := bytes.LastIndexByte(stat, ')')
	return end >= 0 && len(stat) > end+2 && stat[end+2] != 'Z' && stat[end+2] != 'X'
}
