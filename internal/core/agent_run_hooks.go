package core

import "fmt"

// AgentRunHooks is the business-side transition seam the control plane calls inside
// its own transactions (controller-integration D6). Each hook moves IssueRun/Thread
// business state from evidence the control plane has just persisted; a hook never
// opens its own transaction and must be deterministic for a given event set. A
// non-nil error rolls back the whole control-plane transaction, so the Controller
// does not confirm the events and the Node replays them.
//
// Business tables (issue_runs phase/thread_state/idle_since, thread_entries,
// space_agents) are written only by these hooks and the business API; the control
// plane never writes them directly, and vice versa (D6 invariant 7).
type AgentRunHooks interface {
	// RunWorkspaceSettled runs in the transaction where the run Workspace's
	// create_workspace operation reaches a terminal state: ready=true moves the
	// run to `starting`, ready=false (create_workspace failed) moves it to
	// `releasing` with status=failed and failure_reason=workspace_unavailable
	// (IssueRun D3).
	RunWorkspaceSettled(t *transaction, run Object, ready bool) error

	// ThreadEventsTakenOver runs in TakeOverThreadEvents after the event receipts
	// are written: it assigns gapless seq values, writes thread_entries rows,
	// advances thread_state, marks delivered user turns, and moves the run to
	// `running` (Thread D1/D3, IssueRun D3).
	ThreadEventsTakenOver(t *transaction, run, execution Object, events []Object) error

	// SessionEnded runs in the takeover transaction of the session execution's
	// terminal event: it moves the Thread to `ended`, discards queued user turns,
	// moves the run to `delivering` and releases the revision-delivery work item
	// (Thread D4, IssueRun D3).
	SessionEnded(t *transaction, run, execution, ended Object) error

	// DeliverySettled runs in the takeover transaction of a revision-delivery
	// execution's terminal event, after the revisions row is written: a saved or
	// unchanged revision moves the run to `releasing`; a failed delivery keeps it
	// `delivering` and releases a backoff retry (IssueRun D4/D5).
	DeliverySettled(t *transaction, run, execution Object, result DeliverySettledResult) error

	// RunWorkspaceDeleted runs in the transaction where the run Workspace's
	// delete_workspace operation succeeds: it moves the run to `done` (IssueRun D3).
	RunWorkspaceDeleted(t *transaction, run Object) error
}

// DeliverySettledResult is the terminal outcome of one revision-delivery execution
// (controller-integration D6 deliverySettled). Exactly one variant is set: Kind is
// "saved" or "unchanged" and carries the registered RevisionID, or Kind is "failed"
// and carries the failure Reason (IssueRun D5).
type DeliverySettledResult struct {
	Kind       string
	RevisionID string
	Reason     string
}

// Delivery outcome kinds for DeliverySettledResult.
const (
	DeliverySaved     = "saved"
	DeliveryUnchanged = "unchanged"
	DeliveryFailed    = "failed"
)

// UnavailableAgentRunHooks is the default hook set for a Store with no AgentRunHooks
// wired. Every hook returns an explicit not-implemented error instead of silently
// succeeding, so no control-plane transaction can advance business state while the
// AgentRunDispatcher transitions are unimplemented (B skeleton).
type UnavailableAgentRunHooks struct{}

func (UnavailableAgentRunHooks) RunWorkspaceSettled(t *transaction, run Object, ready bool) error {
	return fmt.Errorf("business hook runWorkspaceSettled not implemented: B skeleton")
}

func (UnavailableAgentRunHooks) ThreadEventsTakenOver(t *transaction, run, execution Object, events []Object) error {
	return fmt.Errorf("business hook threadEventsTakenOver not implemented: B skeleton")
}

func (UnavailableAgentRunHooks) SessionEnded(t *transaction, run, execution, ended Object) error {
	return fmt.Errorf("business hook sessionEnded not implemented: B skeleton")
}

func (UnavailableAgentRunHooks) DeliverySettled(t *transaction, run, execution Object, result DeliverySettledResult) error {
	return fmt.Errorf("business hook deliverySettled not implemented: B skeleton")
}

func (UnavailableAgentRunHooks) RunWorkspaceDeleted(t *transaction, run Object) error {
	return fmt.Errorf("business hook runWorkspaceDeleted not implemented: B skeleton")
}
