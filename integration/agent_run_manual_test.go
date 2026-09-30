package integration

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wanglongan587/cloud/internal/core"
)

// manualAgentRun posts a manual IssueRun for an agent and returns the raw status + body so a test can
// assert the exact accepted/rejected contract without the fixture's want-status fatal path.
func (f *fixture) manualAgentRun(t *testing.T, iid, agentID, key string, input core.Object) (int, core.Object) {
	t.Helper()
	body := core.Object{"executorType": "agent", "executorId": agentID}
	if input != nil {
		body["input"] = input
	}
	o, status, e := f.client.Call(context.Background(), "POST", f.path("/issues/"+iid+"/runs"),
		"gateway", core.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "gateway-a"}}, &f.user, key, body)
	must(t, e)
	return status, o
}

// TestManualAgentActiveResolvesAndSnapshots: a manual agent run for an ACTIVE space_agents agent
// succeeds, records the space_agents.id as executor, and pins the authoritative plugin identity/version
// into the run input snapshot.
func TestManualAgentActiveResolvesAndSnapshots(t *testing.T) {
	f := setup(t)
	sid := f.defaultSpaceID()
	agentID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	f.seedSpaceAgent(t, sid, f.tid, agentID, "official/hello-world", "Hello World", "active", "1.0.0")

	iid := f.issueForAgent(t, "Manual agent", "", "m")
	status, body := f.manualAgentRun(t, iid, agentID, "m-1", nil)
	if status != 200 {
		t.Fatalf("active agent manual run status = %d: %v", status, body)
	}
	run := f.singleRun(t, iid)
	if run.S("executorType") != "agent" || run.S("executorId") != agentID {
		t.Fatalf("run must reference the space_agents.id as executor: %v", run)
	}
	in := run.O("input")
	if in.S("agentPluginId") != "official/hello-world" || in.S("agentPluginVersion") != "1.0.0" {
		t.Fatalf("manual agent run must snapshot authoritative plugin identity/version: %v", in)
	}
}

// TestManualAgentSnapshotNotSpoofable: a caller-supplied agentPluginId/agentPluginVersion in the body
// must never override the authoritative server values; the snapshot is written after (over) the caller
// input, so server fields always win.
func TestManualAgentSnapshotNotSpoofable(t *testing.T) {
	f := setup(t)
	sid := f.defaultSpaceID()
	agentID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	f.seedSpaceAgent(t, sid, f.tid, agentID, "official/hello-world", "Hello World", "active", "1.0.0")

	iid := f.issueForAgent(t, "Spoof attempt", "", "s")
	status, body := f.manualAgentRun(t, iid, agentID, "s-1", core.Object{
		"agentPluginId":      "fake/plugin",
		"agentPluginVersion": "999.0.0",
		"prompt":             "hi",
	})
	if status != 200 {
		t.Fatalf("spoof agent manual run status = %d: %v", status, body)
	}
	in := f.singleRun(t, iid).O("input")
	if in.S("agentPluginId") != "official/hello-world" || in.S("agentPluginVersion") != "1.0.0" {
		t.Fatalf("caller must not override the authoritative snapshot: %v", in)
	}
	if in.S("prompt") != "hi" {
		t.Fatalf("non-authoritative caller input must still be preserved: %v", in)
	}
}

// TestManualAgentSnapshotPinIsImmutable: the run input fixes the plugin version at create time; a later
// upgrade (1.0.0 -> 2.0.0) never rewrites a historical manual run's snapshot, and a NEW manual run pins
// the upgraded version.
func TestManualAgentSnapshotPinIsImmutable(t *testing.T) {
	f := setup(t)
	sid := f.defaultSpaceID()
	agentID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	f.seedSpaceAgent(t, sid, f.tid, agentID, "official/hello-world", "Hello World", "active", "1.0.0")

	iid := f.issueForAgent(t, "Manual pinned", "", "p")
	if status, _ := f.manualAgentRun(t, iid, agentID, "p-1", nil); status != 200 {
		t.Fatalf("first manual run failed")
	}
	if got := f.singleRun(t, iid).O("input").S("agentPluginVersion"); got != "1.0.0" {
		t.Fatalf("first manual run pinned = %q, want 1.0.0", got)
	}

	if _, err := f.store.Pool.Exec(`UPDATE space_plugins SET desired_version='2.0.0',version=version+1 WHERE space_id=$1 AND source_namespace='official' AND identifier='hello-world'`, sid); err != nil {
		t.Fatalf("upgrade plugin: %v", err)
	}
	if _, err := f.store.Pool.Exec(`UPDATE space_agents SET version=version+1 WHERE id=$1`, agentID); err != nil {
		t.Fatalf("refresh agent: %v", err)
	}

	iid2 := f.issueForAgent(t, "Manual pinned 2", "", "p2")
	if status, _ := f.manualAgentRun(t, iid2, agentID, "p2-1", nil); status != 200 {
		t.Fatalf("second manual run failed")
	}
	if got := f.singleRun(t, iid2).O("input").S("agentPluginVersion"); got != "2.0.0" {
		t.Fatalf("second manual run must pin the upgraded version, got %q", got)
	}
	if got := f.singleRun(t, iid).O("input").S("agentPluginVersion"); got != "1.0.0" {
		t.Fatalf("historical manual run must stay pinned at create-time version, got %q", got)
	}
}

