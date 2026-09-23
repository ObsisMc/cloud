package integration

import (
	"context"
	"testing"

	"github.com/wanglongan587/cloud/internal/core"
)

func issueItems(o core.Object) []core.Object {
	raw, _ := o["items"].([]any)
	out := make([]core.Object, 0, len(raw))
	for _, v := range raw {
		out = append(out, core.Object(v.(map[string]any)))
	}
	return out
}

func issuePos(o core.Object) float64 {
	if v, ok := o["position"].(float64); ok {
		return v
	}
	return 0
}

func TestIssueBoardCRUDAndMove(t *testing.T) {
	f := setup(t)

	// Create three issues; each new issue lands at the top of its column.
	first := f.call("POST", f.path("/issues"), core.Object{"title": "First", "status": "todo", "priority": "high"}, "create-1", 200).O("resource")
	second := f.call("POST", f.path("/issues"), core.Object{"title": "Second", "status": "todo"}, "create-2", 200).O("resource")
	third := f.call("POST", f.path("/issues"), core.Object{"title": "Third", "status": "in_progress"}, "create-3", 200).O("resource")

	if first.S("status") != "todo" || first.S("priority") != "high" || first.S("creatorUserId") != f.uid {
		t.Fatalf("unexpected created issue: %v", first)
	}
	// todo was empty for "First" (pos 0), then "Second" took the top (pos -1).
	if issuePos(first) != 0 || issuePos(second) != -1 {
		t.Fatalf("column-top positions wrong: first=%v second=%v", issuePos(first), issuePos(second))
	}

	// Board list: canonical column order, then position, then id.
	list := f.call("GET", f.path("/issues"), nil, "", 200)
	board := issueItems(list)
	if len(board) != 3 {
		t.Fatalf("board size %d != 3", len(board))
	}
	if board[0].S("id") != second.S("id") || board[1].S("id") != first.S("id") || board[2].S("id") != third.S("id") {
		t.Fatalf("board order wrong: %v %v %v", board[0].S("title"), board[1].S("title"), board[2].S("title"))
	}

	// Get single issue.
	got := f.call("GET", f.path("/issues/"+first.S("id")), nil, "", 200)
	if got.S("title") != "First" {
		t.Fatalf("get wrong issue: %v", got)
	}

	// Idempotent replay returns the same issue id.
	replay := f.call("POST", f.path("/issues"), core.Object{"title": "First", "status": "todo", "priority": "high"}, "create-1", 200)
	if replay.O("resource").S("id") != first.S("id") {
		t.Fatalf("idempotent create made a new issue: %v", replay)
	}
	// Same key, changed body → conflict.
	f.call("POST", f.path("/issues"), core.Object{"title": "Changed"}, "create-1", 409)

	// Partial update: change priority + title.
	updated := f.call("PUT", f.path("/issues/"+second.S("id")), core.Object{"title": "Second revised", "priority": "low", "version": second.N("version")}, "", 200)
	if updated.S("title") != "Second revised" || updated.S("priority") != "low" || updated.N("version") != second.N("version")+1 {
		t.Fatalf("update not applied: %v", updated)
	}
	// Missing version ⇒ 428 precondition.
	f.call("PUT", f.path("/issues/"+second.S("id")), core.Object{"title": "No version"}, "", 428)
	// Stale version ⇒ 409 conflict.
	f.call("PUT", f.path("/issues/"+second.S("id")), core.Object{"title": "Stale", "version": second.N("version")}, "", 409)

	// Move "third" (in_progress) into todo with no anchors → re-rank to top of todo (MIN - 1 = -2).
	moved := f.call("POST", f.path("/issues/"+third.S("id")+"/move"), core.Object{"status": "todo", "version": third.N("version")}, "move-1", 200)
	if moved.S("status") != "todo" || issuePos(moved) != -2 {
		t.Fatalf("status-only move did not re-rank to column top: status=%s pos=%v", moved.S("status"), issuePos(moved))
	}

	// Move "first" (todo pos 0) between third(-2) and second(-1) → midpoint -1.5.
	between := f.call("POST", f.path("/issues/"+first.S("id")+"/move"), core.Object{"beforeId": third.S("id"), "afterId": second.S("id"), "version": first.N("version")}, "move-2", 200)
	if issuePos(between) != -1.5 {
		t.Fatalf("midpoint move wrong: pos=%v", issuePos(between))
	}

	// Final board order within todo: third(-2), first(-1.5), second(-1).
	final := issueItems(f.call("GET", f.path("/issues"), nil, "", 200))
	if final[0].S("id") != third.S("id") || final[1].S("id") != first.S("id") || final[2].S("id") != second.S("id") {
		t.Fatalf("post-move board wrong: %v %v %v", issuePos(final[0]), issuePos(final[1]), issuePos(final[2]))
	}

	// Invalid status and invalid anchor are rejected without mutation (using the current version).
	f.call("POST", f.path("/issues/"+first.S("id")+"/move"), core.Object{"status": "nope", "version": between.N("version")}, "move-bad", 400)
	f.call("POST", f.path("/issues/"+first.S("id")+"/move"), core.Object{"beforeId": "not-a-uuid", "version": between.N("version")}, "move-bad2", 400)

	// Soft delete removes it from the board.
	del := f.call("DELETE", f.path("/issues/"+second.S("id")), core.Object{"version": updated.N("version")}, "delete-1", 200)
	if del["deletedAt"] == nil {
		t.Fatalf("delete did not set deletedAt: %v", del)
	}
	f.call("GET", f.path("/issues/"+second.S("id")), nil, "", 404)
	afterDelete := issueItems(f.call("GET", f.path("/issues"), nil, "", 200))
	if len(afterDelete) != 2 {
		t.Fatalf("deleted issue still listed: %d", len(afterDelete))
	}

	// Persistence: rows are real PostgreSQL rows, not in-memory.
	if f.scalar("SELECT count(*) FROM issues WHERE tenant_id=$1 AND deleted_at IS NULL", f.tid) != 2 {
		t.Fatal("issue rows not persisted")
	}
	if f.scalar("SELECT count(*) FROM issues WHERE tenant_id=$1 AND deleted_at IS NOT NULL", f.tid) != 1 {
		t.Fatal("soft-deleted row not persisted")
	}
}

