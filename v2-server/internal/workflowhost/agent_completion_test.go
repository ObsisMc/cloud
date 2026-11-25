package workflowhost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/workflow"
)

// The agent completion policy: a node whose agentConfig asks for it gives its Agent the completion
// tool, and completes only once the Agent reported the step done in the settled turn and someone
// confirmed that report.

// policyChain is interactiveChain at schemaVersion 4 with A's completion settings set to policy.
func policyChain(policy string) string {
	return `{"nodes":[
 {"id":"start","data":{"kind":"start"}},
 {"id":"a","data":{"kind":"agent","title":"起草","agentConfig":{"schemaVersion":4,"executor":{"agentCli":"official/ora-space.opencode","modelId":"fixture"},"prompt":"起草：{{#start.input#}}","interactive":true,` + policy + `}}},
 {"id":"b","data":{"kind":"agent","title":"审阅","agentConfig":{"executor":{"agentCli":"official/ora-space.opencode","modelId":"fixture"},"prompt":"审阅：{{#a.output#}}"}}},
 {"id":"out","data":{"kind":"output","outputs":[{"name":"review","variableSelector":["b","output"]}]}}
],"edges":[{"source":"start","target":"a"},{"source":"a","target":"b"},{"source":"b","target":"out"}]}`
}

var (
	agentChain      = policyChain(`"completionSignal":"agent"`)
	autoAcceptChain = policyChain(`"completionSignal":"agent","autoAcceptAgentCompletion":true`)
)

// fakeAgent stands in for an Agent that was given the completion tool: it holds the endpoint of its
// start and the token of its latest turn.
type fakeAgent struct {
	t   *testing.T
	url string
}

// agentOf starts the fake Agent of session as the driver would: the endpoint once, per Agent.
func (f *fixture) agentOf(t *testing.T, session string) (*fakeAgent, string) {
	t.Helper()
	host := f.exec.request(session).Host
	if host == nil {
		t.Fatal("the session was not given the completion tool")
	}
	a := &fakeAgent{t: t, url: host.Attach(session).Servers[0].URL}
	host.Started()
	return a, host.Instruct().Token
}

// follow is the token the latest appended turn was sent with.
func (f *fixture) follow(t *testing.T) string {
	t.Helper()
	return f.followTools(t).Instruct().Token
}

// followTools is the completion tool of the latest appended turn.
func (f *fixture) followTools(t *testing.T) execution.HostTools {
	t.Helper()
	f.exec.mu.Lock()
	defer f.exec.mu.Unlock()
	host := f.exec.appended[len(f.exec.appended)-1].Host
	if host == nil {
		t.Fatal("the follow-up turn was not given the completion tool")
	}
	return host
}

// report calls the tool and returns its answer.
func (a *fakeAgent) report(token string) string {
	a.t.Helper()
	resp, err := http.Post(a.url, "application/json", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ora_complete_workflow_step","arguments":{"attemptToken":"`+token+`"}}}`))
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.Status
	}
	var out struct {
		Result struct{ Content []struct{ Text string } }
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		a.t.Fatal(err)
	}
	return out.Result.Content[0].Text
}

// startAgentNode starts graph and returns the run, A's session, its fake Agent and the opening
// turn's token.
func (f *fixture) startAgentNode(t *testing.T, graph string) (runID, session string, agent *fakeAgent, token string) {
	t.Helper()
	runID = f.run(t, graph, str("支付重构"))
	if _, err := f.host.StartRun(context.Background(), f.scope, runID); err != nil {
		t.Fatal(err)
	}
	session = f.exec.awaitStart(t)
	agent, token = f.agentOf(t, session)
	return
}

// turn sends the starter's message, lets the Agent report when done is set, and ends the turn.
func (f *fixture) turn(t *testing.T, runID, session string, agent *fakeAgent, r execution.Result, done bool) {
	t.Helper()
	if _, err := f.host.SendNodeTurn(context.Background(), f.scope, runID, "a", "继续"); err != nil {
		t.Fatal(err)
	}
	f.exec.awaitAppend(t)
	if done {
		if text := agent.report(f.follow(t)); !strings.HasPrefix(text, "accepted:") {
			t.Fatalf("report = %s", text)
		}
	}
	f.exec.finish(session, r)
}

