//go:build linux

package executionruntime

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

// TestRealStoppedOpenCodeTurnKeepsTheSession is the v2 evidence for circus#1404: with the pinned
// Node's turn process fence (desktop#807), stopping a running OpenCode turn that left a tool job
// in the background kills the job before the stop settles, keeps the Agent session, and the
// conversation's next message runs in that same session and is answered.
func TestRealStoppedOpenCodeTurnKeepsTheSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	hold := make(chan struct{})
	f, driverRoot, project := backgroundJobRuntime(t, ctx, backgroundJobFixture{loop: true, hold: hold})
	// Release a held answer before the runtime's own cleanup waits for its turn.
	t.Cleanup(func() { close(hold) })

	caller := execution.Caller{Kind: execution.CallerChat, ID: "stop-conversation", AttemptID: "first"}
	first, err := f.service.Start(ctx, f.scope, execution.StartRequest{Caller: caller, Target: execution.Target{ProjectID: project.ID}, ExecutorID: "opencode:fixture", Prompt: execution.TextPrompt("Start the requested background job with the shell tool.")})
	if err != nil {
		t.Fatal(err)
	}
	pidFile := awaitCheckoutFile(t, ctx, driverRoot, "ora-v2-job.pid")
	pid := readJobPID(t, pidFile)
	t.Cleanup(func() {
		if jobAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	ticks := filepath.Join(filepath.Dir(pidFile), "ora-v2-late.txt")
	// The job must be running and writing while the turn is still held, or the stop proves nothing.
	awaitGrowth(t, ctx, ticks)
	if !jobAlive(pid) {
		t.Fatalf("background job %d was not running when the turn was stopped", pid)
	}
	before, err := f.service.ReadSession(ctx, f.scope, first.SessionID)
	if err != nil {
		t.Fatal(err)
	}

	stopAt := time.Now()
	stopped, err := f.service.Cancel(ctx, f.scope, first)
	settle := time.Since(stopAt)
	if err != nil || stopped.Status != execution.StatusCancelled || stopped.AgentLost {
		t.Fatalf("stop of the running OpenCode turn = %+v err=%v after %v", stopped, err, settle)
	}
	if jobAlive(pid) {
		t.Fatalf("background job %d outlived the settled stop (%v)", pid, settle)
	}
	session, err := f.service.ReadSession(ctx, f.scope, first.SessionID)
	if err != nil || session.EndedAt != nil {
		t.Fatalf("session after the stop = %+v err=%v: the stop did not keep it", session, err)
	}
	// A job that escaped the kill would keep appending every 200 ms.
	size := fileSize(t, ticks)
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(2 * time.Second):
	}
	if grown := fileSize(t, ticks); grown != size {
		t.Fatalf("%s grew from %d to %d bytes after the stop settled", ticks, size, grown)
	}

	next := caller
	next.AttemptID = "second"
	second, err := f.service.Append(ctx, f.scope, execution.AppendRequest{SessionID: first.SessionID, Caller: next, Prompt: execution.TextPrompt("Reply with the fixture answer.")})
	if err != nil || second.SessionID != first.SessionID {
		t.Fatalf("Append after the kept stop = %+v err=%v", second, err)
	}
	result, _ := observeTurn(t, f, second, 0)
	requireRealFixtureResult(t, result)
	// The same Agent answered in the same checkout: its tool wrote the marker beside the job's
	// files, and the session's workspace did not change.
	if marker := findCheckoutFile(t, driverRoot, "ora-v2-result.txt"); filepath.Dir(marker) != filepath.Dir(pidFile) {
		t.Fatalf("next turn wrote %s, not in the stopped turn's checkout %s", marker, filepath.Dir(pidFile))
	}
	after, err := f.service.ReadSession(ctx, f.scope, first.SessionID)
	if err != nil || after.EndedAt != nil || after.WorkspaceID != before.WorkspaceID {
		t.Fatalf("session after the next turn = %+v err=%v, before the stop %+v", after, err, before)
	}
	t.Logf("stop settled cancelled in %v with the session kept; job %d dead, %s stayed at %d bytes; next turn %s answered", settle.Round(time.Millisecond), pid, filepath.Base(ticks), size, second.TurnID)
}

// awaitGrowth waits until the file at path exists and has grown at least once.
func awaitGrowth(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	first := int64(-1)
	for {
		if info, err := os.Stat(path); err == nil {
			if first >= 0 && info.Size() > first {
				return
			}
			if first < 0 {
				first = info.Size()
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s never grew: %v", path, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// fileSize returns the size of the file at path, which must exist.
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
