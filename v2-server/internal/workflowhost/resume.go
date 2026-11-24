package workflowhost

import (
	"context"
	"errors"
	"fmt"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/workflow"
)

var _ workflow.ResumeRepository = repository{}
var _ server.WorkflowRunResumer = (*Host)(nil)

func resumeState(st store.WorkflowResumeState) workflow.ResumeState {
	nodes := make([]workflow.NodeRun, len(st.Nodes))
	for i, n := range st.Nodes {
		nodes[i] = nodeRun(n)
	}
	return workflow.ResumeState{Status: st.Status, Nodes: nodes, FormNodeIDs: st.FormNodeIDs, SubmittedFormNodeIDs: st.SubmittedFormNodeIDs,
		TaskHandling: st.TaskLink == store.TaskLinkHandle, TaskDecomposition: st.TaskLink == store.TaskLinkDecomposition}
}

func (r repository) ResumeState(ctx context.Context, runID string) (workflow.ResumeState, error) {
	st, err := r.s.WorkflowResumeState(ctx, runID)
	return resumeState(st), err
}

func (r repository) ResumeRun(ctx context.Context, runID, actor string, plan func(workflow.ResumeState) ([]string, error), clear func([]byte, []string) ([]byte, error)) error {
	return r.s.ResumeWorkflowEngineRun(ctx, runID, actor, func(st store.WorkflowResumeState) ([]string, error) {
		return plan(resumeState(st))
	}, clear)
}

// PreviewRunResume plans a resume from failure of a run in the caller's tenant without changing it.
func (h *Host) PreviewRunResume(ctx context.Context, scope server.WorkflowRunScope, runID string) (server.WorkflowRunResumePreview, error) {
	if _, err := h.store.WorkflowRun(ctx, scope.TenantID, runID); err != nil {
		return server.WorkflowRunResumePreview{}, err
	}
	plan, err := h.engine.PreviewResume(ctx, runID)
	if err != nil {
		return server.WorkflowRunResumePreview{}, err
	}
	// The rows allow it, but the Agents would execute under a starter who has left the workspace,
	// or this process still starts or ends a turn of the run: say so now rather than let the
	// resume be refused, or its Agents fail, after the user confirmed it.
	if plan.Refusal == "" {
		if _, available, err := h.resumeStarter(ctx, scope, runID); err != nil {
			return server.WorkflowRunResumePreview{}, err
		} else if !available {
			plan.Refusal = workflow.ResumeRefusedStarterUnavailable
		}
	}
	if plan.Refusal == "" && h.settling(runID) {
		plan.Refusal = workflow.ResumeRefusedSettling
	}
	failed := make([]server.WorkflowRunResumeFailedNode, len(plan.Failed))
	for i, n := range plan.Failed {
		failed[i] = server.WorkflowRunResumeFailedNode{NodeID: n.NodeID, NodeRunID: n.NodeRunID, Status: n.Status, ContainerNodeID: n.Container}
	}
	return server.WorkflowRunResumePreview{
		Reason: plan.Refusal, RunningNodeIDs: plan.Running, FailedNodes: failed,
		RestartContainerNodeIDs: plan.Containers, ClearNodeIDs: plan.Clear,
	}, nil
}