// settled waits for A to park with turns settled turns and returns its summary.
func (f *fixture) settledTurns(t *testing.T, runID string, turns int) map[string]any {
	t.Helper()
	_, nodes := f.await(t, runID, func(run store.WorkflowRun, n []store.WorkflowNodeRun) bool {
		return parked("a")(run, n) && interactivePayload(t, node(n, "a"))["turns"] == float64(turns)
	})
	// The row is written before the host takes the next message: wait for that as well.
	deadline := time.Now().Add(5 * time.Second)
	for st := f.host.state(runID); st != nil; time.Sleep(time.Millisecond) {
		st.mu.Lock()
		n := st.interactive["a"]
		ready := n == nil || n.phase == phaseParked
		st.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the parked node takes no message")
		}
	}
	return interactivePayload(t, node(nodes, "a"))
}

func signalOf(summary map[string]any) string {
	signal, _ := summary["completion"].(map[string]any)["agentSignal"].(map[string]any)
	id, _ := signal["signalId"].(string)
	return id
}

// succeeded waits for A to complete and returns its payload.
func (f *fixture) succeeded(t *testing.T, runID string) (store.WorkflowNodeRun, map[string]any) {
	t.Helper()
	_, nodes := f.await(t, runID, func(_ store.WorkflowRun, n []store.WorkflowNodeRun) bool {
		return node(n, "a").Status == store.WorkflowStatusSucceeded
	})
	return node(nodes, "a"), payloadOf(t, node(nodes, "a"))
}

func (f *fixture) startedSessions() int {
	f.exec.mu.Lock()
	defer f.exec.mu.Unlock()
	return len(f.exec.started)
}

func signalChanged(err error) bool {
	var changed *server.WorkflowNodeSignalChanged
	return errors.As(err, &changed)
}

// Only the agent-policy session gets the tool: an end_turn node and an automatic one start without.
func TestOnlyAnAgentPolicySessionGetsTheCompletionTool(t *testing.T) {
	f := newFixture(t)
	runID, a := f.startParked(t, policyChain(`"completionSignal":"end_turn","autoAcceptAgentCompletion":true`), "初稿")
	if f.exec.request(a).Host != nil {
		t.Fatal("an end_turn node was given the tool")
	}
	f.turn(t, runID, a, nil, completed("定稿"), false)
	if p := f.settledTurns(t, runID, 2); p["completable"] != true || p["completion"].(map[string]any)["completionPolicy"] != "end_turn" {
		t.Fatalf("end_turn summary = %v", p)
	}
	f.exec.mu.Lock()
	followUp := f.exec.appended[len(f.exec.appended)-1].Host
	f.exec.mu.Unlock()
	if followUp != nil {
		t.Fatal("an end_turn follow-up was given the tool")
	}
	if _, err := f.host.CompleteNode(context.Background(), f.scope, runID, "a", server.WorkflowNodeCompletion{}); err != nil {
		t.Fatal(err)
	}
	if b := f.exec.awaitStart(t); f.exec.request(b).Host != nil {
		t.Fatal("an automatic node was given the tool")
	}

	_, agentSession, _, _ := f.startAgentNode(t, agentChain)
	if f.exec.request(agentSession).Lifetime != execution.LifetimeHeld {
		t.Fatal("the tool went to a session that is not held")
	}
}

