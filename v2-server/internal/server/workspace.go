package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/workspacefs"
)

// The workspace routes are outside api/openapi.json; workspace.openapi.json describes them. They
// read the checkout behind a conversation, a workflow run or a task run, read-only, for the side
// panel.
type workspaceStore interface {
	ChatConversation(ctx context.Context, tenant, user, id string) (store.ChatConversation, error)
	ChatWorkspace(ctx context.Context, tenant, user, conversationID string) (store.WorkspaceLocation, error)
	WorkflowRunWorkspace(ctx context.Context, tenant, runID string) (store.WorkspaceLocation, error)
	TaskRunWorkspace(ctx context.Context, tenant, taskID, runID string) (store.WorkspaceLocation, error)
}

// locate finds the checkout a request names, after its own resource checks. It writes the response
// itself and returns false when there is nothing to read.
type locate func(http.ResponseWriter, *http.Request, access) (workspacefs.Workspace, bool)

func (h *handler) workspaceRoutes(rt *router) {
	for _, r := range []struct {
		base string
		find locate
	}{
		{chatBase + "/conversations/{cid}/workspace", h.chatWorkspace},
		{"/api/v2/tenants/{tid}/workflow-runs/{rid}/workspace", h.workflowRunWorkspace},
		{"/api/v2/tenants/{tid}/tasks/{taskid}/runs/{rid}/workspace", h.taskRunWorkspace},
	} {
		rt.tenant("GET "+r.base+"/entries", workspaceRead(r.find, func(_ context.Context, w workspacefs.Workspace, p string) (any, error) { return w.List(p) }))
		rt.tenant("GET "+r.base+"/file", workspaceRead(r.find, func(_ context.Context, w workspacefs.Workspace, p string) (any, error) { return w.Read(p) }))
		rt.tenant("GET "+r.base+"/changes", workspaceRead(r.find, func(c context.Context, w workspacefs.Workspace, _ string) (any, error) { return w.Changes(c) }))
		rt.tenant("GET "+r.base+"/diff", workspaceRead(r.find, func(c context.Context, w workspacefs.Workspace, p string) (any, error) { return w.Diff(c, p) }))
	}
}

func workspaceRead(find locate, read func(context.Context, workspacefs.Workspace, string) (any, error)) func(http.ResponseWriter, *http.Request, access) {
	return func(w http.ResponseWriter, r *http.Request, a access) {
		ws, ok := find(w, r, a)
		if !ok {
			return
		}
		out, e := read(r.Context(), ws, r.URL.Query().Get("path"))
		if e != nil {
			workspaceFault(w, e)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (h *handler) chatWorkspace(w http.ResponseWriter, r *http.Request, a access) (ws workspacefs.Workspace, found bool) {
	h.chat(func(w http.ResponseWriter, r *http.Request, c chatRequest) {
		s, ok := h.store.(workspaceStore)
		if !ok {
			fault(w, http.StatusServiceUnavailable, "workspace_unavailable")
			return
		}
		if _, e := s.ChatConversation(r.Context(), c.scope.TenantID, c.scope.UserID, r.PathValue("cid")); e != nil {
			chatFault(w, "chat workspace conversation", e)
			return
		}
		at, e := s.ChatWorkspace(r.Context(), c.scope.TenantID, c.scope.UserID, r.PathValue("cid"))
		ws, found = workspaceAt(w, at, e)
	})(w, r, a)
	return ws, found
}

func (h *handler) workflowRunWorkspace(w http.ResponseWriter, r *http.Request, a access) (workspacefs.Workspace, bool) {
	runs, tenant, ok := h.workflowRunScope(w, a)
	if !ok {
		return workspacefs.Workspace{}, false
	}
	s, ok := h.store.(workspaceStore)
	if !ok {
		fault(w, http.StatusServiceUnavailable, "workspace_unavailable")
		return workspacefs.Workspace{}, false
	}
	runID := r.PathValue("rid")
	if !workflowUUID.MatchString(runID) {
		fault(w, http.StatusNotFound, "workflow_run_not_found")
		return workspacefs.Workspace{}, false
	}
	if _, e := runs.WorkflowRun(r.Context(), tenant, runID); e != nil {
		workflowRunFault(w, e, "workflow run workspace")
		return workspacefs.Workspace{}, false
	}
	at, e := s.WorkflowRunWorkspace(r.Context(), tenant, runID)
	return workspaceAt(w, at, e)
}

func (h *handler) taskRunWorkspace(w http.ResponseWriter, r *http.Request, a access) (workspacefs.Workspace, bool) {
	host, scope, id, ok := h.taskRunScope(w, r, a)
	if !ok {
		return workspacefs.Workspace{}, false
	}
	s, ok := h.store.(workspaceStore)
	if !ok {
		fault(w, http.StatusServiceUnavailable, "workspace_unavailable")
		return workspacefs.Workspace{}, false
	}
	runID := r.PathValue("rid")
	if _, e := host.Run(r.Context(), scope, id, runID); e != nil {
		taskRunFault(w, "task run workspace", e)
		return workspacefs.Workspace{}, false
	}
	at, e := s.TaskRunWorkspace(r.Context(), scope.TenantID, id, runID)
	return workspaceAt(w, at, e)
}

// workspaceAt turns a recorded location into a Workspace, or answers that there is none yet: the
// first turn has not cloned its checkout, or no Agent node of the run has started.
func workspaceAt(w http.ResponseWriter, at store.WorkspaceLocation, e error) (workspacefs.Workspace, bool) {
	if errors.Is(e, store.ErrNotFound) {
		fault(w, http.StatusNotFound, "workspace_not_ready")
		return workspacefs.Workspace{}, false
	}
	if e != nil {
		internal(w, "read workspace location", e)
		return workspacefs.Workspace{}, false
	}
	return workspacefs.Workspace{Root: at.Path, BaseCommit: at.BaseCommit}, true
}

func workspaceFault(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, workspacefs.ErrGone):
		fault(w, http.StatusGone, "workspace_removed")
	case errors.Is(e, workspacefs.ErrInvalidPath):
		fault(w, http.StatusBadRequest, "invalid_path")
	case errors.Is(e, workspacefs.ErrNotFound):
		fault(w, http.StatusNotFound, "path_not_found")
	case errors.Is(e, workspacefs.ErrNotDirectory):
		fault(w, http.StatusBadRequest, "not_a_directory")
	case errors.Is(e, workspacefs.ErrNotFile):
		fault(w, http.StatusBadRequest, "not_a_file")
	case errors.Is(e, workspacefs.ErrBinary):
		fault(w, http.StatusUnprocessableEntity, "binary_file")
	case errors.Is(e, workspacefs.ErrTooLarge):
		fault(w, http.StatusRequestEntityTooLarge, "too_large")
	default:
		internal(w, "read workspace", e)
	}
}
