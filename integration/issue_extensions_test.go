package integration

import (
	"context"
	"testing"

	"github.com/wanglongan587/cloud/internal/core"
)

func TestIssueStatusCatalog(t *testing.T) {
	f := setup(t)

	// First read lazily seeds the 7 canonical columns.
	catalog := issueItems(f.call("GET", f.path("/issue-statuses"), nil, "", 200))
	if len(catalog) != 7 {
		t.Fatalf("canonical catalog size %d != 7", len(catalog))
	}
	if !catalog[0].B("isSystem") || catalog[0].S("key") != "backlog" {
		t.Fatalf("unexpected first column: %v", catalog[0])
	}

	// Create a custom column.
	created := f.call("POST", f.path("/issue-statuses"), core.Object{"key": "review", "name": "Review", "category": "started", "color": "#ff0"}, "st-1", 200).O("resource")
	if created.S("key") != "review" || created.B("isSystem") {
		t.Fatalf("unexpected custom column: %v", created)
	}
	if len(issueItems(f.call("GET", f.path("/issue-statuses"), nil, "", 200))) != 8 {
		t.Fatal("custom column not listed")
	}

	// Invalid key and duplicate key.
	f.call("POST", f.path("/issue-statuses"), core.Object{"key": "Bad Key", "name": "X"}, "st-2", 400)
	f.call("POST", f.path("/issue-statuses"), core.Object{"key": "review", "name": "X"}, "st-3", 409)

	// Update name; version precondition enforced.
	renamed := f.call("PUT", f.path("/issue-statuses/"+created.S("id")), core.Object{"name": "Reviewed", "version": created.N("version")}, "", 200)
	if renamed.S("name") != "Reviewed" || renamed.N("version") != created.N("version")+1 {
		t.Fatalf("status rename not applied: %v", renamed)
	}
	f.call("PUT", f.path("/issue-statuses/"+created.S("id")), core.Object{"name": "No version"}, "", 428)

	// System columns cannot be archived.
	system := catalog[0]
	f.call("DELETE", f.path("/issue-statuses/"+system.S("id")), core.Object{"version": system.N("version")}, "st-del-sys", 409)

	// Custom column can be archived, and is then unusable on issues.
	archived := f.call("DELETE", f.path("/issue-statuses/"+created.S("id")), core.Object{"version": renamed.N("version")}, "st-del", 200)
	if archived["deletedAt"] == nil {
		t.Fatalf("archive did not set deletedAt: %v", archived)
	}
	f.call("POST", f.path("/issues"), core.Object{"title": "X", "status": "review"}, "st-use", 400)
}

func TestIssueComments(t *testing.T) {
	f := setup(t)
	issue := f.call("POST", f.path("/issues"), core.Object{"title": "Discuss"}, "c-issue", 200).O("resource")

	comment := f.call("POST", f.path("/issues/"+issue.S("id")+"/comments"), core.Object{"body": "first"}, "c-1", 200).O("resource")
	if comment.S("body") != "first" || comment.S("authorUserId") != f.uid {
		t.Fatalf("unexpected comment: %v", comment)
	}

	list := f.call("GET", f.path("/issues/"+issue.S("id")+"/comments"), nil, "", 200)
	if len(issueItems(list)) != 1 {
		t.Fatalf("comment list size != 1")
	}

	updated := f.call("PUT", f.path("/issues/"+issue.S("id")+"/comments/"+comment.S("id")), core.Object{"body": "edited", "version": comment.N("version")}, "", 200)
	if updated.S("body") != "edited" {
		t.Fatalf("comment edit not applied: %v", updated)
	}
	f.call("PUT", f.path("/issues/"+issue.S("id")+"/comments/"+comment.S("id")), core.Object{"body": "stale", "version": comment.N("version")}, "", 409)

	del := f.call("DELETE", f.path("/issues/"+issue.S("id")+"/comments/"+comment.S("id")), core.Object{"version": updated.N("version")}, "c-del", 200)
	if del["deletedAt"] == nil {
		t.Fatalf("comment delete did not set deletedAt: %v", del)
	}
	if len(issueItems(f.call("GET", f.path("/issues/"+issue.S("id")+"/comments"), nil, "", 200))) != 0 {
		t.Fatal("deleted comment still listed")
	}

	// Cross-tenant isolation.
	other, e := f.store.Bootstrap(context.Background(), "Other", "corp", "other", "Other")
	must(t, e)
	original := f.tid
	f.tid = other.S("tenantId")
	f.call("GET", f.path("/issues/"+issue.S("id")+"/comments"), nil, "", 403)
	f.tid = original
}

