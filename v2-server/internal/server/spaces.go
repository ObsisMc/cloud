package server

import (
	"net/http"
)

// spaces returns the caller's workspace spaces in one page. v2 has exactly one per tenant, so
// pagination is skipped; the squad member picker reads the first space's plugins.
func (h *handler) spaces(w http.ResponseWriter, r *http.Request, a access) {
	items, e := h.store.TenantSpaces(r.Context(), a.Access)
	if e != nil {
		internal(w, "tenant spaces", e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": ""})
}