func TestIssueBoardValidationAndIsolation(t *testing.T) {
	f := setup(t)

	// Missing title / invalid enum / invalid assignee.
	f.call("POST", f.path("/issues"), core.Object{"status": "todo"}, "v1", 400)
	f.call("POST", f.path("/issues"), core.Object{"title": "X", "status": "nope"}, "v2", 400)
	f.call("POST", f.path("/issues"), core.Object{"title": "X", "priority": "urgent-mega"}, "v3", 400)
	f.call("POST", f.path("/issues"), core.Object{"title": "X", "assigneeUserId": "nope"}, "v4", 400)

	// Unknown body field is rejected by the allowlist.
	f.call("POST", f.path("/issues"), core.Object{"title": "X", "hacker": "y"}, "v5", 400)

	// A separate tenant cannot read this tenant's issues.
	other, e := f.store.Bootstrap(context.Background(), "Other", "corp", "other", "Other")
	must(t, e)
	issueID := f.call("POST", f.path("/issues"), core.Object{"title": "Private"}, "v6", 200).O("resource").S("id")
	original := f.tid
	f.tid = other.S("tenantId")
	f.call("GET", f.path("/issues/"+issueID), nil, "", 403)
	f.tid = original

	// Parent link: child references parent; deleting parent leaves child (SET NULL).
	parent := f.call("POST", f.path("/issues"), core.Object{"title": "Parent"}, "v7", 200).O("resource")
	child := f.call("POST", f.path("/issues"), core.Object{"title": "Child", "parentIssueId": parent.S("id")}, "v8", 200).O("resource")
	if child.S("parentIssueId") != parent.S("id") {
		t.Fatalf("parent link missing: %v", child)
	}
	// Self-reference update is rejected.
	f.call("PUT", f.path("/issues/"+child.S("id")), core.Object{"parentIssueId": child.S("id"), "version": child.N("version")}, "", 400)
	// Soft-deleting the parent orphans the child (SET NULL), matching Multica's hard-delete cascade.
	f.call("DELETE", f.path("/issues/"+parent.S("id")), core.Object{"version": parent.N("version")}, "v10", 200)
	after := f.call("GET", f.path("/issues/"+child.S("id")), nil, "", 200)
	if after["parentIssueId"] != nil {
		t.Fatalf("parent delete should SET NULL: %v", after)
	}
}