func TestIssueLabels(t *testing.T) {
	f := setup(t)
	issue := f.call("POST", f.path("/issues"), core.Object{"title": "Label me"}, "l-issue", 200).O("resource")

	label := f.call("POST", f.path("/labels"), core.Object{"name": "bug", "color": "#f00"}, "l-1", 200).O("resource")
	if label.S("name") != "bug" {
		t.Fatalf("unexpected label: %v", label)
	}
	f.call("POST", f.path("/labels"), core.Object{"name": "bug"}, "l-2", 409)

	if len(issueItems(f.call("GET", f.path("/labels"), nil, "", 200))) != 1 {
		t.Fatal("label not listed")
	}

	// Attach → the issue embeds it; detach → removed.
	f.call("POST", f.path("/issues/"+issue.S("id")+"/labels"), core.Object{"labelId": label.S("id")}, "l-attach", 200)
	with := f.call("GET", f.path("/issues/"+issue.S("id")), nil, "", 200)
	if len(with["labels"].([]any)) != 1 {
		t.Fatalf("issue did not embed label: %v", with)
	}
	if len(issueItems(f.call("GET", f.path("/issues/"+issue.S("id")+"/labels"), nil, "", 200))) != 1 {
		t.Fatal("issue label list empty after attach")
	}

	f.call("DELETE", f.path("/issues/"+issue.S("id")+"/labels/"+label.S("id")), core.Object{}, "l-detach", 200)
	after := f.call("GET", f.path("/issues/"+issue.S("id")), nil, "", 200)
	if len(after["labels"].([]any)) != 0 {
		t.Fatalf("detach did not remove label: %v", after)
	}

	// Rename and delete the label.
	renamed := f.call("PUT", f.path("/labels/"+label.S("id")), core.Object{"name": "defect", "color": "#00f", "version": label.N("version")}, "", 200)
	if renamed.S("name") != "defect" || renamed.S("color") != "#00f" {
		t.Fatalf("label update not applied: %v", renamed)
	}
	f.call("DELETE", f.path("/labels/"+label.S("id")), core.Object{"version": renamed.N("version")}, "l-del", 200)
	if len(issueItems(f.call("GET", f.path("/labels"), nil, "", 200))) != 0 {
		t.Fatal("deleted label still listed")
	}
}

func TestIssueNumbersPropertiesSearchBatch(t *testing.T) {
	f := setup(t)

	// Per-tenant numbers increment 1,2,... and properties round-trip.
	one := f.call("POST", f.path("/issues"), core.Object{"title": "Alpha", "properties": core.Object{"storyPoints": 3}}, "n-1", 200).O("resource")
	two := f.call("POST", f.path("/issues"), core.Object{"title": "Beta"}, "n-2", 200).O("resource")
	if one.N("number") != 1 || two.N("number") != 2 {
		t.Fatalf("numbers wrong: %d %d", one.N("number"), two.N("number"))
	}
	if one.O("properties").N("storyPoints") != 3 {
		t.Fatalf("properties not stored: %v", one.O("properties"))
	}

	// Search hits title (Alpha) and description.
	desc := f.call("POST", f.path("/issues"), core.Object{"title": "Gamma", "description": "mentions alpha here"}, "n-3", 200).O("resource")
	hits := issueItems(f.call("GET", f.path("/issues")+"?q=alpha", nil, "", 200))
	if len(hits) != 2 {
		t.Fatalf("search should hit 2, got %d", len(hits))
	}
	miss := issueItems(f.call("GET", f.path("/issues")+"?q=zzz", nil, "", 200))
	if len(miss) != 0 {
		t.Fatal("search should miss")
	}

	// Batch update status + priority across several issues.
	batch := f.call("POST", f.path("/issues/batch"), core.Object{"ids": []string{one.S("id"), two.S("id")}, "status": "done", "priority": "low"}, "batch-1", 200)
	batchItems := issueItems(batch)
	if len(batchItems) != 2 {
		t.Fatalf("batch returned %d != 2", len(batchItems))
	}
	for _, it := range batchItems {
		if it.S("status") != "done" || it.S("priority") != "low" {
			t.Fatalf("batch patch not applied: %v", it)
		}
	}
	_ = desc
	// Invalid id in batch → 404, invalid ids shape → 400.
	f.call("POST", f.path("/issues/batch"), core.Object{"ids": []string{"not-a-uuid"}, "status": "todo"}, "batch-2", 400)
}

func TestIssueViewsGroupsSubscribers(t *testing.T) {
	f := setup(t)
	issue := f.call("POST", f.path("/issues"), core.Object{"title": "Watch", "priority": "high"}, "g-1", 200).O("resource")

	// Subscribers: subscribe, list, unsubscribe.
	f.call("POST", f.path("/issues/"+issue.S("id")+"/subscribers"), core.Object{"userId": f.uid}, "sub-1", 200)
	subs := issueItems(f.call("GET", f.path("/issues/"+issue.S("id")+"/subscribers"), nil, "", 200))
	if len(subs) != 1 || subs[0].S("displayName") != "Alice" {
		t.Fatalf("subscriber list wrong: %v", subs)
	}
	f.call("DELETE", f.path("/issues/"+issue.S("id")+"/subscribers"), core.Object{"userId": f.uid}, "sub-2", 200)
	if len(issueItems(f.call("GET", f.path("/issues/"+issue.S("id")+"/subscribers"), nil, "", 200))) != 0 {
		t.Fatal("unsubscribe did not remove")
	}

	// Saved views: create/list/update/delete.
	view := f.call("POST", f.path("/issue-views"), core.Object{"name": "My bugs", "filter": core.Object{"priority": "high"}}, "v-1", 200).O("resource")
	if view.S("name") != "My bugs" || view.O("filter").S("priority") != "high" {
		t.Fatalf("view not stored: %v", view)
	}
	if len(issueItems(f.call("GET", f.path("/issue-views"), nil, "", 200))) != 1 {
		t.Fatal("view not listed")
	}
	upd := f.call("PUT", f.path("/issue-views/"+view.S("id")), core.Object{"name": "Renamed", "version": view.N("version")}, "", 200)
	if upd.S("name") != "Renamed" {
		t.Fatalf("view update failed: %v", upd)
	}
	f.call("DELETE", f.path("/issue-views/"+view.S("id")), core.Object{"version": upd.N("version")}, "v-del", 200)

	// Grouped view by priority.
	f.call("POST", f.path("/issues"), core.Object{"title": "Low priority", "priority": "low"}, "g-2", 200)
	groups := f.call("GET", f.path("/issue-groups")+"?by=priority", nil, "", 200)
	rawGroups, _ := groups["groups"].([]any)
	if len(rawGroups) < 2 {
		t.Fatalf("expected >=2 priority groups, got %d", len(rawGroups))
	}
}
