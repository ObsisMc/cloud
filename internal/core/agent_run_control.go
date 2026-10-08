package core

import (
	"fmt"
	"time"
)

// AgentRunControlPlane is the control-plane A-seam the business layer calls to declare
// execution work for a Controller to act on (controller-integration D6, B→A direction).
//
// Each method runs and commits inside the caller's transaction: the caller owns the
// transaction and the control-plane implementation must never open its own transaction,
// spawn a goroutine, or make an async/network callback. A non-nil error rolls back the
// caller's transaction with no durable effect. The reverse direction (control plane →
// business state) is the separate AgentRunHooks interface; the two are deliberately not
// merged (D6 ownership: B calls these A-seams, A calls those B-hooks).
type AgentRunControlPlane interface {
	// CreateRunWorkspace declares the run's isolated Workspace (kind=isolated) and its
	// create_workspace operation, idempotently keyed on (issue_run_id, 'create_workspace'),
	// in the caller's transaction (IssueRun D2, operation D4). Busy is NOT an error: when
	// the Project already has an operation in flight the intent is not established, the
	// caller commits normally and keeps the run queued for the dispatch loop to retry
	// (IssueRun D3); that is expressed as a RunWorkspaceOutcome with Busy set, never as a
	// returned error. A non-nil error is a real invariant/internal failure.
	CreateRunWorkspace(t *transaction, run Object) (RunWorkspaceOutcome, error)

	// DeleteRunWorkspace idempotently declares the delete_workspace operation for the
	// run's Workspace, in the caller's transaction (IssueRun D3, operation D4). Only the
	// declaration happens here; the Controller executes and settles it.
	DeleteRunWorkspace(t *transaction, run Object) error

	// EnqueueExecutionWork releases an execution_work item in the caller's transaction
	// (D6 enqueueExecutionWork). kind is 'agent_session' or 'deliver_revision' and input
	// carries the fixed AgentSession/delivery snapshot, which the control plane must
	// consume as-is — including the plugin identity/version — never re-reading the current
	// space_agents/space_plugins version to override it (IssueRun D1/D6). availableAt
	// defers eligibility (nil means now). Returns the execution_work id (WorkAvailable).
	EnqueueExecutionWork(t *transaction, run Object, kind string, input, target Object, availableAt *time.Time) (string, error)

	// EnqueueThreadCommand releases a SubmitUserTurn/EndSession command in the caller's
	// transaction (D6 enqueueThreadCommand). Returns the command id (ThreadCommandAvailable).
	EnqueueThreadCommand(t *transaction, run, command Object) (string, error)
}

// RunWorkspaceOutcome is the distinct non-error return of CreateRunWorkspace. Exactly one
// of Accepted or Busy is set on a success path; a non-nil error carries the failing case.
// Busy is how D6 expresses "the Project already has an operation" without an error, so the
// caller commits normally and keeps the run queued for the dispatch loop to retry.
type RunWorkspaceOutcome struct {
	// Accepted is true when the create_workspace intent (and the run Workspace binding) was
	// established in the caller's transaction.
	Accepted bool
	// Busy is true when the Project already has an operation in flight, so the intent was
	// not established and the caller should commit, keep the run queued, and retry later.
	Busy bool
}

// UnavailableAgentRunControlPlane is the fail-closed default for a Store with no
// control-plane A implementation wired. Every method returns an explicit not-implemented
// error so no business transaction can fabricate a run Workspace or execution_work item
// while the controller integration is unimplemented (A skeleton).
type UnavailableAgentRunControlPlane struct{}

func (UnavailableAgentRunControlPlane) CreateRunWorkspace(t *transaction, run Object) (RunWorkspaceOutcome, error) {
	return RunWorkspaceOutcome{}, fmt.Errorf("control-plane seam createRunWorkspace not implemented: A side")
}

func (UnavailableAgentRunControlPlane) DeleteRunWorkspace(t *transaction, run Object) error {
	return fmt.Errorf("control-plane seam deleteRunWorkspace not implemented: A side")
}

func (UnavailableAgentRunControlPlane) EnqueueExecutionWork(t *transaction, run Object, kind string, input, target Object, availableAt *time.Time) (string, error) {
	return "", fmt.Errorf("control-plane seam enqueueExecutionWork not implemented: A side")
}

func (UnavailableAgentRunControlPlane) EnqueueThreadCommand(t *transaction, run, command Object) (string, error) {
	return "", fmt.Errorf("control-plane seam enqueueThreadCommand not implemented: A side")
}

// AgentSessionWork is the B-side AgentSession payload for an 'agent_session'
// execution_work item (D6 AgentSession). It fixes the plugin identity/version from the
// run-create snapshot (IssueRun D1/D6) and carries the initial turn; the control plane
// must consume the plugin identity from here and never re-read the current
// space_agents/space_plugins version to override it. checkout_execution_id and
// git_identity are control-plane-populated after delivery, so they are not part of this
// B-side set (the Thread AgentSession itself is a later slice).
type AgentSessionWork struct {
	AgentPluginID      string
	AgentPluginVersion string
	InitialTurn        Object
}

// inputObject renders the work as the JSONBObject the control plane stores in
// execution_work.input for kind='agent_session'.
func (w AgentSessionWork) inputObject() Object {
	return Object{
		"agent_plugin_id":      w.AgentPluginID,
		"agent_plugin_version": w.AgentPluginVersion,
		"initial_turn":         w.InitialTurn,
	}
}