// The opening turn needs both confirmations at once, each refused without the other, and B starts
// only then; the completion is attributed to the Agent and confirmed by the starter.
func TestAReportedFirstTurnNeedsBothConfirmations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	runID, a, agent, token := f.startAgentNode(t, agentChain)
	if text := agent.report(token); !strings.HasPrefix(text, "accepted:") {
		t.Fatalf("report = %s", text)
	}
	f.exec.finish(a, completed("初稿"))
	p := f.settledTurns(t, runID, 1)
	snap, signal := p["completion"].(map[string]any), signalOf(p)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnUnconfirmed, blockerConfirmationPending}) || signal == "" ||
		snap["completionPolicy"] != "agent" || snap["parkKind"] != parkAwaitingUser || p["completable"] != false || p["answer"] != "初稿" {
		t.Fatalf("summary = %v", p)
	}
	if text := agent.report(token); text != "stale_attempt" {
		t.Fatalf("report after the turn = %s", text)
	}
	for name, tc := range map[string]struct {
		req   server.WorkflowNodeCompletion
		kinds []string
	}{
		"plain":         {server.WorkflowNodeCompletion{}, []string{blockerTurnUnconfirmed, blockerConfirmationPending}},
		"first turn":    {server.WorkflowNodeCompletion{ConfirmInitialTurn: true}, []string{blockerConfirmationPending}},
		"report":        {server.WorkflowNodeCompletion{ConfirmSignalID: signal}, []string{blockerTurnUnconfirmed}},
		"force":         {server.WorkflowNodeCompletion{Force: true}, []string{blockerTurnUnconfirmed, blockerConfirmationPending}},
		"report, force": {server.WorkflowNodeCompletion{ConfirmSignalID: signal, Force: true}, []string{blockerTurnUnconfirmed}},
	} {
		if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", tc.req); !refusedWith(err, tc.kinds...) {
			t.Fatalf("%s = %v, want %v", name, err, tc.kinds)
		}
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmInitialTurn: true, Force: true}); !errors.Is(err, server.ErrWorkflowNodeCompletionInvalid) {
		t.Fatalf("confirm with force beside a report = %v", err)
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmInitialTurn: true, ConfirmSignalID: "other"}); !signalChanged(err) {
		t.Fatalf("another report's confirmation = %v", err)
	}
	if f.startedSessions() != 1 || f.exec.isClosed(a) {
		t.Fatal("a refused completion moved the run")
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmInitialTurn: true, ConfirmSignalID: signal}); err != nil {
		t.Fatal(err)
	}
	f.exec.awaitStart(t)
	row, payload := f.succeeded(t, runID)
	confirmation, _ := payload["confirmation"].(map[string]any)
	if row.Output == nil || *row.Output != "初稿" || payload["requester"] != requesterAgent || confirmation["by"] != confirmedByHuman ||
		confirmation["at"] != float64(1_700_000_000_000) || payload["completion_forced"] != nil || payload["completionPolicy"] != "agent" ||
		payload["agentSignal"].(map[string]any)["signalId"] != signal {
		t.Fatalf("payload = %v", payload)
	}
	if unapplied, _ := payload["unapplied"].([]any); len(unapplied) != 0 {
		t.Fatalf("unapplied = %v", unapplied)
	}
	if status := agent.report(token); !strings.HasPrefix(status, "401") {
		t.Fatalf("report after completion = %s", status)
	}
}

// Without a report a first turn that ended normally completes only by confirming it and forcing at
// once (completion signal ADR D3): force alone is refused, and the audit lists what force covered.
func TestAnUnreportedFirstTurnTakesConfirmationAndForceTogether(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	runID, a, _, _ := f.startAgentNode(t, agentChain)
	f.exec.finish(a, completed("要不要加缓存？"))
	p := f.settledTurns(t, runID, 1)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnUnconfirmed, blockerSignalPending}) || signalOf(p) != "" {
		t.Fatalf("summary = %v", p)
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", confirm); !refusedWith(err, blockerSignalPending) {
		t.Fatalf("confirmation = %v", err)
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{Force: true}); !refusedWith(err, blockerTurnUnconfirmed, blockerSignalPending) {
		t.Fatalf("force = %v", err)
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmInitialTurn: true, ConfirmSignalID: "none"}); !signalChanged(err) {
		t.Fatalf("confirmation of no report = %v", err)
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmInitialTurn: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	_, payload := f.succeeded(t, runID)
	forced, _ := payload["completion_forced"].(map[string]any)
	if payload["requester"] != requesterHuman || payload["confirmation"] != nil || payload["agentSignal"] != nil ||
		!reflect.DeepEqual(forced["blockers"], []any{map[string]any{"kind": blockerSignalPending}}) || forced["by"] != "human" {
		t.Fatalf("payload = %v", payload)
	}
}

