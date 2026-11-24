//go:build linux

package workflowhost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/agentruntime"
	"github.com/wanglongan587/cloud/v2-server/internal/completiontool"
	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/executionruntime"
	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
	"github.com/wanglongan587/cloud/v2-server/internal/projectrepos"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
)

var attemptTokenInPrompt = regexp.MustCompile(`attemptToken unchanged: ([0-9a-f]{32})`)

// reportingModel calls the completion tool when the Agent advertises it, with the latest token in
// the conversation, and then repeats the tool's answer.
type reportingModel struct {
	mu     sync.Mutex
	tokens []string
}

func (m *reportingModel) Complete(w http.ResponseWriter, r *http.Request, model nodeservices.Model) {
	var req struct {
		Stream   bool              `json:"stream"`
		Messages []json.RawMessage `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		w.WriteHeader(400)
		return
	}
	tool, token := "", ""
	for _, candidate := range req.Tools {
		if strings.Contains(candidate.Function.Name, completiontool.ToolName) {
			tool = candidate.Function.Name
		}
	}
	for _, message := range req.Messages {
		for _, found := range attemptTokenInPrompt.FindAllStringSubmatch(string(message), -1) {
			token = found[1]
		}
	}
	last := string(req.Messages[len(req.Messages)-1])
	message, finish := map[string]any{"role": "assistant", "content": "NO_TOOL"}, "stop"
	switch {
	case strings.Contains(last, `"role":"tool"`):
		message["content"] = "TOOL_REFUSED"
		if strings.Contains(last, "accepted:") {
			message["content"] = "REPORTED " + token
		}
	case tool != "" && token != "":
		m.mu.Lock()
		m.tokens = append(m.tokens, token)
		m.mu.Unlock()
		args, _ := json.Marshal(map[string]string{"attemptToken": token})
		message["content"] = nil
		message["tool_calls"] = []any{map[string]any{"index": 0, "id": "call_" + token, "type": "function", "function": map[string]string{"name": tool, "arguments": string(args)}}}
		finish = "tool_calls"
	}
	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "report", "object": "chat.completion", "model": model.ID, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, choice := range []any{map[string]any{"index": 0, "delta": message, "finish_reason": nil}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
		data, _ := json.Marshal(map[string]any{"id": "report", "object": "chat.completion.chunk", "model": model.ID, "choices": []any{choice}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// The pinned Node and a real OpenCode: the Agent is handed the tool through Node, calls it, and the
// node waits for that report's confirmation; the next turn reports with its own token; and no token
// or endpoint credential reaches the stored conversation or Node's event journal.
func TestRealAgentReportsCompletionThroughNode(t *testing.T) {
	assets := os.Getenv("V2_NODE_ASSETS_DIR")
	if assets == "" {
		t.Skip("set V2_NODE_ASSETS_DIR for real Node and OpenCode")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, "vc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	db := storetest.Open(t)
	user, tenant := storetest.Login(t, db, "agent-completion")
	scope := execution.Scope{TenantID: tenant, UserID: user}
	repositories, err := projectrepos.New(db, projectrepos.Options{Root: filepath.Join(root, "r")})
	if err != nil {
		t.Fatal(err)
	}
	project, err := repositories.SeedDemo(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	models := executionruntime.WithFixtureModels(nil)
	model := models["fixture"]
	provider := &reportingModel{}
	model.Binding.Provider = provider
	models["fixture"] = model
	gitPath, err := exec.CommandContext(ctx, "git", "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	nodes := filepath.Join(root, "n")
	driver, err := agentruntime.New(agentruntime.Options{
		Root: nodes, AssetsDirectory: assets, GitBackend: filepath.Join(strings.TrimSpace(string(gitPath)), "git-http-backend"),
		Bindings: map[string]nodeservices.Binding{"fixture": model.Binding},
		ResolveProject: func(c context.Context, s execution.Scope, id string) (agentruntime.Project, error) {
			p, e := db.GetProject(c, s, id)
			return agentruntime.Project{MirrorPath: p.MirrorPath, Commit: p.Commit, SnapshotRef: p.SnapshotRef}, e
		},
		RecordWorkspace: func(c context.Context, id string, at agentruntime.Checkout) error {
			return db.RecordWorkspace(c, id, store.WorkspaceLocation{Path: at.Path, BaseCommit: at.BaseCommit, CloneExecution: at.CloneExecution})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := driver.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	service, err := executionruntime.New(ctx, executionruntime.Config{Store: db, Models: models, Agent: driver})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	tools := completiontool.NewRegistry(db.WorkflowNodeTurnLive)
	served, err := completiontool.Serve(tools)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(served.Close)
	host, err := NewHost(Config{Store: db, Execution: service, Catalog: service, CompletionTool: tools, CheckHostTools: service.CheckHostTools})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	f := &fixture{s: db, host: host, scope: server.WorkflowRunScope{TenantID: tenant, UserID: user}, project: project.ID}
	settled := func(runID string, turns int) (store.WorkflowNodeRun, map[string]any) {
		t.Helper()
		for {
			rows, err := db.WorkflowEngineNodeRuns(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			a := node(rows, "a")
			if a.Status == store.WorkflowStatusFailed {
				t.Fatalf("A failed: %+v", a)
			}
			if a.Status == store.WorkflowStatusPending && interactivePayload(t, a)["turns"] == float64(turns) {
				return a, interactivePayload(t, a)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	runID := f.run(t, agentChain, str("report"))
	if _, err := host.StartRun(ctx, f.scope, runID); err != nil {
		t.Fatal(err)
	}
	row, p := settled(runID, 1)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnUnconfirmed, blockerConfirmationPending}) || p["answer"] != "REPORTED [redacted]" {
		t.Fatalf("first turn = %v", p)
	}
	if _, err := host.SendNodeTurn(ctx, f.scope, runID, "a", "again"); err != nil {
		t.Fatal(err)
	}
	_, p = settled(runID, 2)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerConfirmationPending}) {
		t.Fatalf("second turn = %v", p)
	}
	if _, err := host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmSignalID: signalOf(p)}); err != nil {
		t.Fatal(err)
	}
	history, err := db.LatestHistory(ctx, *row.SessionID)
	if err != nil || len(history) == 0 {
		t.Fatalf("history = %d records: %v", len(history), err)
	}
	shown, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shown), completiontool.ToolName) || strings.Contains(string(shown), "workflow_completion>") {
		t.Fatalf("history does not show the call, or shows the block: %s", shown)
	}
	journals, err := filepath.Glob(filepath.Join(nodes, "*", "node-events.jsonl*"))
	if err != nil || len(journals) == 0 {
		t.Fatalf("journals = %v %v", journals, err)
	}
	stored := map[string]string{"history": string(shown)}
	for _, path := range journals {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		stored[path] = string(data)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.tokens) != 2 || provider.tokens[0] == provider.tokens[1] {
		t.Fatalf("tokens = %d, want one per turn", len(provider.tokens))
	}
	for name, text := range stored {
		for _, token := range provider.tokens {
			if strings.Contains(text, token) {
				t.Errorf("%s holds a token", name)
			}
		}
	}
}
