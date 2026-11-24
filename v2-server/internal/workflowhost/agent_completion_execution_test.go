package workflowhost

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/completiontool"
	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/executionruntime"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// reportingDriver is an Agent driver whose Agent reports the step done in every turn, through the
// tool it was started with, as the real one does.
type reportingDriver struct {
	t  *testing.T
	db *store.Store

	mu      sync.Mutex
	agents  map[string]*fakeAgent
	answers []string // what the tool answered, per turn
	tokens  []string
}

func (d *reportingDriver) Run(ctx context.Context, _ execution.Scope, s execution.Session, turn execution.Turn, _ func(context.Context, execution.Event) error) (execution.Result, error) {
	done := execution.Result{Status: execution.StatusCompleted, AnswerComplete: true, Answer: "做完了", StopReason: "end_turn"}
	if turn.Host == nil {
		return done, nil
	}
	d.mu.Lock()
	agent := d.agents[s.ID]
	if agent == nil {
		agent = &fakeAgent{t: d.t, url: turn.Host.Attach(s.ID).Servers[0].URL}
		d.agents[s.ID] = agent
	}
	d.mu.Unlock()
	instruction := turn.Host.Instruct()
	// The node is bound to its session right after Start returned, long before a real Agent calls.
	nodeRun, _, _ := strings.Cut(turn.Caller.AttemptID, "/")
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if live, err := d.db.WorkflowNodeTurnLive(ctx, nodeRun, s.ID); err != nil {
			d.t.Error(err)
			break
		} else if live {
			break
		}
	}
	answer := agent.report(instruction.Token)
	d.mu.Lock()
	d.answers, d.tokens = append(d.answers, answer), append(d.tokens, instruction.Token)
	d.mu.Unlock()
	return done, nil
}
func (*reportingDriver) End(context.Context, string) error { return nil }
func (*reportingDriver) Rebuild(string)                    {}
func (*reportingDriver) Reattach(string, string, string)   {}
func (*reportingDriver) Close(context.Context) error       { return nil }
func (*reportingDriver) StopTurn(execution.Ref) bool       { return false }

// Over the real execution service and stored state: the tool is live while a turn of the node's own
// session executes and at no other time, and neither the recorded prompt, the turns nor the node's
// payload carry a token.
func TestTheToolFollowsStoredExecutionState(t *testing.T) {
	f := newFixture(t)
	f.host.Close()
	ctx := t.Context()
	driver := &reportingDriver{t: t, db: f.s, agents: map[string]*fakeAgent{}}
	runtime, err := executionruntime.New(ctx, executionruntime.Config{Store: f.s, Models: executionruntime.WithFixtureModels(nil), Agent: driver})
	if err != nil {
		t.Fatal(err)
	}
	tools := completiontool.NewRegistry(f.s.WorkflowNodeTurnLive)
	served, err := completiontool.Serve(tools)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(served.Close)
	f.host, err = NewHost(Config{Store: f.s, Execution: runtime, Catalog: runtime, CompletionTool: tools,
		CheckHostTools: func(context.Context, execution.Scope, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.host.Close()
		if err := runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	runID := f.run(t, agentChain, str("支付重构"))
	if _, err := f.host.StartRun(ctx, f.scope, runID); err != nil {
		t.Fatal(err)
	}
	p := f.settledTurns(t, runID, 1)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnUnconfirmed, blockerConfirmationPending}) {
		t.Fatalf("summary = %v", p)
	}
	_, nodes := f.await(t, runID, parked("a"))
	row := node(nodes, "a")
	live := func(nodeRun, session string) bool {
		t.Helper()
		ok, err := f.s.WorkflowNodeTurnLive(ctx, nodeRun, session)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if live(row.ID, *row.SessionID) {
		t.Fatal("a parked node's turn is live")
	}
	if _, err := f.host.SendNodeTurn(ctx, f.scope, runID, "a", "继续"); err != nil {
		t.Fatal(err)
	}
	p = f.settledTurns(t, runID, 2)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerConfirmationPending}) {
		t.Fatalf("summary = %v", p)
	}
	driver.mu.Lock()
	answers, tokens := driver.answers, driver.tokens
	driver.mu.Unlock()
	if len(answers) != 2 || !strings.HasPrefix(answers[0], "accepted:") || !strings.HasPrefix(answers[1], "accepted:") || answers[0] == answers[1] {
		t.Fatalf("answers = %v", answers)
	}
	if live(row.ID, "another-session") || live("00000000-0000-4000-8000-000000000000", *row.SessionID) {
		t.Fatal("another node's or session's turn is live")
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmSignalID: signalOf(p)}); err != nil {
		t.Fatal(err)
	}
	f.succeeded(t, runID)
	_, nodes = f.await(t, runID, terminal)
	turns, err := runtime.ReadTurns(ctx, executionScope(f.scope), *row.SessionID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := json.Marshal(map[string]any{"nodes": nodes, "turns": turns})
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if strings.Contains(string(stored), token) {
			t.Fatal("a token was stored")
		}
	}
}