// A report speaks for its own turn only: the next turn starts without it, its confirmation is
// refused even with force, and each turn's token dies with it.
func TestAReportDoesNotOutliveItsTurn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	runID, a, agent, token := f.startAgentNode(t, agentChain)
	agent.report(token)
	f.exec.finish(a, completed("初稿"))
	old := signalOf(f.settledTurns(t, runID, 1))
	f.turn(t, runID, a, agent, completed("还差一步"), false)
	p := f.settledTurns(t, runID, 2)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerSignalPending}) || signalOf(p) != "" {
		t.Fatalf("summary = %v, want the earlier report gone", p)
	}
	for _, force := range []bool{false, true} {
		if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmSignalID: old, Force: force}); !signalChanged(err) {
			t.Fatalf("the earlier report's confirmation (force %v) = %v", force, err)
		}
	}
	if text := agent.report(token); text != "stale_attempt" {
		t.Fatalf("the opening turn's token = %s", text)
	}
	f.turn(t, runID, a, agent, completed("定稿"), true)
	p = f.settledTurns(t, runID, 3)
	signal := signalOf(p)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerConfirmationPending}) || signal == "" || signal == old {
		t.Fatalf("summary = %v", p)
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{}); !refusedWith(err, blockerConfirmationPending) {
		t.Fatalf("plain completion = %v", err)
	}
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmSignalID: signal}); err != nil {
		t.Fatal(err)
	}
	row, payload := f.succeeded(t, runID)
	if *row.Output != "定稿" || payload["requester"] != requesterAgent {
		t.Fatalf("A = %v", payload)
	}
}

// Forcing past a report the starter did not confirm keeps the report and says nobody confirmed it.
func TestForcingPastAReportRecordsThatNobodyConfirmedIt(t *testing.T) {
	f := newFixture(t)
	runID, a, agent, _ := f.startAgentNode(t, agentChain)
	f.exec.finish(a, completed("初稿"))
	f.settledTurns(t, runID, 1)
	f.turn(t, runID, a, agent, completed("定稿"), true)
	signal := signalOf(f.settledTurns(t, runID, 2))
	if _, err := f.host.CompleteNode(context.Background(), f.scope, runID, "a", server.WorkflowNodeCompletion{Force: true}); err != nil {
		t.Fatal(err)
	}
	_, payload := f.succeeded(t, runID)
	forced, _ := payload["completion_forced"].(map[string]any)
	if payload["requester"] != requesterAgent || payload["confirmation"] != nil || payload["agentSignal"].(map[string]any)["signalId"] != signal ||
		!reflect.DeepEqual(forced["blockers"], []any{map[string]any{"kind": blockerConfirmationPending}}) {
		t.Fatalf("payload = %v", payload)
	}
}

// A turn that did not end normally says why itself: a report changes neither its blockers nor its
// park, so only force completes it.
func TestAReportDoesNotCoverATurnThatDidNotEndNormally(t *testing.T) {
	f := newFixture(t)
	runID, a, agent, token := f.startAgentNode(t, autoAcceptChain)
	agent.report(token)
	f.exec.finish(a, agentEndedTurns["refusal"])
	p := f.settledTurns(t, runID, 1)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnRefused}) || p["completion"].(map[string]any)["parkKind"] != parkAbnormal {
		t.Fatalf("summary = %v", p)
	}
	f.turn(t, runID, a, agent, agentEndedTurns["refusal"], true)
	p = f.settledTurns(t, runID, 2)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnRefused}) {
		t.Fatalf("summary = %v, want no auto-accept of an abnormal park", p)
	}
	if signalOf(p) != "" {
		t.Fatalf("summary = %v, want no report", p)
	}
	if _, err := f.host.CompleteNode(context.Background(), f.scope, runID, "a", server.WorkflowNodeCompletion{}); !refusedWith(err, blockerTurnRefused) {
		t.Fatalf("completion = %v", err)
	}
	if f.startedSessions() != 1 {
		t.Fatal("B started")
	}
}