// ResumeRun resumes a failed or cancelled run of the caller's tenant from its failed nodes, as
// desktop's resume_from_failure with the keep-files mode: the workspace is left as it is. The
// permission is the cancellation's. The resumed Agents execute under the run's persisted starter,
// who owns every earlier session of the run, so the run's sessions keep one owner. A starter who
// is no longer an active member of the workspace refuses the resume as starter_unavailable rather
// than let every resumed Agent fail under their identity. When the run has no recorded starter
// yet, an active tenant member who already owns a bound node session is preferred, otherwise the
// caller is used, and the starter is persisted only inside the resume transaction if the resume
// commits. A run whose resume this process already admitted keeps that admission's owner while
// that owner is still an active member: a resume that failed before committing leaves its state
// behind, and the later caller then backfills that owner, so the recorded starter is the user the
// resumed sessions run under.
func (h *Host) ResumeRun(ctx context.Context, scope server.WorkflowRunScope, runID string) (store.WorkflowRun, error) {
	run, err := h.store.WorkflowRun(ctx, scope.TenantID, runID)
	if err != nil {
		return store.WorkflowRun{}, err
	}
	if run.Status != store.WorkflowStatusFailed && run.Status != store.WorkflowStatusCancelled {
		return store.WorkflowRun{}, &server.WorkflowRunNotResumable{Reason: workflow.ResumeRefusedStatus}
	}
	// A cancellation still under way (waiting for its turn to stop) owns the run until it closes.
	unlock, ok := h.tryLockLifecycle(runID)
	if !ok {
		return store.WorkflowRun{}, &server.WorkflowRunNotResumable{Reason: workflow.ResumeRefusedSettling, RunningNodeIDs: []string{}}
	}
	defer unlock()
	owner, available, err := h.resumeStarter(ctx, scope, runID)
	if err != nil {
		return store.WorkflowRun{}, err
	}
	if !available {
		return store.WorkflowRun{}, &server.WorkflowRunNotResumable{Reason: workflow.ResumeRefusedStarterUnavailable, RunningNodeIDs: []string{}}
	}
	if err := h.checkCompletionTool(ctx, execution.Scope{TenantID: scope.TenantID, UserID: owner}, run); err != nil {
		return store.WorkflowRun{}, err
	}
	// A run that a sibling failed may still have interactive nodes parked in this process, their
	// rows pending and their sessions open. Close them before planning, so the resume restarts
	// them like any other cancelled node instead of racing the sweep that would end the session
	// of a node the resumed run still showed as awaiting input.
	if st := h.state(runID); st != nil {
		h.releaseOrphans(st, runID)
	}
	// A state an earlier admitted resume left behind keeps its owner only while that owner may
	// still act in the workspace; one who left is replaced by this resume's owner (admitResume).
	stale := ""
	if left := h.state(runID); left != nil {
		left.mu.Lock()
		leftOwner := left.scope.UserID
		left.mu.Unlock()
		if leftOwner != owner {
			active, err := h.activeMember(ctx, scope.TenantID, leftOwner)
			if err != nil {
				return store.WorkflowRun{}, fmt.Errorf("check the execution owner of workflow run %s: %w", runID, err)
			}
			if !active {
				stale = leftOwner
			}
		}
	}
	st, ok := h.admitResume(runID, execution.Scope{TenantID: scope.TenantID, UserID: owner}, stale)
	if !ok {
		// The rows may name a rule that refuses anyway; otherwise the bridge is still settling a
		// turn (for example a cancelled turn that has not finished ending) and the user retries.
		refused := &server.WorkflowRunNotResumable{Reason: workflow.ResumeRefusedSettling, RunningNodeIDs: []string{}}
		if plan, err := h.engine.PreviewResume(ctx, runID); err == nil && plan.Refusal != "" {
			refused.Reason, refused.RunningNodeIDs = plan.Refusal, plan.Running
		}
		return store.WorkflowRun{}, refused
	}
	if h.resuming != nil {
		h.resuming(runID)
	}
	// Like a start, the resumed run outlives the request. The Agents execute under this process's
	// state, whose owner an earlier admitted resume may have left behind: backfill that owner, not
	// this caller, or the recorded starter and the sessions the resumed Agents open disagree and
	// the run's earlier sessions become unreadable.
	err = h.engine.ResumeFromFailure(context.WithoutCancel(ctx), runID, st.scope.UserID)
	st.started()
	h.forgetFinished(runID, st)
	if err != nil {
		var refusal *workflow.ResumeRefusal
		if errors.As(err, &refusal) {
			return store.WorkflowRun{}, &server.WorkflowRunNotResumable{Reason: refusal.Reason, RunningNodeIDs: refusal.Running}
		}
		if errors.Is(err, store.ErrWorkflowRunState) {
			return store.WorkflowRun{}, &server.WorkflowRunNotResumable{Reason: workflow.ResumeRefusedNothing}
		}
		return store.WorkflowRun{}, err
	}
	return h.store.WorkflowRun(ctx, scope.TenantID, runID)
}

// settling reports whether this process is still starting or executing an Agent turn of the run.
func (h *Host) settling(runID string) bool {
	st := h.state(runID)
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.attempts) != 0 || st.starters != 0 {
		return true
	}
	// A completion still settling an interactive node makes admitResume refuse as well. Nodes
	// merely parked under the stopped run do not: the resume closes them itself.
	for _, n := range st.interactive {
		if n.phase == phaseCompleting {
			return true
		}
	}
	return false
}

// admitResume registers the resumed run's execution owner and counts the resume as a start under
// way. A run with an Agent start or turn still in flight in this process is refused, and so is
// one that still tracks an interactive node: a completion is settling it (ResumeRun released the
// parked ones just before). A leftover
// state is reused with its cancellation request cleared, because the run it belonged to has
// closed, with its owner kept, because its Agents and sessions belong to that identity, and with
// its generation advanced: a dispatch made before the resume carries the old
// generation, so neither its attempt goroutine nor beginStart lets it start a turn, even when it was already past its abandon check and resolving
// its executor while the run was cancelled and resumed.
//
// stale names a leftover owner who is no longer an active member of the workspace. Executing under
// that owner would fail every Agent of the resumed run and record a starter nobody can resume
// under, so an idle leftover state (no attempt goroutine and no unconfirmed session End under the
// old owner) takes scope's owner instead; one that is not idle yet is refused, and the user retries
// once it settled.
func (h *Host) admitResume(runID string, scope execution.Scope, stale string) (*runState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.runs[runID]
	if st == nil {
		st = &runState{scope: scope}
		h.runs[runID] = st
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.attempts) != 0 || st.starters != 0 || len(st.interactive) != 0 {
		return nil, false
	}
	if stale != "" && st.scope.UserID == stale {
		if st.dispatched != 0 || len(st.unended) != 0 {
			return nil, false
		}
		st.scope = scope
	}
	st.cancelling = false
	st.generation++
	st.starters++
	return st, true
}

// resumeStarter returns who a resume of the run would execute under, and whether that user may
// still act in the caller's workspace. A recorded starter is kept even after they left (the
// resume is then refused, never moved to someone else); a legacy run without one prefers an
// active member owning an earlier node session, otherwise the caller.
func (h *Host) resumeStarter(ctx context.Context, scope server.WorkflowRunScope, runID string) (string, bool, error) {
	owner, err := h.store.WorkflowResumeStarter(ctx, runID)
	if err != nil {
		return "", false, fmt.Errorf("read workflow run starter: %w", err)
	}
	if owner == "" {
		return scope.UserID, true, nil
	}
	active, err := h.activeMember(ctx, scope.TenantID, owner)
	if err != nil {
		return "", false, fmt.Errorf("check workflow run starter: %w", err)
	}
	return owner, active, nil
}
