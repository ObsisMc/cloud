package executionruntime

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
)

// heldFixture starts held workflow sessions on an Agent that refuses every turn, which leaves a
// held session open, and confirms every stop.
func heldFixture(t *testing.T) (*runtimeFixture, *failureAgent, execution.StartRequest) {
	t.Helper()
	f := runtimeDatabase(t, nil)
	if err := f.service.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	refused := execution.Result{Status: execution.StatusFailed, StopReason: "refusal", FailureCode: "agent_turn_incomplete", Answer: "no"}
	a := &failureAgent{result: refused, endEntered: make(chan string, 8), allowEnd: make(chan struct{})}
	a.release()
	var err error
	if f.service, err = New(f.ctx, Config{Store: f.db, Models: f.models, Agent: a}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateProject(f.ctx, f.scope, store.Project{ID: "project", Name: "Demo", RepositoryURL: "https://example.com/demo.git", Commit: "fixed", MirrorPath: "/fake/unused"}); err != nil {
		t.Fatal(err)
	}
	return f, a, execution.StartRequest{Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: "run", NodeID: "draft", AttemptID: "a1"},
		Target: execution.Target{ProjectID: "project"}, ExecutorID: "opencode:fixture", Prompt: execution.TextPrompt("run"), Lifetime: execution.LifetimeHeld}
}

func isHeld(f *runtimeFixture, sid string) bool {
	f.service.mu.Lock()
	defer f.service.mu.Unlock()
	return f.service.held[sid]
}

// waitNotHeld returns once the session is dropped from the held list. conclude writes the
// database's ended marker before it clears the in-memory list, so a turn that read the ended
// marker may still briefly observe the session held; polling treats that clear as the
// eventually-consistent state it is.
func waitNotHeld(t *testing.T, f *runtimeFixture, sid string) {
	t.Helper()
	for {
		f.service.mu.Lock()
		held := f.service.held[sid]
		f.service.mu.Unlock()
		if !held {
			return
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("a session ended in the background is still listed as held")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// A held session outlives a turn its Agent ended without completing, but not a result the
// runtime could not store: it takes no further turn and its Agent is stopped like any other
// session's. The result written once the database returns tells the caller so, and the session
// ends without anyone calling End.
func TestRuntimeHeldSessionEndsWhenItsResultCannotBeStored(t *testing.T) {
	f, a, req := heldFixture(t)
	flaky := brokenResultWrites(t, f, 0)

	// A refused turn whose result is stored leaves the session open and its Agent running.
	first, err := f.service.Start(f.ctx, f.scope, req)
	if err != nil {
		t.Fatal(err)
	}
	if result, _ := observeTurn(t, f, first, 0); result.AgentLost {
		t.Fatalf("a stored refusal was recorded as a lost Agent: %+v", result)
	}
	turn := func(attempt string) (execution.Ref, error) {
		caller := req.Caller
		caller.AttemptID = attempt
		return f.service.Append(f.ctx, f.scope, execution.AppendRequest{SessionID: first.SessionID, Caller: caller, Prompt: execution.TextPrompt("again")})
	}
	// The next refused turn cannot be stored.
	flaky.failures.Store(-1)
	second, err := turn("a2")
	if err != nil {
		t.Fatalf("held session refused a turn after a stored refusal: %v", err)
	}
	if len(a.endEntered) != 0 {
		t.Fatal("a stored refusal stopped the held session's Agent")
	}
	waitUnsaved(t, f, first.SessionID)
	select {
	case sid := <-a.endEntered:
		if sid != first.SessionID {
			t.Fatalf("stopped wrong Agent: %s", sid)
		}
	default:
		t.Fatal("a held session whose result was not stored kept its physical Agent")
	}
	if _, err := turn("a3"); !errors.Is(err, execution.ErrSessionEnded) {
		t.Fatalf("held session with an unstored result accepted a turn: %v", err)
	}

	// The database returns. The caller reads AgentLost to learn that no turn can follow.
	flaky.failures.Store(0)
	if result, _ := observeTurn(t, f, second, 0); result.Status != execution.StatusFailed || result.StopReason != "refusal" || !result.AgentLost {
		t.Fatalf("recovered result of a held session that was ended: %+v", result)
	}
	waitSessionEnded(t, f, first.SessionID)
	waitNotHeld(t, f, first.SessionID)
}

func TestRuntimeEndForgetsAHeldSession(t *testing.T) {
	f, _, req := heldFixture(t)
	ref, err := f.service.Start(f.ctx, f.scope, req)
	if err != nil {
		t.Fatal(err)
	}
	observeTurn(t, f, ref, 0)
	if !isHeld(f, ref.SessionID) {
		t.Fatal("a refused turn dropped its session from the held list")
	}
	if err := f.service.End(f.ctx, f.scope, ref.SessionID); err != nil {
		t.Fatal(err)
	}
	if isHeld(f, ref.SessionID) {
		t.Fatal("an ended session is still listed as held")
	}
}

// hostedAgent is a driver that knows why executions ended whose end it has not written yet.
type hostedAgent struct {
	*failureAgent
	ends map[string]string
}

func (a hostedAgent) HostExecutionEnds(string) map[string]string { return a.ends }
func (a hostedAgent) HostExecutionEnding(sid, id string) bool {
	_, ok := a.ends[id]
	return ok && sid == "s"
}

// What is ending is what the driver says is; without a driver that knows, nothing is.
func TestRuntimeAsksItsDriverWhatIsEnding(t *testing.T) {
	a := &failureAgent{}
	s := &Service{}
	for name, agent := range map[string]AgentDriver{"no driver": nil, "a driver that does not know": a} {
		s.agent = agent
		if s.HostExecutionEnding("s", "x-lost") {
			t.Fatalf("%s: ending", name)
		}
	}
	s.agent = hostedAgent{a, map[string]string{"x-lost": "agent_failed"}}
	if !s.HostExecutionEnding("s", "x-lost") || s.HostExecutionEnding("s", "x") || s.HostExecutionEnding("t", "x-lost") {
		t.Fatal("not what the driver says")
	}
}

// A session ended while its driver still has to write why its execution ended keeps that reason, and
// the driver's later write changes nothing.
func TestRuntimeEndsASessionWithItsDriversReason(t *testing.T) {
	f, a, req := heldFixture(t)
	f.service.agent = hostedAgent{a, map[string]string{"x-lost": "agent_failed"}}
	ref, err := f.service.Start(f.ctx, f.scope, req)
	if err != nil {
		t.Fatal(err)
	}
	observeTurn(t, f, ref, 0)
	if err := f.db.RecordHostExecution(f.ctx, "x-lost", ref.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.End(f.ctx, f.scope, ref.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.EndHostExecution(f.ctx, "x-lost", "agent_failed"); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.ConnectConfig(f.ctx, f.config.ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	executions := storetest.HostExecutions(t, conn, ref.SessionID)
	if len(executions) != 1 || executions[0].EndedAt == nil || executions[0].Reason != "agent_failed" {
		t.Fatalf("executions = %+v", executions)
	}
}