// A report made in a turn that did not end normally is dropped with it (completion signal ADR D2):
// the park carries none, and forcing the node is the starter's decision alone.
func TestAReportOfATurnThatDidNotEndNormallyIsDropped(t *testing.T) {
	for name, end := range map[string]func(f *fixture, runID, session string) error{
		"stopped, session kept": func(f *fixture, runID, session string) error {
			f.exec.finish(session, stopKept())
			return nil
		},
		"stopped by the starter": func(f *fixture, runID, _ string) error {
			_, err := f.host.StopNodeTurn(context.Background(), f.scope, runID, "a")
			return err
		},
		"cut at a limit": func(f *fixture, _, session string) error {
			f.exec.finish(session, agentEndedTurns["cut at a limit"])
			return nil
		},
		"refused": func(f *fixture, _, session string) error {
			f.exec.finish(session, agentEndedTurns["refusal"])
			return nil
		},
		"truncated": func(f *fixture, _, session string) error {
			f.exec.finish(session, agentEndedTurns["max_tokens"])
			return nil
		},
		"unknown stop": func(f *fixture, _, session string) error {
			f.exec.finish(session, agentEndedTurns["unknown stop"])
			return nil
		},
		"Agent lost": func(f *fixture, _, session string) error {
			f.exec.finish(session, execution.Result{Status: execution.StatusFailed, FailureCode: "agent_failed", AgentLost: true})
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			runID, a, agent, _ := f.startAgentNode(t, agentChain)
			f.exec.finish(a, completed("初稿"))
			f.settledTurns(t, runID, 1)
			if _, err := f.host.SendNodeTurn(ctx, f.scope, runID, "a", "继续"); err != nil {
				t.Fatal(err)
			}
			f.exec.awaitAppend(t)
			if text := agent.report(f.follow(t)); !strings.HasPrefix(text, "accepted:") {
				t.Fatalf("report = %s", text)
			}
			if err := end(f, runID, a); err != nil {
				t.Fatal(err)
			}
			if p := f.settledTurns(t, runID, 2); signalOf(p) != "" {
				t.Fatalf("summary = %v, want no report", p)
			}
			if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{Force: true}); err != nil {
				t.Fatal(err)
			}
			_, payload := f.succeeded(t, runID)
			if payload["requester"] != requesterHuman || payload["agentSignal"] != nil || payload["completion_forced"] == nil {
				t.Fatalf("payload = %v", payload)
			}
		})
	}
}

// Auto-accept completes the node on its own once a turn the starter opened ends with a report, and
// only then: never the opening turn, never a turn without a report.
func TestAutoAcceptTakesAReportedHumanTurn(t *testing.T) {
	f := newFixture(t)
	runID, a, agent, token := f.startAgentNode(t, autoAcceptChain)
	agent.report(token)
	f.exec.finish(a, completed("初稿"))
	if p := f.settledTurns(t, runID, 1); !slices.Contains(blockerKinds(p), blockerTurnUnconfirmed) || p["completion"].(map[string]any)["autoAcceptAgentCompletion"] != true {
		t.Fatalf("summary = %v", p)
	}
	f.turn(t, runID, a, agent, completed("有个问题"), false)
	f.settledTurns(t, runID, 2)
	if f.startedSessions() != 1 {
		t.Fatal("B started before any report of a human turn")
	}
	f.turn(t, runID, a, agent, completed("定稿"), true)
	b := f.exec.awaitStart(t)
	if text := promptText(t, f.exec.request(b).Prompt); !strings.Contains(text, "审阅：定稿") {
		t.Fatalf("B read %s", text)
	}
	row, payload := f.succeeded(t, runID)
	confirmation, _ := payload["confirmation"].(map[string]any)
	if *row.Output != "定稿" || payload["requester"] != requesterAgent || confirmation["by"] != confirmedByAuto || payload["completion_forced"] != nil {
		t.Fatalf("payload = %v", payload)
	}
	if !f.exec.isClosed(a) {
		t.Fatal("the completed node's session stayed open")
	}
}

