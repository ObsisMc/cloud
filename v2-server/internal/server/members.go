package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// members returns the workspace roster in one page; v2 workspaces are small enough to skip
// pagination, and the squad picker filters the list client-side.
func (h *handler) members(w http.ResponseWriter, r *http.Request, a access) {
	tenant := a.TenantID
	items, e := h.store.TenantMembers(r.Context(), tenant)
	if e != nil {
		internal(w, "tenant members", e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": ""})
}

// invite adds one member to the workspace roster. It is an owner route; the address alone creates
// the account, so the invitee signs in with that address and nothing else. This route is
// outside Cloud's contract (see README), the v2 stand-in until a roster write reaches the contract.
func (h *handler) invite(w http.ResponseWriter, r *http.Request, a access) {
	tenant := a.TenantID
	var in struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		SquadID     string `json:"squadId"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&in); e != nil {
		fault(w, http.StatusBadRequest, "invalid_json")
		return
	}
	email, e := store.NormalizeEmail(in.Email)
	if e != nil {
		fault(w, http.StatusBadRequest, "invalid_email")
		return
	}
	display := strings.TrimSpace(in.DisplayName)
	if len([]rune(display)) > 64 {
		fault(w, http.StatusBadRequest, "invalid_display_name")
		return
	}
	if in.SquadID != "" && !squadUUID.MatchString(in.SquadID) {
		fault(w, http.StatusNotFound, "not_found")
		return
	}
	m, e := h.store.InviteMember(r.Context(), tenant, store.Invite{Email: email, DisplayName: display, SquadID: in.SquadID})
	switch {
	case errors.Is(e, store.ErrSquadNotFound):
		fault(w, http.StatusNotFound, "not_found")
	case errors.Is(e, store.ErrAlreadyMember):
		fault(w, http.StatusConflict, "member_exists")
	case errors.Is(e, store.ErrUserUnavailable):
		fault(w, http.StatusConflict, "user_unavailable")
	case e != nil:
		internal(w, "invite member", e)
	default:
		writeJSON(w, http.StatusCreated, m)
	}
}
