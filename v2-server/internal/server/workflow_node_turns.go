package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// WorkflowNodeTurns is the execution bridge's interactive Agent session: a human turn sent to a
// parked node and stopping the turn it is executing. It is separate from WorkflowRunHost because
// these routes are v2-only (api/openapi.json has no node-level turn or stop route); a bridge that
// does not implement it keeps them unavailable.
type WorkflowNodeTurns interface {
	SendNodeTurn(ctx context.Context, scope WorkflowRunScope, runID, nodeID, content string) (WorkflowNodeTurnAccepted, error)
	StopNodeTurn(ctx context.Context, scope WorkflowRunScope, runID, nodeID string) (store.WorkflowRun, error)
}

// WorkflowNodeTurnAccepted answers an accepted human turn: the run, and the number the bridge gave
// the message among the starter's messages to this node. payload.interactive.sent counts the same
// messages, so the sender can tell whether a later not_delivered notice is about this message.
type WorkflowNodeTurnAccepted struct {
	Run  store.WorkflowRun
	Sent int
}

var (
	// ErrWorkflowRunOwned reports a turn, stop or completion by someone other than the run's
	// starter. The node's session belongs to the starter, so only they can act on it.
	ErrWorkflowRunOwned = errors.New("workflow run started by someone else")
	// ErrWorkflowNodeSessionEnded reports a turn sent to a parked node whose session was stopped.
	ErrWorkflowNodeSessionEnded = errors.New("workflow node session ended")
	// ErrWorkflowNodeTurnNotRunning reports a stop while the node executes no turn.
	ErrWorkflowNodeTurnNotRunning = errors.New("workflow node turn is not running")
)

// WorkflowNodeCompletion is what the starter decided on one completion request: ConfirmInitialTurn
// adopts the workflow's own opening turn, ConfirmSignalID confirms the Agent's report of that ID,
// Force completes despite every other blocker and leaves an audit (desktop
// CompleteWorkflowNodeRequest). Who the completion is attributed to is the host's to say.
type WorkflowNodeCompletion struct {
	Force, ConfirmInitialTurn bool
	ConfirmSignalID           string
}

// WorkflowNodeBlocker is one reason a parked node cannot complete: a stable kind and, for a
// deliverable, the declared path, or a safe detail for an output that does not fit its schema.
type WorkflowNodeBlocker struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message,omitempty"`
}

// WorkflowNodeIncomplete refuses completing a parked node with the blockers the request left
// unresolved.
type WorkflowNodeIncomplete struct {
	Blockers []WorkflowNodeBlocker
}

func (e *WorkflowNodeIncomplete) Error() string {
	kinds := make([]string, len(e.Blockers))
	for i, b := range e.Blockers {
		kinds[i] = b.Kind
	}
	return "workflow node is incomplete: " + strings.Join(kinds, ", ")
}

// WorkflowNodeTurnKey identifies a node's settled turn: the number of the message that opened
// it (0 for the workflow's opening turn) and how many turns had settled with it.
type WorkflowNodeTurnKey struct {
	UserSeq  int `json:"userSeq"`
	EndedSeq int `json:"endedSeq"`
}

// WorkflowNodeTurnChanged refuses a completion whose settled turn is no longer the one the node
// parked with; the page reloads the run and asks again. Force does not cover it.
type WorkflowNodeTurnChanged struct {
	NodeID           string
	Expected, Actual *WorkflowNodeTurnKey
}

func (e *WorkflowNodeTurnChanged) Error() string {
	return fmt.Sprintf("workflow node %s settled another turn since it parked", e.NodeID)
}

// WorkflowNodeOutputChanged refuses a completion whose answer is no longer the one the node
// parked with. Force does not cover it.
type WorkflowNodeOutputChanged struct {
	NodeID, Expected, Actual string
}

func (e *WorkflowNodeOutputChanged) Error() string {
	return fmt.Sprintf("workflow node %s answer changed since it parked", e.NodeID)
}

// WorkflowNodeSignalChanged refuses a completion naming a report other than the one of the node's
// settled turn; the page reloads the run and asks again. Force does not cover it.
type WorkflowNodeSignalChanged struct {
	NodeID, Expected, Actual string
}

func (e *WorkflowNodeSignalChanged) Error() string {
	return fmt.Sprintf("workflow node %s carries another completion report", e.NodeID)
}

// ErrWorkflowNodeCompletionInvalid refuses a completion asking for both confirmInitialTurn and
// force, except for an agent policy node whose opening turn ended normally without a report, where
// the confirmation lifts turn_unconfirmed and force covers the missing report.
var ErrWorkflowNodeCompletionInvalid = errors.New("confirmInitialTurn and force cannot be combined")