// Auto-accept goes through the same gate: a missing deliverable leaves the node to its starter,
// unforced, and the starter's own confirmation is recorded as theirs.
func TestAutoAcceptRefusedByTheGateLeavesTheNodeToTheStarter(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	f.host.runCheckout = func(context.Context, string, string) (deliverableCheckout, error) {
		return deliverableCheckout{Path: dir}, nil
	}
	graph := policyChain(`"completionSignal":"agent","autoAcceptAgentCompletion":true,"completion":{"files":[{"path":"plan.md"}]}`)
	runID, a, agent, _ := f.startAgentNode(t, graph)
	f.exec.finish(a, completed("初稿"))
	f.settledTurns(t, runID, 1)
	f.turn(t, runID, a, agent, completed("定稿"), true)
	signal := signalOf(f.settledTurns(t, runID, 2))
	// The refused acceptance gives the node back; until then the starter's request sees it claimed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := f.host.CompleteNode(context.Background(), f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmSignalID: signal})
		if refusedWith(err, blockerFileMissing) {
			break
		}
		if !errors.Is(err, server.ErrWorkflowNodeNotAwaitingInput) || time.Now().After(deadline) {
			t.Fatalf("confirmation = %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f.startedSessions() != 1 || f.exec.isClosed(a) {
		t.Fatal("a refused auto-accept moved the run")
	}
	if _, err := f.host.CompleteNode(context.Background(), f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmSignalID: signal, Force: true}); err != nil {
		t.Fatal(err)
	}
	_, payload := f.succeeded(t, runID)
	if confirmation, _ := payload["confirmation"].(map[string]any); confirmation["by"] != confirmedByHuman {
		t.Fatalf("payload = %v", payload)
	}
}

// Another node's Agent, which may see the endpoint, reports nothing for this one, and a stored
// state that says the turn is over refuses a report this process would still take.
func TestAReportComesOnlyFromTheNodesOwnLiveTurn(t *testing.T) {
	f := newFixture(t)
	runA, a, mine, myToken := f.startAgentNode(t, autoAcceptChain)
	runOther, other, theirs, theirToken := f.startAgentNode(t, autoAcceptChain)
	if text := theirs.report(myToken); text != "unknown attempt token" {
		t.Fatalf("my token through another Agent's endpoint = %s", text)
	}
	if text := mine.report(theirToken); text != "unknown attempt token" {
		t.Fatalf("another node's token through my endpoint = %s", text)
	}
	f.turnOver.Store(true)
	if text := mine.report(myToken); text != "not_awaiting" {
		t.Fatalf("report of a turn that is stored as over = %s", text)
	}
	f.exec.finish(a, completed("A"))
	f.exec.finish(other, completed("B"))
	if p := f.settledTurns(t, runA, 1); signalOf(p) != "" {
		t.Fatalf("A = %v", p)
	}
	if p := f.settledTurns(t, runOther, 1); signalOf(p) != "" {
		t.Fatalf("other = %v", p)
	}
}

// A cancelled run's Agent loses the tool with its session.
func TestCancellingARunWithdrawsTheTool(t *testing.T) {
	f := newFixture(t)
	runID, a, agent, token := f.startAgentNode(t, agentChain)
	f.exec.finish(a, completed("初稿"))
	f.settledTurns(t, runID, 1)
	if text := agent.report(token); text != "stale_attempt" {
		t.Fatalf("report while parked = %s", text)
	}
	if _, err := f.host.CancelRun(context.Background(), f.scope, runID); err != nil {
		t.Fatal(err)
	}
	if status := agent.report(token); !strings.HasPrefix(status, "401") {
		t.Fatalf("report after the cancellation = %s", status)
	}
}