// TestManualAgentRetiredRejected: only ACTIVE space_agents resolve; a manual run for a retired agent is
// rejected and leaves no run.
func TestManualAgentRetiredRejected(t *testing.T) {
	f := setup(t)
	sid := f.defaultSpaceID()
	agentID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	f.seedSpaceAgent(t, sid, f.tid, agentID, "official/retired-tool", "Old Tool", "retired", "1.0.0")

	iid := f.issueForAgent(t, "Manual retired", "", "r")
	status, body := f.manualAgentRun(t, iid, agentID, "r-1", nil)
	if status != 404 {
		t.Fatalf("retired agent manual run must be rejected 404, got %d %v", status, body)
	}
	if n := f.scalar("SELECT count(*) FROM issue_runs WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL", iid, f.tid); n != 0 {
		t.Fatalf("retired-agent rejection must leave no run, got %d", n)
	}
}

// TestManualAgentForeignTenantRejected: an agent that lives in another tenant (hence its own space) can
// never be resolved from this tenant, so the manual run is rejected 404 and nothing leaks.
func TestManualAgentForeignTenantRejected(t *testing.T) {
	f := setup(t)
	other := f.createPluginTenant("Other", "other-space", "agent-other")
	otherSID := other.S("id")
	var otherTID string
	must(t, f.store.Pool.QueryRow(`SELECT tenant_id FROM collab_workspaces WHERE id=$1`, otherSID).Scan(&otherTID))
	foreignAgent := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	f.seedSpaceAgent(t, otherSID, otherTID, foreignAgent, "official/foreign", "Foreign", "active", "1.0.0")

	iid := f.issueForAgent(t, "Manual foreign", "", "f")
	if status, body := f.manualAgentRun(t, iid, foreignAgent, "f-1", nil); status != 404 {
		t.Fatalf("foreign-tenant agent manual run must be rejected 404, got %d %v", status, body)
	}
	if n := f.scalar("SELECT count(*) FROM issue_runs WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL", iid, f.tid); n != 0 {
		t.Fatalf("foreign-tenant rejection must leave no run, got %d", n)
	}
}

// TestManualAgentForeignSpaceRejected covers the space dimension explicitly. Under the
// one-space-per-tenant invariant (the ADR and space_test both assert a tenant owns a single
// collaboration space), an agent whose space is not the run tenant's collaboration space is
// necessarily another tenant's agent — the same authoritative space+tenant scoping in
// agentRunEvidence rejects it without leaking existence.
func TestManualAgentForeignSpaceRejected(t *testing.T) {
	f := setup(t)
	other := f.createPluginTenant("Spaced", "spaced-space", "agent-spaced")
	otherSID := other.S("id")
	var otherTID string
	must(t, f.store.Pool.QueryRow(`SELECT tenant_id FROM collab_workspaces WHERE id=$1`, otherSID).Scan(&otherTID))
	foreignSpaceAgent := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	f.seedSpaceAgent(t, otherSID, otherTID, foreignSpaceAgent, "official/foreign", "Foreign Space", "active", "1.0.0")

	iid := f.issueForAgent(t, "Manual foreign space", "", "fs")
	if status, body := f.manualAgentRun(t, iid, foreignSpaceAgent, "fs-1", nil); status != 404 {
		t.Fatalf("foreign-space agent manual run must be rejected 404, got %d %v", status, body)
	}
	if n := f.scalar("SELECT count(*) FROM issue_runs WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL", iid, f.tid); n != 0 {
		t.Fatalf("foreign-space rejection must leave no run, got %d", n)
	}
}

// TestManualAgentUnknownIdRejected: a syntactically valid UUID that is neither an active Space Agent nor
// a resolvable directory agent is rejected 404 with no run and no existence leakage.
func TestManualAgentUnknownIdRejected(t *testing.T) {
	f := setup(t)
	iid := f.issueForAgent(t, "Manual unknown", "", "u")
	ghost := "00000000-0000-4000-8000-000000000001"
	if status, body := f.manualAgentRun(t, iid, ghost, "u-1", nil); status != 404 {
		t.Fatalf("unknown agent manual run must be rejected 404, got %d %v", status, body)
	}
	if n := f.scalar("SELECT count(*) FROM issue_runs WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL", iid, f.tid); n != 0 {
		t.Fatalf("unknown-agent rejection must leave no run, got %d", n)
	}
}