// ErrWorkflowCompletionToolUnavailable refuses a run whose graph has an interactive node with the
// agent completion policy when its Agent could not be given the completion tool.
var ErrWorkflowCompletionToolUnavailable = errors.New("the Agent completion tool is not available to this run")

// maxNodeTurnRunes bounds one human turn like a chat message, so a turn cannot carry a document.
const maxNodeTurnRunes = 16000

func (h *handler) workflowNodeTurns(w http.ResponseWriter, r *http.Request, a access) (WorkflowNodeTurns, WorkflowRunScope, string, string, bool) {
	host, scope, ok := h.workflowRunHost(w, a)
	if !ok {
		return nil, scope, "", "", false
	}
	turns, ok := host.(WorkflowNodeTurns)
	if !ok {
		fault(w, http.StatusServiceUnavailable, "workflow_run_host_unavailable")
		return nil, scope, "", "", false
	}
	runID, ok := workflowRunID(w, r)
	if !ok {
		return nil, scope, "", "", false
	}
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		fault(w, http.StatusNotFound, "workflow_node_not_found")
		return nil, scope, "", "", false
	}
	return turns, scope, runID, nodeID, true
}

// sendWorkflowNodeTurn queues a human turn on a parked interactive node. The answer arrives over
// the node's session events; the response only confirms the node is running the turn, and says
// which of the starter's messages this one is.
func (h *handler) sendWorkflowNodeTurn(w http.ResponseWriter, r *http.Request, a access) {
	turns, scope, runID, nodeID, ok := h.workflowNodeTurns(w, r, a)
	if !ok {
		return
	}
	var in struct {
		Content string `json:"content"`
	}
	if !decodeWorkflow(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Content) == "" || utf8.RuneCountInString(in.Content) > maxNodeTurnRunes {
		fault(w, http.StatusBadRequest, "invalid_content")
		return
	}
	accepted, err := turns.SendNodeTurn(r.Context(), scope, runID, nodeID, in.Content)
	if err != nil {
		workflowRunLifecycleFault(w, err, "send workflow node turn")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": toWorkflowRunView(accepted.Run), "sent": accepted.Sent})
}

// stopWorkflowNodeTurn stops the turn an interactive node is executing. The pinned Node can only
// stop a turn by ending its session, so the node parks without further turns; the run goes on.
func (h *handler) stopWorkflowNodeTurn(w http.ResponseWriter, r *http.Request, a access) {
	turns, scope, runID, nodeID, ok := h.workflowNodeTurns(w, r, a)
	if !ok {
		return
	}
	run, err := turns.StopNodeTurn(r.Context(), scope, runID, nodeID)
	if err != nil {
		workflowRunLifecycleFault(w, err, "stop workflow node turn")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": toWorkflowRunView(run)})
}

// interactiveNodeFault maps the interactive refusals; false leaves err to the lifecycle mapping.
func interactiveNodeFault(w http.ResponseWriter, err error) bool {
	var incomplete *WorkflowNodeIncomplete
	var turnChanged *WorkflowNodeTurnChanged
	var outputChanged *WorkflowNodeOutputChanged
	var signalChanged *WorkflowNodeSignalChanged
	switch {
	case errors.Is(err, ErrWorkflowRunOwned):
		fault(w, http.StatusConflict, "run_owned")
	case errors.Is(err, ErrWorkflowNodeSessionEnded):
		fault(w, http.StatusConflict, "workflow_node_session_ended")
	case errors.Is(err, ErrWorkflowNodeTurnNotRunning):
		fault(w, http.StatusConflict, "workflow_node_turn_not_running")
	case errors.As(err, &incomplete):
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":      "workflow_node_incomplete",
			"params":    map[string]any{"blockers": incomplete.Blockers},
			"requestId": requestID(),
		})
	case errors.As(err, &turnChanged):
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":      "workflow_node_turn_changed",
			"params":    map[string]any{"nodeId": turnChanged.NodeID, "expected": turnChanged.Expected, "actual": turnChanged.Actual},
			"requestId": requestID(),
		})
	case errors.As(err, &outputChanged):
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":      "workflow_node_output_changed",
			"params":    map[string]any{"nodeId": outputChanged.NodeID, "expected": outputChanged.Expected, "actual": outputChanged.Actual},
			"requestId": requestID(),
		})
	case errors.As(err, &signalChanged):
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":      "workflow_node_signal_changed",
			"params":    map[string]any{"nodeId": signalChanged.NodeID, "expected": signalChanged.Expected, "actual": signalChanged.Actual},
			"requestId": requestID(),
		})
	case errors.Is(err, ErrWorkflowNodeCompletionInvalid):
		fault(w, http.StatusBadRequest, "invalid_request")
	default:
		return false
	}
	return true
}
