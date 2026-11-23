package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/taskworkflow"
)

func (h *handler) taskWorkflowRoutes(rt *router) {
	base := "/api/v2/tenants/{tid}/tasks/{taskid}/workflow-runs"
	rt.tenant("GET "+base, h.listTaskWorkflows)
	rt.tenant("POST "+base, h.startTaskWorkflow)
}

func taskWorkflowFault(w http.ResponseWriter, err error) {
	var input *taskworkflow.StartInputError
	var hostInput *WorkflowRunInputInvalid
	switch {
	case errors.As(err, &hostInput):
		workflowRunLifecycleFault(w, err, "task workflow")
	case errors.As(err, &input):
		workflowRunLifecycleFault(w, &WorkflowRunInputInvalid{Variable: input.Variable, Reason: input.Reason}, "task workflow")
	case errors.Is(err, taskworkflow.ErrStartGraph):
		fault(w, 400, "workflow_run_start_failed")
	case errors.Is(err, store.ErrConflict):
		fault(w, 409, "version_conflict")
	case errors.Is(err, store.ErrTaskNotRunnable):
		fault(w, 409, "not_agent_task")
	case errors.Is(err, store.ErrTaskNotAssignee):
		fault(w, http.StatusConflict, "not_assignee")
	case errors.Is(err, store.ErrRunActive):
		fault(w, http.StatusConflict, "run_active")
	case errors.Is(err, store.ErrInvalidTransition):
		fault(w, http.StatusConflict, "task_closed")
	case errors.Is(err, store.ErrNotFound):
		fault(w, http.StatusNotFound, "not_found")
	case errors.Is(err, ErrWorkflowRunNotStartable), errors.Is(err, ErrWorkflowRunNotEditable),
		errors.Is(err, ErrWorkflowRoleNotFound), errors.Is(err, ErrWorkflowSkillNotFound):
		workflowRunLifecycleFault(w, err, "task workflow")
	default:
		workflowRunFault(w, err, "task workflow")
	}
}

func (h *handler) listTaskWorkflows(w http.ResponseWriter, r *http.Request, a access) {
	if h.opts.TaskWorkflows == nil {
		fault(w, 503, "workflow_run_host_unavailable")
		return
	}
	if !taskUUID.MatchString(r.PathValue("taskid")) {
		fault(w, 404, "not_found")
		return
	}
	items, err := h.opts.TaskWorkflows.List(r.Context(), a.TenantID, r.PathValue("taskid"))
	if err != nil {
		taskWorkflowFault(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (h *handler) startTaskWorkflow(w http.ResponseWriter, r *http.Request, a access) {
	if h.opts.TaskWorkflows == nil {
		fault(w, 503, "workflow_run_host_unavailable")
		return
	}
	if !taskUUID.MatchString(r.PathValue("taskid")) {
		fault(w, 404, "not_found")
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkflowID string                     `json:"workflowId"`
		SnapshotID string                     `json:"snapshotId"`
		Input      *string                    `json:"input"`
		Values     map[string]json.RawMessage `json:"values"`
	}
	if !decodeStrict(w, r, &in) {
		return
	}
	if !workflowUUID.MatchString(in.WorkflowID) || (in.SnapshotID != "" && !workflowUUID.MatchString(in.SnapshotID)) {
		fault(w, 400, "invalid_workflow")
		return
	}
	id, err := h.opts.TaskWorkflows.Start(r.Context(), store.NewTaskWorkflow{TenantID: a.TenantID, TaskID: r.PathValue("taskid"), UserID: a.UserID, Key: key, WorkflowID: in.WorkflowID, SnapshotID: in.SnapshotID, Input: in.Input}, in.Values)
	if err != nil {
		taskWorkflowFault(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"workflowRunId": id})
}