// A message the starter stopped before the Agent got it leaves the node as it was, its report
// included.
func TestAMessageStoppedBeforeDeliveryKeepsTheReport(t *testing.T) {
	f := newFixture(t)
	runID, a, agent, _ := f.startAgentNode(t, agentChain)
	f.exec.finish(a, completed("初稿"))
	f.settledTurns(t, runID, 1)
	f.turn(t, runID, a, agent, completed("定稿"), true)
	signal := signalOf(f.settledTurns(t, runID, 2))
	f.turn(t, runID, a, agent, execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled", NotDelivered: true}, false)
	p := f.settledTurns(t, runID, 3)
	if signalOf(p) != signal || !reflect.DeepEqual(blockerKinds(p), []string{blockerConfirmationPending}) {
		t.Fatalf("summary = %v", p)
	}
	if _, err := f.host.CompleteNode(context.Background(), f.scope, runID, "a", server.WorkflowNodeCompletion{ConfirmSignalID: signal}); err != nil {
		t.Fatal(err)
	}
}

// A parked node keeps its policy and its report across a restart; the old Agent's endpoint and
// token are gone, and the next turn is given new ones.
func TestAReportSurvivesARestartButItsCredentialsDoNot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	runID, a, agent, token := f.startAgentNode(t, agentChain)
	agent.report(token)
	f.exec.finish(a, completed("初稿"))
	signal := signalOf(f.settledTurns(t, runID, 1))
	_, nodes := f.await(t, runID, parked("a"))
	f.exec.recordedTurns(a, node(nodes, "a").ID, completed("初稿"))
	f.restart(t)
	p := f.settledTurns(t, runID, 1)
	if signalOf(p) != signal || !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnUnconfirmed, blockerConfirmationPending}) {
		t.Fatalf("summary after the restart = %v", p)
	}
	if _, err := f.host.SendNodeTurn(ctx, f.scope, runID, "a", "再改"); err != nil {
		t.Fatal(err)
	}
	f.exec.awaitAppend(t)
	f.exec.mu.Lock()
	host := f.exec.appended[len(f.exec.appended)-1].Host
	f.exec.mu.Unlock()
	fresh := &fakeAgent{t: t, url: host.Attach(a).Servers[0].URL}
	if text := fresh.report(token); text != "unknown attempt token" {
		t.Fatalf("the old token = %s", text)
	}
	if text := fresh.report(host.Instruct().Token); !strings.HasPrefix(text, "accepted:") {
		t.Fatalf("report = %s", text)
	}
	f.exec.finish(a, completed("终稿"))
	if p := f.settledTurns(t, runID, 2); signalOf(p) == "" || signalOf(p) == signal {
		t.Fatalf("summary = %v", p)
	}
}

// A report kept from before the restart counts only for the turn it names.
func TestARestartDropsAReportOfAnotherTurn(t *testing.T) {
	f := newFixture(t)
	runID, a, agent, token := f.startAgentNode(t, agentChain)
	agent.report(token)
	f.exec.finish(a, completed("初稿"))
	f.settledTurns(t, runID, 1)
	_, nodes := f.await(t, runID, parked("a"))
	f.exec.recordedTurns(a, node(nodes, "a").ID, completed("初稿"), completed("没写进摘要的一轮"))
	f.restart(t)
	if p := f.settledTurns(t, runID, 2); signalOf(p) != "" || !reflect.DeepEqual(blockerKinds(p), []string{blockerSignalPending}) {
		t.Fatalf("summary = %v", p)
	}
}

// A node that parked before v2 applied the policy says end_turn and stays so: its Agent was never
// given the tool (default end_turn ADR D4).
func TestANodeParkedBeforeTheToolStaysEndTurn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	runID, a, _, _ := f.startAgentNode(t, agentChain)
	f.exec.finish(a, completed("初稿"))
	f.settledTurns(t, runID, 1)
	_, nodes := f.await(t, runID, parked("a"))
	row := node(nodes, "a")
	f.host.Close()
	if _, err := f.s.MoveWorkflowEngineNode(ctx, runID, row.ID, store.WorkflowStatusPending, store.WorkflowStatusPending, func(payload []byte) ([]byte, error) {
		return workflow.MergePayload(payload, map[string]any{"interactive": map[string]any{"turns": 1, "sent": 0, "session_ended": false,
			"completion": map[string]any{"turnKey": map[string]any{"userSeq": 0, "endedSeq": 1}, "turnOrigin": "workflow", "blockers": []any{}, "warnings": []any{}, "completionPolicy": "end_turn"}}})
	}); err != nil {
		t.Fatal(err)
	}
	f.exec.recordedTurns(a, row.ID, completed("初稿"))
	f.restart(t)
	p := f.settledTurns(t, runID, 1)
	if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnUnconfirmed}) || p["completion"].(map[string]any)["completionPolicy"] != "end_turn" {
		t.Fatalf("summary = %v", p)
	}
	if _, err := f.host.SendNodeTurn(ctx, f.scope, runID, "a", "再改"); err != nil {
		t.Fatal(err)
	}
	f.exec.awaitAppend(t)
	f.exec.mu.Lock()
	host := f.exec.appended[len(f.exec.appended)-1].Host
	f.exec.mu.Unlock()
	if host != nil {
		t.Fatal("a node that parked under end_turn was given the tool")
	}
	f.exec.finish(a, completed("终稿"))
	f.settledTurns(t, runID, 2)
	if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{}); err != nil {
		t.Fatal(err)
	}
}

