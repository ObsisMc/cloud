package executionruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// recoveryAgent records what each turn was run with and answers from a script.
type recoveryAgent struct {
	mu      sync.Mutex
	runs    []execution.Session
	prompts []string
	// results, when not empty, answer the next turns in order instead of a completed one; errs,
	// when set at the same position, is returned with the result without publishing it.
	results []execution.Result
	errs    []error
	ended   []string
	// endErrs, when not empty, answer the next Ends in order.
	endErrs []error
	// reattached is the checkout each workspace was reattached to: clone execution and path.
	reattached map[string][2]string
}

func (a *recoveryAgent) Run(ctx context.Context, _ execution.Scope, session execution.Session, turn execution.Turn, emit func(context.Context, execution.Event) error) (execution.Result, error) {
	text, _ := turn.Prompt.Text()
	a.mu.Lock()
	a.runs = append(a.runs, session)
	a.prompts = append(a.prompts, text)
	result := execution.Result{Status: execution.StatusCompleted, Answer: "答复：" + text, AnswerComplete: true, StopReason: "end_turn"}
	var err error
	if len(a.results) > 0 {
		result, a.results = a.results[0], a.results[1:]
		if len(a.errs) > 0 {
			err, a.errs = a.errs[0], a.errs[1:]
		}
		if err == nil && result.NotStarted {
			err = errors.New(result.FailureCode)
		}
	}
	a.mu.Unlock()
	if err != nil {
		return result, err
	}
	return result, emit(ctx, execution.Event{Kind: execution.EventTurnEnded, Result: &result})
}

func (a *recoveryAgent) End(_ context.Context, sid string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ended = append(a.ended, sid)
	if len(a.endErrs) > 0 {
		err := a.endErrs[0]
		a.endErrs = a.endErrs[1:]
		return err
	}
	return nil
}
func (*recoveryAgent) Rebuild(string)              {}
func (*recoveryAgent) StopTurn(execution.Ref) bool { return false }
func (a *recoveryAgent) Reattach(workspace, clone, path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reattached == nil {
		a.reattached = map[string][2]string{}
	}
	a.reattached[workspace] = [2]string{clone, path}
}
func (*recoveryAgent) Close(context.Context) error { return nil }

func (a *recoveryAgent) last() (execution.Session, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs[len(a.runs)-1], a.prompts[len(a.prompts)-1]
}

const recoveryGraph = `{"nodes":[{"id":"start","data":{"kind":"start"}},{"id":"a","data":{"kind":"agent","agentConfig":{"executor":{"agentCli":"official/ora-space.opencode","modelId":"fixture"},"prompt":"起草","interactive":true}}}],"edges":[{"source":"start","target":"a"}]}`

// parkedSession starts a held workflow session whose node is parked awaiting input, records its
// checkout at dir and a page history, and restarts the service over the same database.
func parkedSession(t *testing.T, f *runtimeFixture, agent *recoveryAgent, dir string) (execution.Ref, store.WorkflowNodeRun) {
	t.Helper()
	return parkedSessionOn(t, f, agent, dir, "c-old-incarnation")
}

