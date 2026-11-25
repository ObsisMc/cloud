package workflowhost

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// A node whose Agent had not connected to the completion tool when the node parked says so and is
// otherwise like any other (host completion tool ADR D8).

// asks sends one MCP request to the Agent's endpoint.
func (a *fakeAgent) asks(method string) {
	a.t.Helper()
	resp, err := http.Post(a.url, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{}}`))
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("%s = %s", method, resp.Status)
	}
}

// restarted starts the fake Agent of the latest appended turn's new execution.
func (f *fixture) restarted(t *testing.T, session string) *fakeAgent {
	t.Helper()
	tools := f.followTools(t)
	a := &fakeAgent{t: t, url: tools.Attach(session, "x-"+session).Servers[0].URL}
	tools.Started()
	return a
}

func unconnected(summary map[string]any) bool { return summary["completion_tool_unconnected"] == true }

// stopFirst stops A's first turn, which then ends with r.
func (f *fixture) stopFirst(t *testing.T, runID, session string, r execution.Result) {
	t.Helper()
	f.await(t, runID, func(_ store.WorkflowRun, n []store.WorkflowNodeRun) bool {
		return node(n, "a").Status == store.WorkflowStatusRunning && strings.Contains(string(node(n, "a").Payload), `"interactive"`)
	})
	f.exec.onCancel = func(execution.Ref) error {
		f.exec.finish(session, r)
		return nil
	}
	if _, err := f.host.StopNodeTurn(context.Background(), f.scope, runID, "a"); err != nil {
		t.Fatal(err)
	}
}

// said fails unless A's summary says what want does, and has no other form of it.
func said(t *testing.T, when string, summary map[string]any, want bool) {
	t.Helper()
	if _, present := summary["completion_tool_unconnected"]; present != want || unconnected(summary) != want {
		t.Fatalf("%s: summary = %v, want unconnected %v", when, summary, want)
	}
}

// send sends the starter's next message and waits until the node executes it.
func (f *fixture) send(t *testing.T, runID string) {
	t.Helper()
	if _, err := f.host.SendNodeTurn(context.Background(), f.scope, runID, "a", "继续"); err != nil {
		t.Fatal(err)
	}
	f.exec.awaitAppend(t)
}

// executing is A's summary while a turn runs.
func (f *fixture) executing(t *testing.T, runID string) map[string]any {
	t.Helper()
	row := f.nodeRow(t, runID, "a")
	if row.Status != store.WorkflowStatusRunning {
		t.Fatalf("A is %s, want it running", row.Status)
	}
	return interactivePayload(t, row)
}

// An Agent that asked its endpoint for anything the tool needs is connected; one that did not is
// said to be, and the gate, the node and the run are the same either way.
func TestAnAgentThatDidNotConnectIsSaidOnTheNode(t *testing.T) {
	for name, tc := range map[string]struct {
		asks []string
		want bool
	}{
		"nothing":    {nil, true},
		"ping":       {[]string{"ping"}, true},
		"initialize": {[]string{"initialize"}, false},
		"tools/list": {[]string{"tools/list"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			runID, a, agent, token := f.startAgentNode(t, agentChain)
			for _, method := range tc.asks {
				agent.asks(method)
			}
			f.exec.finish(a, completed("初稿"))
			p := f.settledTurns(t, runID, 1)
			said(t, "first park", p, tc.want)
			if !reflect.DeepEqual(blockerKinds(p), []string{blockerTurnUnconfirmed, blockerSignalPending}) || p["completable"] != false ||
				p["session_ended"] != false || p["completion"].(map[string]any)["parkKind"] != parkAwaitingUser {
				t.Fatalf("summary = %v", p)
			}
			run, nodes := f.await(t, runID, parked("a"))
			if run.Status != store.WorkflowRunAwaitingInput {
				t.Fatalf("run is %s", run.Status)
			}
			credential := agent.url[strings.LastIndex(agent.url, "/")+1:]
			if payload := string(node(nodes, "a").Payload); strings.Contains(payload, credential) || strings.Contains(payload, token) {
				t.Fatal("the payload carries a credential")
			}
			// The conversation goes on, and the starter may still complete without a report.
			f.turn(t, runID, a, agent, completed("定稿"), false)
			said(t, "second park", f.settledTurns(t, runID, 2), tc.want)
			if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{}); !refusedWith(err, blockerSignalPending) {
				t.Fatalf("plain completion = %v", err)
			}
			if _, err := f.host.CompleteNode(ctx, f.scope, runID, "a", server.WorkflowNodeCompletion{Force: true}); err != nil {
				t.Fatal(err)
			}
			row, payload := f.succeeded(t, runID)
			said(t, "completed", payload["interactive"].(map[string]any), false)
			if row.Output == nil || *row.Output != "定稿" {
				t.Fatalf("A = %+v", row)
			}
			f.exec.awaitStart(t)
		})
	}
}

// The notice follows the execution: it stays while the next turn runs, goes at the first park after
// the Agent connected, and does not come back.
func TestAConnectionOfTheSameExecutionEndsTheNotice(t *testing.T) {
	f := newFixture(t)
	runID, a, agent, _ := f.startAgentNode(t, agentChain)
	f.exec.finish(a, completed("初稿"))
	said(t, "first park", f.settledTurns(t, runID, 1), true)
	f.send(t, runID)
	agent.asks("initialize")
	said(t, "while the turn runs", f.executing(t, runID), true)
	f.exec.finish(a, completed("二稿"))
	said(t, "second park", f.settledTurns(t, runID, 2), false)
	f.turn(t, runID, a, agent, completed("三稿"), false)
	said(t, "third park", f.settledTurns(t, runID, 3), false)
}

// So does one that came before a message the execution service refused: the node parks again, in the
// same execution. When the session turned out to have ended, that execution is over, and the last
// conclusion stands.
func TestARefusedMessageParksWithWhatIsKnownByThen(t *testing.T) {
	for name, tc := range map[string]struct {
		refusal   error
		connected bool // by the first park; otherwise only after it
		want      bool
	}{
		"in the same execution":                {execution.ErrConflict, false, false},
		"session ended, connected too late":    {execution.ErrSessionEnded, false, true},
		"session ended, connected by the park": {execution.ErrSessionEnded, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			runID, a, agent, _ := f.startAgentNode(t, agentChain)
			if tc.connected {
				agent.asks("initialize")
			}
			f.exec.finish(a, completed("初稿"))
			said(t, "first park", f.settledTurns(t, runID, 1), !tc.connected)
			agent.asks("initialize")
			f.refuseAppendsOf(a, tc.refusal)
			if _, err := f.host.SendNodeTurn(context.Background(), f.scope, runID, "a", "继续"); err != nil {
				t.Fatal(err)
			}
			_, nodes := f.await(t, runID, func(run store.WorkflowRun, n []store.WorkflowNodeRun) bool {
				return parked("a")(run, n) && interactivePayload(t, node(n, "a"))["last_turn_error"] == turnErrorNotDelivered
			})
			p := interactivePayload(t, node(nodes, "a"))
			said(t, "after the refused message", p, tc.want)
			if ended := tc.refusal == execution.ErrSessionEnded; p["session_ended"] != ended {
				t.Fatalf("summary = %v, want session_ended %v", p, ended)
			}
		})
	}
}

// A new execution is judged at its own first park; until then the last conclusion stands.
func TestANewExecutionIsJudgedAtItsFirstPark(t *testing.T) {
	for name, first := range map[string]bool{"the first connected": true, "the first did not": false} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			runID, a, agent, _ := f.startAgentNode(t, agentChain)
			if first {
				agent.asks("tools/list")
			}
			f.exec.finish(a, completed("初稿"))
			said(t, "first park", f.settledTurns(t, runID, 1), !first)
			f.send(t, runID)
			next := f.restarted(t, a)
			if !first {
				next.asks("initialize")
			}
			said(t, "while the new execution's first turn runs", f.executing(t, runID), !first)
			f.exec.finish(a, completed("二稿"))
			said(t, "the new execution's first park", f.settledTurns(t, runID, 2), first)
		})
	}
}

// Without a start that was sent there is no execution to speak of: a first turn says nothing, and a
// later one leaves the last conclusion as it was.
func TestAnExecutionThatWasNotStartedIsNotJudged(t *testing.T) {
	notStarted := execution.Result{Status: execution.StatusFailed, FailureCode: "execution_unavailable", NotStarted: true, NotDelivered: true}
	for name, attach := range map[string]bool{"before its credential": false, "after its credential": true} {
		t.Run("first turn stopped "+name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			runID := f.run(t, agentChain, str("支付重构"))
			if _, err := f.host.StartRun(ctx, f.scope, runID); err != nil {
				t.Fatal(err)
			}
			a := f.exec.awaitStart(t)
			if attach {
				f.exec.request(a).Host.Attach(a, "x-"+a)
			}
			r := notStarted
			r.Status, r.FailureCode, r.StopReason, r.AgentLost = execution.StatusCancelled, "", "cancelled", true
			f.stopFirst(t, runID, a, r)
			said(t, "park", f.settledTurns(t, runID, 1), false)
		})
		for when, connected := range map[string]bool{"connected": true, "not connected": false, "connected too late": false} {
			t.Run("later execution "+name+", the earlier one "+when, func(t *testing.T) {
				f := newFixture(t)
				runID, a, agent, _ := f.startAgentNode(t, agentChain)
				if connected {
					agent.asks("initialize")
				}
				f.exec.finish(a, completed("初稿"))
				said(t, "first park", f.settledTurns(t, runID, 1), !connected)
				if when == "connected too late" {
					agent.asks("initialize") // then its Node was lost
				}
				f.send(t, runID)
				if attach {
					f.followTools(t).Attach(a, "x-"+a)
				}
				f.exec.finish(a, notStarted)
				_, nodes := f.await(t, runID, func(run store.WorkflowRun, n []store.WorkflowNodeRun) bool {
					return parked("a")(run, n) && interactivePayload(t, node(n, "a"))["last_turn_error"] == turnErrorNotDelivered
				})
				said(t, "after the refused message", interactivePayload(t, node(nodes, "a")), !connected)
			})
		}
	}
}

// However the first turn of an execution ended, its start was sent: a stop or a failure before the
// Agent connected is said like a normal end.
func TestAFirstTurnThatEndedAbnormallyIsJudgedToo(t *testing.T) {
	for name, r := range map[string]execution.Result{
		"failed":    {Status: execution.StatusFailed, FailureCode: "agent_failed", AgentLost: true},
		"refused":   {Status: execution.StatusCompleted, AnswerComplete: true, StopReason: "refusal"},
		"cancelled": {Status: execution.StatusCancelled, StopReason: "cancelled"},
	} {
		for _, connected := range []bool{true, false} {
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				runID, a, agent, _ := f.startAgentNode(t, agentChain)
				if connected {
					agent.asks("initialize")
				}
				f.exec.finish(a, r)
				said(t, "park", f.settledTurns(t, runID, 1), !connected)
			})
		}
	}
	t.Run("stopped by the starter", func(t *testing.T) {
		f := newFixture(t)
		runID, a, _, _ := f.startAgentNode(t, agentChain)
		f.stopFirst(t, runID, a, execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled"})
		said(t, "park", f.settledTurns(t, runID, 1), true)
	})
}

// The notice is part of the stored summary, so it waits through a restart with the node; the Agent
// of the next turn is judged on its own.
func TestTheNoticeSurvivesARestart(t *testing.T) {
	for name, connected := range map[string]bool{"connected": true, "not connected": false} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			runID, a, agent, _ := f.startAgentNode(t, agentChain)
			if connected {
				agent.asks("initialize")
			}
			f.exec.finish(a, completed("初稿"))
			f.settledTurns(t, runID, 1)
			_, nodes := f.await(t, runID, parked("a"))
			f.exec.recordedTurns(a, node(nodes, "a").ID, completed("初稿"))
			f.restart(t)
			said(t, "after the restart", f.settledTurns(t, runID, 1), !connected)
			f.send(t, runID)
			next := f.restarted(t, a)
			if !connected {
				next.asks("tools/list")
			}
			said(t, "while the next turn runs", f.executing(t, runID), !connected)
			f.exec.finish(a, completed("二稿"))
			said(t, "after the next turn", f.settledTurns(t, runID, 2), connected)
		})
	}
}

// Nothing is said of a node that was given no tool.
func TestAnEndTurnNodeSaysNothingOfTheTool(t *testing.T) {
	f := newFixture(t)
	runID, a := f.startParked(t, policyChain(`"completionSignal":"end_turn"`), "初稿")
	said(t, "first park", f.settledTurns(t, runID, 1), false)
	f.turn(t, runID, a, nil, completed("定稿"), false)
	said(t, "second park", f.settledTurns(t, runID, 2), false)
}