// A completion started on one report is refused once the node carries another, or none.
func TestACompletionOfAnotherReportIsStale(t *testing.T) {
	snapshot := func(id string) completionSnapshot {
		s := completionSnapshot{TurnKey: &turnKey{UserSeq: 1, EndedSeq: 2}, OutputDigest: "sha256:x"}
		if id != "" {
			s.AgentSignal = &agentSignal{SignalID: id, TurnKey: *s.TurnKey}
		}
		return s
	}
	for _, tc := range []struct {
		stored, live string
		changed      bool
	}{{"s1", "s1", false}, {"", "", false}, {"s1", "s2", true}, {"s1", "", true}, {"", "s2", true}} {
		stored := snapshot(tc.stored)
		if err := stale("a", &stored, snapshot(tc.live)); signalChanged(err) != tc.changed || (err != nil) != tc.changed {
			t.Errorf("stored %q, live %q: %v", tc.stored, tc.live, err)
		}
	}
}

// A run whose graph asks for the policy is refused at its start, before anything runs, when its
// Agents could not be given the tool; a graph that does not ask is not.
func TestARunIsRefusedAtItsStartWithoutTheTool(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.hostTools = execution.ErrHostMCPUnavailable
	runID := f.run(t, agentChain, str("支付重构"))
	if _, err := f.host.StartRun(ctx, f.scope, runID); !errors.Is(err, server.ErrWorkflowCompletionToolUnavailable) {
		t.Fatalf("start = %v", err)
	}
	if run, err := f.s.WorkflowRun(ctx, f.scope.TenantID, runID); err != nil || run.Status != store.WorkflowStatusPending || f.startedSessions() != 0 {
		t.Fatalf("run = %+v %v", run, err)
	}
	// A Node that could not be asked says nothing about the tool.
	for _, failure := range []error{execution.ErrUnavailable, context.Canceled} {
		f.hostTools = failure
		_, err := f.host.StartRun(ctx, f.scope, runID)
		if !errors.Is(err, failure) || errors.Is(err, server.ErrWorkflowCompletionToolUnavailable) || errors.Is(err, server.ErrWorkflowRunNotStartable) {
			t.Fatalf("start = %v, want %v", err, failure)
		}
		if run, err := f.s.WorkflowRun(ctx, f.scope.TenantID, runID); err != nil || run.Status != store.WorkflowStatusPending || f.startedSessions() != 0 {
			t.Fatalf("run = %+v %v", run, err)
		}
	}
	f.hostTools = execution.ErrHostMCPUnavailable
	endTurn := f.run(t, policyChain(`"completionSignal":"end_turn"`), str("支付重构"))
	if _, err := f.host.StartRun(ctx, f.scope, endTurn); err != nil {
		t.Fatal(err)
	}
	f.exec.awaitStart(t)
	f.hostTools = nil
	if _, err := f.host.StartRun(ctx, f.scope, runID); err != nil {
		t.Fatalf("start once the tool is there = %v", err)
	}
	f.exec.awaitStart(t)
}

// An Agent that cannot take the tool fails the node instead of running it under another policy.
func TestAnAgentWithoutHTTPMCPFailsTheNode(t *testing.T) {
	for _, code := range []string{execution.FailureHostMCPAgent, execution.FailureHostMCPNode} {
		f := newFixture(t)
		runID, a, agent, token := f.startAgentNode(t, agentChain)
		f.exec.finish(a, execution.Result{Status: execution.StatusFailed, FailureCode: code, AgentLost: true})
		run, nodes := f.await(t, runID, terminal)
		row := node(nodes, "a")
		if run.Status != store.WorkflowStatusFailed || row.Status != store.WorkflowStatusFailed || row.Error == nil || !strings.Contains(*row.Error, code) {
			t.Fatalf("run %s, A = %+v", run.Status, row)
		}
		if status := agent.report(token); !strings.HasPrefix(status, "401") {
			t.Fatalf("the failed node's endpoint = %s", status)
		}
	}
}