// parkedSessionOn is parkedSession with the checkout recorded under clone, empty for one recorded
// before clone executions were kept.
func parkedSessionOn(t *testing.T, f *runtimeFixture, agent *recoveryAgent, dir, clone string) (execution.Ref, store.WorkflowNodeRun) {
	t.Helper()
	ctx := f.ctx
	if err := f.service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var err error
	if f.service, err = New(ctx, Config{Store: f.db, Models: f.models, Agent: agent}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateProject(ctx, f.scope, store.Project{ID: "project", Name: "Demo", RepositoryURL: "https://example.com/demo.git", Commit: "fixed", MirrorPath: "/fake/unused"}); err != nil {
		t.Fatal(err)
	}
	w, _, err := f.db.CreateWorkflow(ctx, f.scope.TenantID, "交互恢复", recoveryGraph)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.db.PublishWorkflow(ctx, f.scope.TenantID, w.ID, "v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.db.CreateWorkflowRun(ctx, f.scope.TenantID, w.ID, snapshot.ID, store.WorkflowRunInput{ProjectID: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.StartWorkflowRun(ctx, f.scope.TenantID, run.ID); err != nil {
		t.Fatal(err)
	}
	node, err := f.db.CreateWorkflowNodeRun(ctx, f.scope.TenantID, run.ID, store.WorkflowNodeRunInput{NodeID: "a", NodeType: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := f.service.Start(ctx, f.scope, execution.StartRequest{Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: run.ID, NodeID: "a", AttemptID: node.ID},
		Target: execution.Target{ProjectID: "project"}, ExecutorID: "opencode:fixture", Prompt: execution.TextPrompt("起草方案"), Lifetime: execution.LifetimeHeld})
	if err != nil {
		t.Fatal(err)
	}
	awaitResult(t, f, ref)
	session, err := f.db.ReadExecutionSession(ctx, f.scope, ref.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.RecordWorkspace(ctx, session.WorkspaceID, store.WorkspaceLocation{Path: dir, BaseCommit: "fixed", CloneExecution: clone}); err != nil {
		t.Fatal(err)
	}
	history := &execution.HistorySnapshot{Records: []execution.HistoryRecord{historyRecord(t, 1, chunk("user_message_chunk", "起草方案")), historyRecord(t, 2, chunk("agent_message_chunk", "答复：起草方案")), historyRecord(t, 3, turnEnded())}}
	if _, err := f.db.AppendExecutionEvent(ctx, f.scope, execution.Event{Ref: ref, Kind: execution.EventHistorySnapshot, History: history}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.BindWorkflowNodeSession(ctx, f.scope.TenantID, run.ID, node.ID, ref.SessionID); err != nil {
		t.Fatal(err)
	}
	if moved, err := f.db.MoveWorkflowEngineNode(ctx, run.ID, node.ID, store.WorkflowStatusRunning, store.WorkflowStatusPending,
		func([]byte) ([]byte, error) { return []byte(`{"interactive":{"turns":1}}`), nil }); err != nil || !moved {
		t.Fatalf("park = %v %v", moved, err)
	}
	// The process stops; the next one recovers over the same database.
	if err := f.service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if f.service, err = New(ctx, Config{Store: f.db, Models: f.models, Agent: agent}); err != nil {
		t.Fatal(err)
	}
	return ref, node
}

func awaitResult(t *testing.T, f *runtimeFixture, ref execution.Ref) execution.Result {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		turns, err := f.db.ReadExecutionTurns(f.ctx, f.scope, ref.SessionID, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, turn := range turns.Items {
			if turn.Ref == ref && turn.Result != nil {
				return *turn.Result
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("turn %+v never ended", ref)
	return execution.Result{}
}

func followUp(t *testing.T, f *runtimeFixture, session string, node store.WorkflowNodeRun, n int, prompt string) (execution.Ref, error) {
	t.Helper()
	return f.service.Append(f.ctx, f.scope, execution.AppendRequest{SessionID: session,
		Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: node.RunID, NodeID: "a", AttemptID: node.ID + "/turn-" + strconv.Itoa(n)}, Prompt: execution.TextPrompt(prompt)})
}

func TestRecoveredSessionStartsItsAgentOnTheRecordedCheckout(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	dir := t.TempDir()
	first, node := parkedSession(t, f, agent, dir)

	session, err := f.db.ReadExecutionSession(f.ctx, f.scope, first.SessionID)
	if err != nil || session.EndedAt != nil {
		t.Fatalf("session after restart = %+v %v, want the parked node's session open", session, err)
	}
	ref, err := followUp(t, f, first.SessionID, node, 1, "再改一版")
	if err != nil {
		t.Fatalf("follow-up after restart: %v", err)
	}
	if r := awaitResult(t, f, ref); r.Status != execution.StatusCompleted {
		t.Fatalf("recovered turn = %+v", r)
	}
	run, prompt := agent.last()
	agent.mu.Lock()
	reattached := agent.reattached[run.WorkspaceID]
	agent.mu.Unlock()
	if prompt != "再改一版" || run.Recovery == nil || reattached != [2]string{"c-old-incarnation", dir} {
		t.Fatalf("recovered turn ran with %q %+v on %v, want the typed prompt on the recorded checkout", prompt, run.Recovery, reattached)
	}
	if !strings.Contains(run.Recovery.Handoff, "答复：起草方案") || len(run.Recovery.History) != 3 {
		t.Fatalf("recovery handoff %q history %d, want the recorded conversation", run.Recovery.Handoff, len(run.Recovery.History))
	}
	turns, err := f.db.ReadExecutionTurns(f.ctx, f.scope, first.SessionID, "", 100)
	if err != nil || len(turns.Items) != 2 {
		t.Fatalf("turns = %+v %v", turns, err)
	}
	if text, _ := turns.Items[1].Prompt.Text(); text != "再改一版" {
		t.Fatalf("recorded prompt = %q, want what was typed without the handoff", text)
	}

	// The new Agent holds the conversation now: the next turn continues it as usual.
	ref, err = followUp(t, f, first.SessionID, node, 2, "定稿")
	if err != nil {
		t.Fatal(err)
	}
	awaitResult(t, f, ref)
	if run, _ := agent.last(); run.Recovery != nil {
		t.Fatalf("second turn after the restart recovered again: %+v", run.Recovery)
	}
}

func TestRecoveredSessionWaitsWhenItsAgentCannotStart(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	first, node := parkedSession(t, f, agent, t.TempDir())
	agent.mu.Lock()
	agent.results = []execution.Result{{Status: execution.StatusFailed, FailureCode: "agent_prepare_failed", NotStarted: true}}
	agent.mu.Unlock()

	ref, err := followUp(t, f, first.SessionID, node, 1, "再改一版")
	if err != nil {
		t.Fatal(err)
	}
	// Its new Agent never started, so the message never reached one: the workflow host does not
	// count it as a turn and says it was not delivered.
	if r := awaitResult(t, f, ref); r.Status != execution.StatusFailed || r.AgentLost || !r.NotDelivered {
		t.Fatalf("unstarted recovery = %+v, want an undelivered failed turn that keeps the session", r)
	}
	if session, err := f.db.ReadExecutionSession(f.ctx, f.scope, first.SessionID); err != nil || session.EndedAt != nil {
		t.Fatalf("session = %+v %v, want it still open", session, err)
	}
	ref, err = followUp(t, f, first.SessionID, node, 2, "再试一次")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	awaitResult(t, f, ref)
	if run, _ := agent.last(); run.Recovery == nil {
		t.Fatal("the retry did not recover the Agent")
	}
}

func TestRecoveredSessionEndsWhenItsCheckoutIsGone(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	dir := filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	first, node := parkedSession(t, f, agent, dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	runs := len(agent.runs)
	agent.mu.Unlock()

	ref, err := followUp(t, f, first.SessionID, node, 1, "再改一版")
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitResult(t, f, ref); r.FailureCode != execution.FailureWorkspaceMissing || !r.AgentLost {
		t.Fatalf("turn without a checkout = %+v", r)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.runs) != runs {
		t.Fatal("an Agent was started without its checkout")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if session, err := f.db.ReadExecutionSession(f.ctx, f.scope, first.SessionID); err == nil && session.EndedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the session stayed open without its checkout")
}

func TestRunOfANodeParkedOnAnEndedSessionKeepsItsCheckout(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	dir := t.TempDir()
	first, _ := parkedSession(t, f, agent, dir)
	// The starter stopped the node's turn before the restart: its session ended, and the node
	// waits only to be completed. Its run still owns the directory.
	if err := f.service.End(f.ctx, f.scope, first.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	next := &recoveryAgent{}
	var err error
	if f.service, err = New(f.ctx, Config{Store: f.db, Models: f.models, Agent: next}); err != nil {
		t.Fatal(err)
	}
	session, err := f.db.ReadExecutionSession(f.ctx, f.scope, first.SessionID)
	if err != nil || session.EndedAt == nil {
		t.Fatalf("session = %+v %v, want it still ended", session, err)
	}
	next.mu.Lock()
	defer next.mu.Unlock()
	if got := next.reattached[session.WorkspaceID]; got != [2]string{"c-old-incarnation", dir} {
		t.Fatalf("reattached = %v, want the run's recorded checkout for its later sessions", next.reattached)
	}
}

func TestHeldSessionContinuesOnANewAgentWhenItsNodeWasAlreadyGone(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	first, node := parkedSession(t, f, agent, t.TempDir())
	ref, err := followUp(t, f, first.SessionID, node, 1, "再改一版") // the recovered Agent takes over
	if err != nil {
		t.Fatal(err)
	}
	awaitResult(t, f, ref)

	// The Node dies while the node waits: the next message finds it gone before reaching the
	// Agent, and a replacement Agent takes that same turn.
	agent.mu.Lock()
	agent.results = []execution.Result{{Status: execution.StatusFailed, FailureCode: execution.FailureNodeLost, AgentLost: true}}
	agent.errs = []error{execution.ErrNodeLost}
	runs := len(agent.runs)
	agent.mu.Unlock()
	ref, err = followUp(t, f, first.SessionID, node, 2, "定稿")
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitResult(t, f, ref); r.Status != execution.StatusCompleted || r.Answer != "答复：定稿" {
		t.Fatalf("turn after the Node was lost = %+v, want the replacement Agent's answer", r)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.runs) != runs+2 || agent.runs[runs].Recovery != nil || agent.runs[runs+1].Recovery == nil || agent.prompts[runs+1] != "定稿" {
		t.Fatalf("runs after the Node was lost = %+v, want the lost attempt then one recovered run of the same prompt", agent.runs[runs:])
	}
	if !strings.Contains(agent.runs[runs+1].Recovery.Handoff, "答复：起草方案") {
		t.Fatalf("handoff = %q", agent.runs[runs+1].Recovery.Handoff)
	}
}

func TestHeldSessionOutlivesANodeLostMidTurn(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	first, node := parkedSession(t, f, agent, t.TempDir())
	ref, err := followUp(t, f, first.SessionID, node, 1, "再改一版")
	if err != nil {
		t.Fatal(err)
	}
	awaitResult(t, f, ref)

	// The Node dies while the Agent works on a turn: the turn fails and is not replayed, the
	// session stays open, and the next message starts a new Agent.
	agent.mu.Lock()
	agent.results = []execution.Result{{Status: execution.StatusFailed, FailureCode: execution.FailureNodeLost, AgentLost: true}}
	agent.errs = []error{errors.New(execution.FailureNodeLost)}
	agent.mu.Unlock()
	ref, err = followUp(t, f, first.SessionID, node, 2, "做到一半")
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitResult(t, f, ref); r.Status != execution.StatusFailed || r.AgentLost || r.FailureCode != execution.FailureNodeLost {
		t.Fatalf("turn lost with its Node = %+v, want a failed turn that keeps the session", r)
	}
	if session, err := f.db.ReadExecutionSession(f.ctx, f.scope, first.SessionID); err != nil || session.EndedAt != nil {
		t.Fatalf("session = %+v %v, want it open", session, err)
	}
	agent.mu.Lock()
	runs := len(agent.runs)
	agent.mu.Unlock()
	ref, err = followUp(t, f, first.SessionID, node, 3, "继续")
	if err != nil {
		t.Fatalf("message after the lost turn: %v", err)
	}
	if r := awaitResult(t, f, ref); r.Status != execution.StatusCompleted {
		t.Fatalf("recovered turn = %+v", r)
	}
	run, prompt := agent.last()
	agent.mu.Lock()
	total := len(agent.runs)
	agent.mu.Unlock()
	if run.Recovery == nil || prompt != "继续" || total != runs+1 {
		t.Fatalf("last run %+v %q, want one recovered run of the new message only", run.Recovery, prompt)
	}
}

func TestFirstTurnLostWithItsNodeEndsTheSession(t *testing.T) {
	f := runtimeDatabase(t, nil)
	if err := f.service.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	agent := &recoveryAgent{results: []execution.Result{{Status: execution.StatusFailed, FailureCode: execution.FailureNodeLost, AgentLost: true}}, errs: []error{execution.ErrNodeLost}}
	var err error
	if f.service, err = New(f.ctx, Config{Store: f.db, Models: f.models, Agent: agent}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateProject(f.ctx, f.scope, store.Project{ID: "project", Name: "Demo", RepositoryURL: "https://example.com/demo.git", Commit: "fixed", MirrorPath: "/fake/unused"}); err != nil {
		t.Fatal(err)
	}
	// Nothing of a first turn is recorded to hand to a new Agent, so its loss keeps the earlier
	// rule: the session ends and the node fails as one that could not be set up.
	ref, err := f.service.Start(f.ctx, f.scope, execution.StartRequest{Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: "run", NodeID: "a", AttemptID: "attempt"},
		Target: execution.Target{ProjectID: "project"}, ExecutorID: "opencode:fixture", Prompt: execution.TextPrompt("起草方案"), Lifetime: execution.LifetimeHeld})
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitResult(t, f, ref); r.FailureCode != execution.FailureNodeLost || !r.AgentLost {
		t.Fatalf("first turn lost with its Node = %+v, want the session's Agent reported lost", r)
	}
	agent.mu.Lock()
	runs := len(agent.runs)
	agent.mu.Unlock()
	if runs != 1 {
		t.Fatalf("the first turn ran %d times, want once: it is never handed to a new Agent", runs)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if session, err := f.db.ReadExecutionSession(f.ctx, f.scope, ref.SessionID); err == nil && session.EndedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the session stayed open after its first turn was lost")
}

func TestSessionThatIsNotHeldEndsWhenItsNodeIsLost(t *testing.T) {
	f := runtimeDatabase(t, nil)
	if err := f.service.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	agent := &recoveryAgent{}
	var err error
	if f.service, err = New(f.ctx, Config{Store: f.db, Models: f.models, Agent: agent}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateProject(f.ctx, f.scope, store.Project{ID: "project", Name: "Demo", RepositoryURL: "https://example.com/demo.git", Commit: "fixed", MirrorPath: "/fake/unused"}); err != nil {
		t.Fatal(err)
	}
	ref, err := f.service.Start(f.ctx, f.scope, execution.StartRequest{Caller: execution.Caller{Kind: execution.CallerChat, ID: "conversation", AttemptID: "first"},
		Target: execution.Target{ProjectID: "project"}, ExecutorID: "opencode:fixture", Prompt: execution.TextPrompt("第一轮")})
	if err != nil {
		t.Fatal(err)
	}
	awaitResult(t, f, ref)

	// A conversation's Node is gone before its second turn reaches the Agent. Only a held session
	// is continued on a new Agent; this one ends as before, and the turn is not run again.
	agent.mu.Lock()
	agent.results = []execution.Result{{Status: execution.StatusFailed, FailureCode: execution.FailureNodeLost, AgentLost: true}}
	agent.errs = []error{execution.ErrNodeLost}
	runs := len(agent.runs)
	agent.mu.Unlock()
	next, err := f.service.Append(f.ctx, f.scope, execution.AppendRequest{SessionID: ref.SessionID, Caller: execution.Caller{Kind: execution.CallerChat, ID: "conversation", AttemptID: "second"}, Prompt: execution.TextPrompt("第二轮")})
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitResult(t, f, next); r.Status != execution.StatusFailed || r.FailureCode != execution.FailureNodeLost || !r.AgentLost {
		t.Fatalf("conversation turn after its Node was lost = %+v, want it failed with the session's Agent lost", r)
	}
	agent.mu.Lock()
	ran := agent.runs[runs:]
	agent.mu.Unlock()
	if len(ran) != 1 || ran[0].Recovery != nil {
		t.Fatalf("runs after the Node was lost = %+v, want the one lost attempt and no new Agent", ran)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if session, err := f.db.ReadExecutionSession(f.ctx, f.scope, ref.SessionID); err == nil && session.EndedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the conversation's session stayed open after its Node was lost")
}

// recovering reports whether the service still holds the session for a new Agent.
func recovering(f *runtimeFixture, sid string) bool {
	f.service.mu.Lock()
	defer f.service.mu.Unlock()
	return f.service.recovering[sid]
}

// A session waiting for a new Agent, after a restart or after its Node was lost, has no Agent a
// Node holds: End confirms it and the session stops waiting, so the workflow host's End retries
// (cloud#394) settle instead of retrying forever.
func TestEndConfirmsASessionWaitingForANewAgent(t *testing.T) {
	for name, lose := range map[string]func(t *testing.T, f *runtimeFixture, agent *recoveryAgent, session string, node store.WorkflowNodeRun){
		"after a restart": func(*testing.T, *runtimeFixture, *recoveryAgent, string, store.WorkflowNodeRun) {},
		"after its Node was lost": func(t *testing.T, f *runtimeFixture, agent *recoveryAgent, session string, node store.WorkflowNodeRun) {
			ref, err := followUp(t, f, session, node, 1, "再改一版")
			if err != nil {
				t.Fatal(err)
			}
			awaitResult(t, f, ref)
			agent.mu.Lock()
			agent.results = []execution.Result{{Status: execution.StatusFailed, FailureCode: execution.FailureNodeLost, AgentLost: true}}
			agent.errs = []error{errors.New(execution.FailureNodeLost)}
			agent.mu.Unlock()
			if ref, err = followUp(t, f, session, node, 2, "做到一半"); err != nil {
				t.Fatal(err)
			}
			awaitResult(t, f, ref)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := runtimeDatabase(t, nil)
			agent := &recoveryAgent{}
			first, node := parkedSession(t, f, agent, t.TempDir())
			lose(t, f, agent, first.SessionID, node)
			if !recovering(f, first.SessionID) {
				t.Fatal("the session does not wait for a new Agent")
			}
			if err := f.service.End(f.ctx, f.scope, first.SessionID); err != nil {
				t.Fatalf("End of a session waiting for a new Agent = %v, want it confirmed", err)
			}
			if recovering(f, first.SessionID) {
				t.Fatal("the ended session still waits for a new Agent")
			}
			if session, err := f.db.ReadExecutionSession(f.ctx, f.scope, first.SessionID); err != nil || session.EndedAt == nil {
				t.Fatalf("session = %+v %v, want it ended", session, err)
			}
		})
	}
}

// A held session lost with its Node whose End is not confirmed at first (the retired Node's stop
// was not proven yet) is confirmed by the next End, and is never continued on a new Agent after
// that: no turn reaches the Agent again.
func TestARetriedEndOfASessionLostWithItsNodeNeverRevivesIt(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	first, node := parkedSession(t, f, agent, t.TempDir())
	ref, err := followUp(t, f, first.SessionID, node, 1, "再改一版")
	if err != nil {
		t.Fatal(err)
	}
	awaitResult(t, f, ref)
	agent.mu.Lock()
	agent.results = []execution.Result{{Status: execution.StatusFailed, FailureCode: execution.FailureNodeLost, AgentLost: true}}
	agent.errs = []error{errors.New(execution.FailureNodeLost)}
	// The stop right after the loss, and the first End, find the Node's stop unproven.
	agent.endErrs = []error{execution.ErrStopUnconfirmed, execution.ErrStopUnconfirmed}
	agent.mu.Unlock()
	if ref, err = followUp(t, f, first.SessionID, node, 2, "做到一半"); err != nil {
		t.Fatal(err)
	}
	awaitResult(t, f, ref)
	if err := f.service.End(f.ctx, f.scope, first.SessionID); !errors.Is(err, execution.ErrStopUnconfirmed) {
		t.Fatalf("first End = %v, want it unconfirmed", err)
	}
	if err := f.service.End(f.ctx, f.scope, first.SessionID); err != nil {
		t.Fatalf("retried End = %v, want it confirmed", err)
	}
	agent.mu.Lock()
	runs := len(agent.runs)
	agent.mu.Unlock()
	if _, err := followUp(t, f, first.SessionID, node, 3, "继续"); err == nil {
		t.Fatal("a message reached a session whose End was confirmed")
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.runs) != runs || recovering(f, first.SessionID) {
		t.Fatalf("runs %d -> %d, recovering %v: the ended session was continued", runs, len(agent.runs), recovering(f, first.SessionID))
	}
}

// The handoff and this turn's message share Node's limit on one user turn: a long message leaves
// the conversation less room, and the earliest turns are left out to fit beside it.
func TestRecoveryHandoffLeavesRoomForThePrompt(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	first, _ := parkedSession(t, f, agent, t.TempDir())
	var records []execution.HistoryRecord
	for i := range 3 {
		base := uint32(i * 3)
		records = append(records,
			historyRecord(t, base+1, chunk("user_message_chunk", "第 "+strconv.Itoa(i+1)+" 轮")),
			historyRecord(t, base+2, chunk("agent_message_chunk", strings.Repeat("答", 8*1024))),
			historyRecord(t, base+3, turnEnded()))
	}
	if _, err := f.db.AppendExecutionEvent(f.ctx, f.scope, execution.Event{Ref: first, Kind: execution.EventHistorySnapshot, History: &execution.HistorySnapshot{Records: records}}); err != nil {
		t.Fatal(err)
	}
	session, err := f.db.ReadExecutionSession(f.ctx, f.scope, first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	prompt := strings.Repeat("p", 30*1024)
	recovery, gone, err := f.service.recovery(f.ctx, session, execution.Turn{Prompt: execution.TextPrompt(prompt)})
	if err != nil || gone || recovery == nil {
		t.Fatalf("recovery = %+v %v %v", recovery, gone, err)
	}
	if size := len(recovery.Handoff) + len(prompt); size > maxTurnText {
		t.Fatalf("handoff %d bytes + prompt %d bytes = %d, over Node's %d-byte turn limit", len(recovery.Handoff), len(prompt), size, maxTurnText)
	}
	// A turn with host tools leaves their instruction its room.
	reserved, _, err := f.service.recovery(f.ctx, session, execution.Turn{Prompt: execution.TextPrompt(prompt), Host: instructing{}})
	if err != nil || reserved == nil || len(reserved.Handoff)+len(prompt)+execution.MaxHostInstruction > maxTurnText {
		t.Fatalf("recovery with host tools = %+v %v", reserved, err)
	}
	full := strings.Repeat("p", maxTurnText-len(recovery.Handoff)-execution.MaxHostInstruction/2)
	if plain, _, err := f.service.recovery(f.ctx, session, execution.Turn{Prompt: execution.TextPrompt(full)}); err != nil || len(plain.Handoff)+len(full) <= maxTurnText-execution.MaxHostInstruction {
		t.Fatalf("the prompt does not reach into the reserve: %v", err)
	}
	if reserved, _, err := f.service.recovery(f.ctx, session, execution.Turn{Prompt: execution.TextPrompt(full), Host: instructing{}}); err != nil || len(reserved.Handoff)+len(full)+execution.MaxHostInstruction > maxTurnText {
		t.Fatalf("recovery of a long prompt with host tools = %v", err)
	}
	if !strings.Contains(recovery.Handoff, "earliest turns are left out") || !strings.Contains(recovery.Handoff, "第 3 轮") {
		t.Fatalf("handoff = %.200q…, want the latest turn kept and the earliest left out", recovery.Handoff)
	}
}

// A checkout recorded before clone executions were kept has no clone execution: Node cannot be
// asked for it, and a fresh clone would silently lose its edits, so it counts as gone although
// its directory is still there.
func TestRecoveredSessionOnACheckoutWithoutACloneExecutionEnds(t *testing.T) {
	f := runtimeDatabase(t, nil)
	agent := &recoveryAgent{}
	first, node := parkedSessionOn(t, f, agent, t.TempDir(), "")
	agent.mu.Lock()
	runs := len(agent.runs)
	agent.mu.Unlock()

	ref, err := followUp(t, f, first.SessionID, node, 1, "再改一版")
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitResult(t, f, ref); r.FailureCode != execution.FailureWorkspaceMissing || !r.AgentLost {
		t.Fatalf("turn on a checkout without a clone execution = %+v, want %s", r, execution.FailureWorkspaceMissing)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.runs) != runs {
		t.Fatal("an Agent was started on a checkout Node cannot resolve")
	}
}

// instructing is a turn's host tools.
type instructing struct{}

func (instructing) Attach(string, string) execution.HostServers { return execution.HostServers{} }
func (instructing) Started()                                    {}
func (instructing) Instruct() execution.HostInstruction         { return execution.HostInstruction{} }
