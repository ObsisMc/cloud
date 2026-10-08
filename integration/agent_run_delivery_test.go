package integration

// Phase 5 Batch 2 + Cloud Revision acceptance: Delivery → Revision → Release → Done, the second half
// of the Agent IssueRun lifecycle (IssueRun D3/D4/D5, Cloud Revision D1–D4, controller-integration
// D2/D6, operation D4; plan §5 Batch 2).
//
// The chain under test is the production one end to end:
//
//	a session ends            → the run `delivering` + one released deliver_revision work item (Batch 1)
//	agent_work_claim/dispatch → a registered deliver_revision execution carrying D2's fixed spec
//	GrantRevisionUpload       → one presigned PUT per object key of that attempt's frozen input
//	TakeOverNodeEvent         → the `agent_delivery_takeover` action: D4 steps 1–2 (the local input
//	                            comparison and the object-store HEAD for every declared object) run
//	                            BEFORE the transaction opens; the receipt, the durable result, the
//	                            `revisions` row and the settlement then commit in ONE transaction
//	DeliverySettled           → a verified Revision releases the run `releasing` with
//	                            deliveryState=saved|unchanged and the Revision's id; a failure D5 has
//	                            not given up on releases a backoff retry and keeps the run `delivering`
//	the releasing transition  → exactly one delete_workspace operation, declared in the same commit
//	the delete reaching       → RunWorkspaceDeleted moves the run `releasing → done`
//	`succeeded`
//
// The object store is the fixture's own double, installed as store.RevisionObjects (revisionObjects
// below). What these tests assert about it is the interface the delivery path consumes — which object
// keys are probed, with which declared size and digest, and what a failed probe does to the run — not
// that a real S3 accepts Cloud's signatures. The request shapes are internal/objectstore's own
// business, and the ADR's local-MinIO integration test is still owed: no MinIO endpoint was
// reachable when this slice was written, and that gap is registered in the plan rather than papered
// over here.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// controllerClaims is the lease holder the seeded Thread scenes install (`ctrl-a`), which every
// lease-validated control action in these tests speaks as.
func controllerClaims() *core.Claims {
	return &core.Claims{Kind: "service", Role: "controller", RegisteredClaims: jwt.RegisteredClaims{Subject: "ctrl-a"}}
}

// revisionFailedResult renders one delivery failure addressed to the scene's own Node, so the A
// layer's node-identity check is satisfied the way a real Node's result would be.
func revisionFailedResult(scene liveThreadScene, reason controlpb.RevisionFailureReason) *controlpb.ExecutionResult {
	return &controlpb.ExecutionResult{
		Node:    &controlpb.NodeIdentity{NodeId: scene.nodeID, NodeIncarnationId: "inc-" + scene.nodeID[:8]},
		Outcome: &controlpb.ExecutionResult_RevisionFailed{RevisionFailed: &controlpb.RevisionFailed{Reason: reason}},
	}
}

// revisionObjects is the fixture's stand-in for Cloud's object-store client (Cloud Revision D1). It
// answers every probe the way a store holding exactly the declared objects would, except for the one
// key a case names as missing or different, which is how the failure path is reached without a real
// S3. Every probe is recorded, because *which* objects Cloud spends a request on is an obligation in
// its own right: a result Cloud cannot compare against its own frozen input must never reach the
// store at all (D4 step 1 precedes step 2).
type revisionObjects struct {
	mu       sync.Mutex
	probes   []core.Object
	issued   []string
	ttl      time.Duration
	rejectAt string
}

func (r *revisionObjects) PresignPut(objectKey string, ttl time.Duration) (core.UploadGrant, error) {
	r.mu.Lock()
	r.issued = append(r.issued, objectKey)
	r.ttl = ttl
	r.mu.Unlock()
	// A grant that is shaped like the real one but is not a signature: the fixture's Controller never
	// uploads anything, so the URL is only ever asserted on, never fetched.
	return core.UploadGrant{
		ObjectKey: objectKey,
		URL:       "https://store.invalid/" + objectKey + "?X-Amz-Signature=fixture",
		Method:    http.MethodPut,
		Headers:   map[string]string{},
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, nil
}

func (r *revisionObjects) Verify(_ context.Context, objectKey string, size int64, sha256 string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes = append(r.probes, core.Object{"key": objectKey, "size": size, "sha256": sha256})
	if objectKey == r.rejectAt {
		return fmt.Errorf("object %s: the stored object does not match the declaration", objectKey)
	}
	return nil
}

// probed returns the objects Cloud verified, in order.
func (r *revisionObjects) probed() []core.Object {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.Object(nil), r.probes...)
}

// issuedKeys returns the object keys Cloud signed a grant for, in order.
func (r *revisionObjects) issuedKeys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.issued...)
}

// issuedTTL returns the lifetime of the last grant Cloud signed.
func (r *revisionObjects) issuedTTL() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ttl
}

// useObjectStore installs the double on the store and returns it. A Store with a nil
// RevisionObjects is the other legal state D1 defines — a deployment without object storage — and the
// tests that need it clear the field explicitly.
func (f *fixture) useObjectStore() *revisionObjects {
	f.t.Helper()
	objects := &revisionObjects{}
	f.store.RevisionObjects = objects
	return objects
}

// deliveryInputOf reads the frozen DeliverRevisionSpec one registered attempt carries. Every
// well-formed result below is measured against it rather than against invented keys, because D4 step
// 1 compares the two: a helper with hard-coded object keys could only ever exercise the refusal path.
func (f *fixture) deliveryInputOf(execution string) core.Object {
	f.t.Helper()
	var raw string
	must(f.t, f.store.Pool.QueryRow(`SELECT input::text FROM node_executions WHERE execution_id=$1`, execution).Scan(&raw))
	out := core.Object{}
	must(f.t, json.Unmarshal([]byte(raw), &out))
	return out
}

// deliveredResult renders the success outcome a Node reports after uploading a bundle, measured
// against the attempt's own input: the Revision ref and the baseline it was dispatched with, the two
// keys Cloud assigned it, and a size and digest for each.
func deliveredResult(scene liveThreadScene, in core.Object, final string) *controlpb.ExecutionResult {
	return &controlpb.ExecutionResult{
		Node: &controlpb.NodeIdentity{NodeId: scene.nodeID, NodeIncarnationId: "inc-" + scene.nodeID[:8]},
		Outcome: &controlpb.ExecutionResult_RevisionDelivered{RevisionDelivered: &controlpb.RevisionDelivered{
			FinalCommit: final, BaseCommit: in.S("base_commit"), RevisionRef: in.S("revision_ref"),
			Bundle:  &controlpb.StoredObject{Key: in.S("bundle_key"), Size: revisionBundleSize, Sha256: revisionBundleSHA},
			History: &controlpb.StoredObject{Key: in.S("history_key"), Size: revisionHistorySize, Sha256: revisionHistorySHA},
		}},
	}
}

// unchangedResult renders the outcome for a session that changed nothing: the final commit equals the
// baseline and only the history was uploaded, so the result declares no bundle at all.
func unchangedResult(scene liveThreadScene, in core.Object) *controlpb.ExecutionResult {
	return &controlpb.ExecutionResult{
		Node: &controlpb.NodeIdentity{NodeId: scene.nodeID, NodeIncarnationId: "inc-" + scene.nodeID[:8]},
		Outcome: &controlpb.ExecutionResult_RevisionUnchanged{RevisionUnchanged: &controlpb.RevisionUnchanged{
			FinalCommit: in.S("base_commit"), BaseCommit: in.S("base_commit"), RevisionRef: in.S("revision_ref"),
			History: &controlpb.StoredObject{Key: in.S("history_key"), Size: revisionHistorySize, Sha256: revisionHistorySHA},
		}},
	}
}

// The measurements every delivered/unchanged result declares, and the two commits the cases name. A
// digest is 64 hex digits by contract; the values themselves are arbitrary, because nothing in Cloud
// compares a digest against content — the object store does, and the fixture's double answers for it.
const (
	revisionBundleSize  = 4096
	revisionHistorySize = 512
	revisionFinalCommit = "1111111111111111111111111111111111111111"
)

var (
	revisionBundleSHA  = strings.Repeat("a", 64)
	revisionHistorySHA = strings.Repeat("b", 64)
)

// revisionRow is one `revisions` row in its typed column form, or nil when Cloud registered none.
// The columns are read individually rather than as JSON because the assertions compare them against
// the run's own state, and a JSON round trip would turn every bigint into a float64.
type revisionRow struct {
	ID            string
	TenantID      string
	RunID         string
	WorkspaceID   string
	ProjectID     string
	RepositoryURL string
	BaseCommit    string
	FinalCommit   string
	RevisionRef   string
	// The bundle exists exactly when the checkout changed: empty and nil together.
	BundleKey     string
	BundleSize    *int64
	BundleSHA256  string
	HistoryKey    string
	HistorySize   int64
	HistorySHA256 string
	// Always NULL in the first version: expiry and cleanup are an explicit non-goal (D4).
	ExpiresAt *time.Time
}

func (f *fixture) revisionRow(runID string) *revisionRow {
	f.t.Helper()
	if f.scalar(`SELECT count(*) FROM revisions WHERE run_id=$1`, runID) == 0 {
		return nil
	}
	var row revisionRow
	var bundleKey, bundleSHA sql.NullString
	var bundleSize sql.NullInt64
	var expires sql.NullTime
	must(f.t, f.store.Pool.QueryRow(`
		SELECT id::text, tenant_id::text, run_id::text, workspace_id::text, project_id::text,
		       repository_url, base_commit, final_commit, revision_ref,
		       bundle_key, bundle_size, bundle_sha256,
		       history_key, history_size, history_sha256, expires_at
		FROM revisions WHERE run_id=$1`, runID).
		Scan(&row.ID, &row.TenantID, &row.RunID, &row.WorkspaceID, &row.ProjectID,
			&row.RepositoryURL, &row.BaseCommit, &row.FinalCommit, &row.RevisionRef,
			&bundleKey, &bundleSize, &bundleSHA, &row.HistoryKey, &row.HistorySize, &row.HistorySHA256, &expires))
	row.BundleKey, row.BundleSHA256 = bundleKey.String, bundleSHA.String
	if bundleSize.Valid {
		size := bundleSize.Int64
		row.BundleSize = &size
	}
	if expires.Valid {
		at := expires.Time
		row.ExpiresAt = &at
	}
	return &row
}

// receiptResult reads the Node's own payload as the takeover's receipt kept it. D4 step 4 rewrites
// the durable `node_executions.result` when Cloud's verification fails, so the receipt is the only
// row that still holds what the Node claimed — and it is exactly the copy the replay comparison is
// made against, which is why an exact replay after a failed verification must stay a no-op.
func (f *fixture) receiptResult(execution string, sequence int64) core.Object {
	f.t.Helper()
	var raw string
	must(f.t, f.store.Pool.QueryRow(`
		SELECT COALESCE(event -> 'result', '{}')::text FROM node_event_receipts
		WHERE execution_id=$1 AND sequence=$2`, execution, sequence).Scan(&raw))
	out := core.Object{}
	must(f.t, json.Unmarshal([]byte(raw), &out))
	return out
}

// nodeResult reads one execution's durable terminal result, or nil when it holds none. It is the row
// D4 step 4 may rewrite, so a case has to be able to tell the Node's own payload from Cloud's verdict.
func (f *fixture) nodeResult(execution string) core.Object {
	f.t.Helper()
	var raw sql.NullString
	must(f.t, f.store.Pool.QueryRow(`SELECT result::text FROM node_executions WHERE execution_id=$1`, execution).Scan(&raw))
	if !raw.Valid {
		return nil
	}
	out := core.Object{}
	must(f.t, json.Unmarshal([]byte(raw.String), &out))
	return out
}

// deliveryRows reads the attempt's two durable rows whole, as text: the execution and the work item's
// frozen input. Comparing them as text is what lets "this request wrote nothing" be asserted over
// every column, including ones a later change adds, rather than over a chosen list of fields.
func (f *fixture) deliveryRows(execution string) (executionRow, workInput string) {
	f.t.Helper()
	must(f.t, f.store.Pool.QueryRow(`SELECT to_jsonb(e)::text FROM node_executions e WHERE execution_id=$1`, execution).Scan(&executionRow))
	must(f.t, f.store.Pool.QueryRow(`SELECT to_jsonb(w)::text FROM execution_work w WHERE execution_id=$1`, execution).Scan(&workInput))
	return executionRow, workInput
}

// runPlacement reads the project and repository URL Cloud copies onto a Revision row from the run's
// own Workspace and project — never from the delivered payload, which names neither (D4).
func (f *fixture) runPlacement(runID string) (projectID, repositoryURL string) {
	f.t.Helper()
	must(f.t, f.store.Pool.QueryRow(`
		SELECT p.id, p.repository_url FROM issue_runs r
		JOIN workspaces w ON w.id = r.workspace_id
		JOIN projects p ON p.id = w.project_id
		WHERE r.id=$1`, runID).Scan(&projectID, &repositoryURL))
	return projectID, repositoryURL
}

// deliveryTerminal submits one delivery terminal event over the real gRPC surface as the seeded
// lease holder. The canonical event bytes are the caller's label: the A layer stores them verbatim
// and never parses them, so their only job is to make "same sequence, different bytes" detectable.
func (f *fixture) deliveryTerminal(scene liveThreadScene, execution, submission string, sequence uint64, res *controlpb.ExecutionResult, event string) (*controlpb.TakeOverNodeEventResponse, error) {
	return f.executions.TakeOverNodeEvent(asController("ctrl-a"), &controlpb.TakeOverNodeEventRequest{
		SubmissionId: submission, Epoch: 1, OperationId: scene.runID, ExecutionId: execution,
		Sequence: sequence, Result: res, Event: []byte(event),
	})
}

// deliveringScene drives the scene to exactly the state Batch 2 starts from: a live session that
// ended through the production takeover, the run `delivering`, and its delivery work item released.
func deliveringScene(t *testing.T, f *fixture) liveThreadScene {
	t.Helper()
	f.useRealControlPlane()
	scene := seedLiveThreadScene(t, f)
	scene.start(t, f)
	f.runningThread(t, scene)
	f.sessionEndOK(t, scene, "", 2, controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_USER_ENDED)
	if got := len(f.deliveryWork(scene.runID)); got != 1 {
		t.Fatalf("the terminal takeover must release one delivery work item, got %d", got)
	}
	return scene
}

// claimDelivery picks the run's released delivery work item the way a Controller does, through the
// real agent_work_claim action, and requires it to be a delivery.
func (f *fixture) claimDelivery(t *testing.T, scene liveThreadScene) core.Object {
	t.Helper()
	out, e := f.store.Control(context.Background(), &core.ControlRequest{
		Action: "agent_work_claim", Body: core.Object{"epoch": 1}, Service: controllerClaims(),
	})
	must(t, e)
	work := out.O("work")
	if len(work) == 0 {
		t.Fatal("the run must have an unregistered delivery work item to claim")
	}
	if work.S("kind") != "deliver_revision" {
		t.Fatalf("claimed work kind = %q, want deliver_revision", work.S("kind"))
	}
	if work.S("runId") != scene.runID {
		t.Fatalf("claimed work belongs to run %s, want %s", work.S("runId"), scene.runID)
	}
	return work
}

// registerDelivery registers the claimed delivery work item through the real agent_work_dispatch
// action, which is what makes a delivery execution addressable by a terminal result.
func (f *fixture) registerDelivery(t *testing.T, scene liveThreadScene, work core.Object, execution string) {
	t.Helper()
	_, e := f.store.Control(context.Background(), &core.ControlRequest{
		Action: "agent_work_dispatch",
		Body: core.Object{
			"workId": work.S("id"), "executionId": execution, "nodeId": scene.nodeID,
			"input": work.O("input"), "epoch": 1,
		},
		Service: controllerClaims(),
	})
	must(t, e)
}

// deliverRevision claims and registers the run's delivery work item, returning the execution id a
// terminal result is addressed to.
func (f *fixture) deliverRevision(t *testing.T, scene liveThreadScene) string {
	t.Helper()
	work := f.claimDelivery(t, scene)
	execution := "exec-delivery-" + work.S("id")[:8]
	f.registerDelivery(t, scene, work, execution)
	return execution
}

// deliveryAttempts reads the run's delivery work items with the timestamps D5's backoff is expressed
// in, oldest first. `backoffSeconds` is the declared delay between the item's declaration and its
// eligibility, which is what makes a retry wait instead of spinning.
func (f *fixture) deliveryAttempts(runID string) []core.Object {
	f.t.Helper()
	rows, e := f.store.Pool.Query(`
		SELECT id, input::text AS input, (execution_id IS NOT NULL) AS registered,
		       extract(epoch FROM (available_at - created_at)) AS backoff_seconds,
		       (available_at <= clock_timestamp()) AS available
		FROM execution_work WHERE run_id=$1 AND kind='deliver_revision' ORDER BY created_at, id`, runID)
	must(f.t, e)
	defer rows.Close()
	var out []core.Object
	for rows.Next() {
		var id, input string
		var registered, available bool
		var backoff float64
		must(f.t, rows.Scan(&id, &input, &registered, &backoff, &available))
		o := core.Object{"id": id, "registered": registered, "backoffSeconds": backoff, "available": available}
		must(f.t, json.Unmarshal([]byte(input), &o))
		out = append(out, o)
	}
	must(f.t, rows.Err())
	return out
}

// runPhase reads the run's phase alone, which is all the lifecycle trace needs.
func (f *fixture) runPhase(runID string) string {
	f.t.Helper()
	var phase string
	must(f.t, f.store.Pool.QueryRow(`SELECT phase FROM issue_runs WHERE id=$1`, runID).Scan(&phase))
	return phase
}

// runStatusVersion reads the run's status and version: the two columns a terminal transition must
// leave alone or bump exactly once.
func (f *fixture) runStatusVersion(runID string) (string, int64) {
	f.t.Helper()
	var status string
	var version int64
	must(f.t, f.store.Pool.QueryRow(`SELECT status, version FROM issue_runs WHERE id=$1`, runID).Scan(&status, &version))
	return status, version
}

// runResult reads the run's durable result object (D4's `{revisionId|null, deliveryState}`).
func (f *fixture) runResult(runID string) core.Object {
	f.t.Helper()
	var raw string
	must(f.t, f.store.Pool.QueryRow(`SELECT COALESCE(result,'{}')::text FROM issue_runs WHERE id=$1`, runID).Scan(&raw))
	out := core.Object{}
	must(f.t, json.Unmarshal([]byte(raw), &out))
	return out
}

// runWorkspaceID reads the run's bound Workspace.
func (f *fixture) runWorkspaceID(runID string) string {
	f.t.Helper()
	var wid string
	must(f.t, f.store.Pool.QueryRow(`SELECT COALESCE(workspace_id::text,'') FROM issue_runs WHERE id=$1`, runID).Scan(&wid))
	return wid
}

// workspaceFields reads the Workspace columns the release path writes.
func (f *fixture) workspaceFields(wid string) core.Object {
	f.t.Helper()
	rows, e := f.store.Pool.Query(`
		SELECT desired_state, observed_state, admission_open, admission_epoch, owner_user_id, project_id, deleted_at IS NOT NULL
		FROM workspaces WHERE id=$1`, wid)
	must(f.t, e)
	defer rows.Close()
	if !rows.Next() {
		f.t.Fatalf("workspace %s not found", wid)
	}
	var desired, observed, owner, project string
	var open, deleted bool
	var epoch int64
	must(f.t, rows.Scan(&desired, &observed, &open, &epoch, &owner, &project, &deleted))
	return core.Object{
		"desiredState": desired, "observedState": observed, "admissionOpen": open,
		"admissionEpoch": epoch, "ownerUserId": owner, "projectId": project, "deleted": deleted,
	}
}

// runOperations reads the run Workspace's operations oldest first, which is the durable set a
// declaration or a re-declaration must not duplicate.
func (f *fixture) runOperations(runID string) []core.Object {
	f.t.Helper()
	var wid string
	must(f.t, f.store.Pool.QueryRow(`SELECT COALESCE(workspace_id::text,'') FROM issue_runs WHERE id=$1`, runID).Scan(&wid))
	rows, e := f.store.Pool.Query(`
		SELECT id, kind, state, step, actor_user_id, idempotency_key, COALESCE(error_code,'') AS error_code, request::text AS request
		FROM operations WHERE workspace_id=$1 ORDER BY created_at, id`, wid)
	must(f.t, e)
	defer rows.Close()
	var out []core.Object
	for rows.Next() {
		var id, kind, state, step, actor, key, code, request string
		must(f.t, rows.Scan(&id, &kind, &state, &step, &actor, &key, &code, &request))
		out = append(out, core.Object{
			"id": id, "kind": kind, "state": state, "step": step,
			"actorUserId": actor, "idempotencyKey": key, "errorCode": code, "request": request,
		})
	}
	must(f.t, rows.Err())
	return out
}

// deleteOperations narrows runOperations to the delete intents.
func deleteOperations(ops []core.Object) []core.Object {
	var out []core.Object
	for _, o := range ops {
		if o.S("kind") == "delete_workspace" {
			out = append(out, o)
		}
	}
	return out
}

// activityDetails reads the newest issue activity of one action, which is where the release reports
// the two independent outcomes (how the session ended, whether the Revision was saved).
func (f *fixture) activityDetails(issueID, action string) core.Object {
	f.t.Helper()
	var raw string
	must(f.t, f.store.Pool.QueryRow(`
		SELECT COALESCE(details,'{}')::text FROM issue_activities
		WHERE issue_id=$1 AND action=$2 ORDER BY seq DESC LIMIT 1`, issueID, action).Scan(&raw))
	out := core.Object{}
	must(f.t, json.Unmarshal([]byte(raw), &out))
	return out
}

// bindRunWorkspaceSandbox gives the seeded run Workspace's sandbox and Node the external identity a
// real deployment reaches through the create_workspace operation, and returns both row ids.
//
// The seeded scene's Workspace is synthetic: its sandbox and Node exist in Cloud's tables, but the
// Substrate — the durable external journal every effect is dispatched against — never saw them,
// because in production the `sandbox` step's effect creates them. Both fix-ups mirror what that step
// leaves behind:
//
//   - The Substrate journal entry. The `sandbox_ensure` effect's id and the sandbox row's id are the
//     same value (the operation machine draws one id for both), and `sandbox_terminate` looks the
//     sandbox up by the row id, so recording it under that id is what makes a real terminate/cleanup
//     executable rather than a stub. The Substrate's own effect API is used, so the journal's
//     conflict and replay rules apply to it like any other effect.
//   - substrate_sandbox_id and the Node's credential subject. nodeScope requires the former to be
//     set, and a Node credential is only accepted when its subject is the Node row's own
//     service_subject — the identity the `node` step's registration establishes.
func (f *fixture) bindRunWorkspaceSandbox(t *testing.T, runID string) (sandboxID, nodeID string) {
	t.Helper()
	wid := f.runWorkspaceID(runID)
	must(t, f.store.Pool.QueryRow(`
		SELECT s.id, n.id FROM sandbox_instances s JOIN node_instances n ON n.sandbox_instance_id=s.id
		WHERE s.workspace_id=$1 AND s.terminated_at IS NULL AND n.ended_at IS NULL
		ORDER BY s.generation DESC LIMIT 1`, wid).Scan(&sandboxID, &nodeID))
	f.recordSandbox(t, sandboxID, wid)
	_, e := f.store.Pool.Exec(`UPDATE sandbox_instances SET substrate_sandbox_id=id::text WHERE id=$1`, sandboxID)
	must(t, e)
	_, e = f.store.Pool.Exec(`UPDATE node_instances SET service_subject=id::text WHERE id=$1`, nodeID)
	must(t, e)
	return sandboxID, nodeID
}

// recordSandbox stages one Workspace's sandbox in the Substrate's own journal through the same effect
// API a Controller's `sandbox` step uses. The request is shaped exactly like planEffect's
// (`kind`/`projectId`/`workspaceId`) because the Substrate validates those fields before it performs
// anything: the journal entry is what the later `sandbox_terminate` reads back by sandbox id, and an
// entry the Substrate would not have accepted is not a faithful stand-in for one it did. Re-staging
// the same journal entry is a no-op there, so the helper is safe to call more than once.
func (f *fixture) recordSandbox(t *testing.T, sandboxID, wid string) {
	t.Helper()
	var projectID string
	must(t, f.store.Pool.QueryRow(`SELECT project_id FROM workspaces WHERE id=$1`, wid).Scan(&projectID))
	f.substrateRun(t, core.Object{
		"id": sandboxID,
		"request": core.Object{
			"kind": "sandbox_ensure", "projectId": projectID, "workspaceId": wid,
		},
	}, http.StatusOK)
}

// driveDelete hands the run Workspace's operations to the fixture's simulator Controller — the
// production executor of every operation — and drives them to their terminal state.
//
// One handover precedes it: the Thread scenes' seed installs Controller `ctrl-a` as the lease holder
// (every takeover above speaks as it), while the operation path speaks as the fixture's simulator
// Controller, so the lease is re-pointed to it with the epoch that Controller holds. That is exactly
// what a real deployment performs when a Controller takes over, and `leaseValid` matches on the
// holder, so nothing else about the lease changes.
func (f *fixture) driveDelete(t *testing.T, runID string) {
	t.Helper()
	f.bindRunWorkspaceSandbox(t, runID)
	_, e := f.store.Pool.Exec(
		`UPDATE controller_leases SET holder_id=$2, epoch=$1, expires_at=clock_timestamp()+interval '1 hour' WHERE name='global'`,
		f.controller.Epoch, f.client.Subject,
	)
	must(t, e)
	f.drain()
}

// lifecycleOrder is IssueRun D3's phase machine, oldest first. A run may only move to the next entry:
// a jump skips a stage and a move backwards re-opens a closed one, and both are the failures the
// forward-only assertions in this file exist to catch.
var lifecycleOrder = []string{"provisioning", "starting", "running", "delivering", "releasing", "done"}

// assertForwardOnly requires every observed step of a run's phase trace to be the immediate next
// stage of D3's machine, which is the whole "no backward lifecycle, no skipped stage" obligation —
// asserted over the run's own observed history rather than over a list of expected values.
func assertForwardOnly(t *testing.T, trace []string) {
	t.Helper()
	for i := 1; i < len(trace); i++ {
		from, to := trace[i-1], trace[i]
		fromAt, toAt := indexOf(lifecycleOrder, from), indexOf(lifecycleOrder, to)
		if fromAt < 0 || toAt < 0 {
			t.Fatalf("observed phase outside D3's machine: %q → %q (trace %v)", from, to, trace)
		}
		if toAt != fromAt+1 {
			t.Fatalf("the lifecycle must advance exactly one stage: %q → %q (trace %v)", from, to, trace)
		}
	}
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// P5-7, §3/§4 — the released delivery work item is claimable and registrable, its input is the fixed
// DeliverRevision snapshot D2 defines (read from durable Cloud state, never from a Thread identity),
// and registering it moves nothing: registration is not starting the delivery.
func TestDeliveryExecutionClaimCarriesTheFixedSpecAndMovesNothing(t *testing.T) {
	f := setup(t)
	scene := deliveringScene(t, f)
	before := f.runPhase(scene.runID)

	work := f.claimDelivery(t, scene)
	in := work.O("input")
	if got := in.S("session_execution_id"); got != scene.executionID {
		t.Fatalf("session_execution_id = %q, want the ended session execution %q", got, scene.executionID)
	}
	if got := in.S("base_commit"); got != seedRunWorkspaceCommit {
		t.Fatalf("base_commit = %q, want the run Workspace's recorded baseline %q", got, seedRunWorkspaceCommit)
	}
	if got := in.S("revision_ref"); got != "refs/ora/revisions/"+scene.runID {
		t.Fatalf("revision_ref = %q, want the name Cloud owns for this run", got)
	}
	// The per-attempt object keys (D2): both live under the run's own prefix, so no two runs and no
	// two attempts can overwrite each other's objects.
	prefix := "revisions/" + scene.tenantID + "/" + scene.runID + "/"
	if got := in.S("bundle_key"); !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, "/revision.bundle") {
		t.Fatalf("bundle_key = %q, want a per-attempt key under %s", got, prefix)
	}
	if got := in.S("history_key"); !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, "/session.jsonl") {
		t.Fatalf("history_key = %q, want a per-attempt key under %s", got, prefix)
	}
	// The third segment is the delivery attempt Cloud drew before this work item existed (D2). Both
	// keys name the same one, and it is Cloud's own attempt id rather than the run or the execution:
	// the Controller picks the execution id later, so a key read here cannot contain it.
	attempt := strings.TrimSuffix(strings.TrimPrefix(in.S("bundle_key"), prefix), "/revision.bundle")
	if attempt == "" || attempt == scene.runID {
		t.Fatalf("the attempt segment %q must be Cloud's own attempt id, not the run", attempt)
	}
	if got := strings.TrimSuffix(strings.TrimPrefix(in.S("history_key"), prefix), "/session.jsonl"); got != attempt {
		t.Fatalf("both object keys must name the same attempt: %q vs %q", got, attempt)
	}
	if got := work.O("target").S("node_id"); got != scene.nodeID {
		t.Fatalf("the delivery target node = %q, want the run Workspace's Node %q", got, scene.nodeID)
	}

	execution := "exec-delivery-" + work.S("id")[:8]
	f.registerDelivery(t, scene, work, execution)
	// The keys were drawn before any execution existed and are never rewritten to name it: a key that
	// carried the execution id could not have been written into the frozen input at all.
	for _, key := range []string{in.S("bundle_key"), in.S("history_key")} {
		if strings.Contains(key, execution) {
			t.Fatalf("the per-attempt key %q names the execution id, which did not exist when Cloud drew it", key)
		}
	}
	if got := f.runPhase(scene.runID); got != before {
		t.Fatalf("registering a delivery execution must not move the run off delivering, got %q", got)
	}
	if got := f.scalar(`SELECT count(*) FROM node_executions WHERE execution_id=$1 AND kind='deliver_revision'`, execution); got != 1 {
		t.Fatalf("the dispatch must register exactly one deliver_revision execution, got %d", got)
	}
	if got := f.deliveryWork(scene.runID); len(got) != 1 || !got[0].B("registered") {
		t.Fatalf("the delivery work item must be registered exactly once, got %v", got)
	}
	// With the only work item registered, the claim slot is empty: a Controller can never be handed
	// an execution it has already been given. The raw map is inspected rather than `O()`, which
	// answers with an empty object for an absent key and so cannot express "nothing was claimed".
	out, e := f.store.Control(context.Background(), &core.ControlRequest{
		Action: "agent_work_claim", Body: core.Object{"epoch": 1}, Service: controllerClaims(),
	})
	must(t, e)
	if got := out["work"]; got != nil {
		t.Fatalf("a registered delivery must not be claimed twice, got %v", got)
	}
}

// P5-8, §10/§11 — a failed delivery keeps the run `delivering` (the sandbox holds the work the
// Revision needs) and releases exactly one new attempt with D5's backoff. The retry is the same
// logical delivery: same run, same session, same baseline and same Revision ref — only the per-attempt
// object keys differ, so the failed attempt's objects can never be overwritten.
func TestFailedDeliveryKeepsDeliveringAndReleasesOneBackoffRetry(t *testing.T) {
	f := setup(t)
	scene := deliveringScene(t, f)
	execution := f.deliverRevision(t, scene)
	first := f.deliveryAttempts(scene.runID)
	rows := f.threadRows(scene.runID)

	// With neither give-up limit configured, the pass must be a no-op: an unconfigured window is "no
	// give-up", never "give up immediately".
	must(t, f.store.GiveUpStaleDeliveriesOnce(context.Background()))
	if got := f.runPhase(scene.runID); got != "delivering" {
		t.Fatalf("an unconfigured give-up window must release nothing, got phase %q", got)
	}

	if _, e := f.deliveryTerminal(scene, execution, "p5-delivery-fail", 1,
		revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
		"revision_failed/upload_failed"); e != nil {
		t.Fatalf("the failed delivery result must be taken over: %v", e)
	}

	phase, status, state := f.threadRunState(scene.runID)
	if phase != "delivering" || status != "running" || state != "ended" {
		t.Fatalf("a failed delivery must leave the run delivering/running with its Thread ended, got %s/%s/%s", phase, status, state)
	}
	// The terminal fact is durable even though the delivery did not settle: the receipt is the ack
	// basis and the result is what the retry's spec and the eventual status are read from.
	if got := f.nodeSequence(execution); got != 1 {
		t.Fatalf("the failed result must advance the execution's sequence once, got %d", got)
	}
	if got := f.scalar(`SELECT count(*) FROM node_event_receipts WHERE execution_id=$1 AND sequence=1`, execution); got != 1 {
		t.Fatalf("the failed result must be receipted once, got %d", got)
	}
	if got := fmt.Sprint(f.threadRows(scene.runID)); got != fmt.Sprint(rows) {
		t.Fatalf("a failed delivery must not touch the Thread log, got %s", got)
	}
	if len(first) != 1 {
		t.Fatalf("the scene must hold exactly one delivery attempt before the failure, got %d", len(first))
	}

	attempts := f.deliveryAttempts(scene.runID)
	if len(attempts) != 2 {
		t.Fatalf("a failed delivery must release exactly one retry, got %d attempts", len(attempts))
	}
	if !attempts[0].B("registered") || attempts[1].B("registered") {
		t.Fatalf("only the first attempt may be registered, got %v", attempts)
	}
	if got := attempts[1].N("backoffSeconds"); got < 29 || got > 31 {
		t.Fatalf("D5's first retry must wait 30s, got %ds", got)
	}
	if attempts[1].B("available") {
		t.Fatalf("the released retry must not be claimable before its backoff elapses, got %v", attempts[1])
	}
	// One logical delivery: everything but the per-attempt keys is identical.
	for _, key := range []string{"session_execution_id", "base_commit", "revision_ref"} {
		if attempts[0].S(key) != attempts[1].S(key) {
			t.Fatalf("a retry must not become a new logical delivery: %s changed (%q → %q)", key, attempts[0].S(key), attempts[1].S(key))
		}
	}
	if attempts[0].S("bundle_key") == attempts[1].S("bundle_key") {
		t.Fatalf("a retry must not reuse the failed attempt's object key, got %q", attempts[0].S("bundle_key"))
	}
	// The run stayed delivering, so nothing released it: no delete intent exists yet.
	if got := deleteOperations(f.runOperations(scene.runID)); len(got) != 0 {
		t.Fatalf("a run still delivering must have no delete intent, got %v", got)
	}
}

// P5-9, §9/§15 — D5's two give-up limits release the run: `delivering → releasing` with
// `deliveryState=failed`, D4's status derived from the session's own end reason, and a Timeline
// activity that reports the two outcomes independently. `done` is never reached here, and the status
// is never read off the delivery outcome.
func TestDeliveryGiveUpReleasesTheRunWithD5StateAndD4Status(t *testing.T) {
	cases := []struct {
		name   string
		reason controlpb.AgentSessionEndReason
		status string
		limit  func(f *fixture)
		// age reports whether the case reaches D5's limit through the run Workspace's Node heartbeat
		// rather than through elapsed time: the unreachability window is measured from the Node's last
		// report, so the case has to move that instant.
		age bool
	}{
		{
			// D5's continuous-failure window: the run has been failing to deliver for longer than the
			// configured window, so the next terminal failure releases it.
			name: "continuous failure window", reason: controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_USER_ENDED, status: "completed",
			limit: func(f *fixture) { f.store.DeliveryGiveUpAfter = time.Nanosecond },
		},
		{
			// D5's unreachability window: the Workspace's Node has been unknown long enough that
			// waiting out the full failure window would only hold the sandbox longer.
			name: "Workspace unreachable", reason: controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_USER_ENDED, status: "completed",
			limit: func(f *fixture) { f.store.DeliveryUnreachableAfter = time.Nanosecond }, age: true,
		},
		{
			// D4's status is derived from the session end reason, not from the delivery: an Agent that
			// failed ends `failed` even though the delivery path is what releases the run.
			name: "failed session", reason: controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_AGENT_FAILED, status: "failed",
			limit: func(f *fixture) { f.store.DeliveryGiveUpAfter = time.Nanosecond },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			f.useRealControlPlane()
			scene := seedLiveThreadScene(t, f)
			scene.start(t, f)
			f.runningThread(t, scene)
			f.sessionEndOK(t, scene, "", 2, c.reason)
			execution := f.deliverRevision(t, scene)
			c.limit(f)
			if c.age {
				// The Node's heartbeat is the run Workspace's only evidence of liveness; one older
				// than the window is exactly the "state unknown" D5 measures.
				must(t, f.ageRunWorkspaceNode(scene.runID))
			}

			if _, e := f.deliveryTerminal(scene, execution, "p5-give-up", 1,
				revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
				"revision_failed/upload_failed"); e != nil {
				t.Fatalf("the give-up result must be taken over: %v", e)
			}

			phase, status, state := f.threadRunState(scene.runID)
			if phase != "releasing" || state != "ended" {
				t.Fatalf("the give-up must release the run, got phase=%q thread=%q", phase, state)
			}
			if status != c.status {
				t.Fatalf("status must come from the session end reason: got %q want %q", status, c.status)
			}
			result := f.runResult(scene.runID)
			if got := result.S("deliveryState"); got != "failed" {
				t.Fatalf("deliveryState = %q, want failed", got)
			}
			// D4 spells the result `{revisionId | null, deliveryState}`: an absent key would be
			// indistinguishable from a Revision whose id Cloud failed to record.
			if v, ok := result["revisionId"]; !ok || v != nil {
				t.Fatalf("revisionId must be an explicit null on the release path, got %v (present=%v)", v, ok)
			}
			details := f.activityDetails(scene.issueID, "run."+c.status)
			if got := details.S("deliveryState"); got != "failed" {
				t.Fatalf("the timeline activity must report the delivery outcome, got %v", details)
			}
			if got := details.S("sessionEndReason"); got != endReasonName(c.reason) {
				t.Fatalf("the timeline activity must report the session end reason, got %v", details)
			}
			// The released retry the earlier failure had declared is not a reason to hold the run: the
			// release is the end of the delivery, and no second attempt is declared.
			if got := len(f.deliveryAttempts(scene.runID)); got != 1 {
				t.Fatalf("the give-up must not release another attempt, got %d", got)
			}
			if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
				t.Fatalf("the release must declare exactly one delete intent, got %d", got)
			}
		})
	}
}

// endReasonName is the stored snake_case spelling the release path derives D4's status from.
func endReasonName(r controlpb.AgentSessionEndReason) string {
	return strings.ToLower(strings.TrimPrefix(r.String(), "AGENT_SESSION_END_REASON_"))
}

// ageRunWorkspaceNode pushes the run Workspace's Node heartbeat past any meaningful window, which is
// the durable form of "the Node's state is unknown".
func (f *fixture) ageRunWorkspaceNode(runID string) error {
	f.t.Helper()
	_, e := f.store.Pool.Exec(`
		UPDATE node_instances SET last_seen_at=now()-interval '1 hour'
		WHERE workspace_id=(SELECT workspace_id FROM issue_runs WHERE id=$1) AND ended_at IS NULL`, runID)
	return e
}

// P5-9 continued, §10 — the same give-up is reached by Cloud's own clock when no Node ever reports
// again: the background pass applies D5's limits to a run the delivery path already left delivering,
// so a lost or endlessly failing Node cannot hold a Workspace forever.
func TestGiveUpStaleDeliveriesPassReleasesOnItsOwnClock(t *testing.T) {
	f := setup(t)
	scene := deliveringScene(t, f)
	execution := f.deliverRevision(t, scene)
	if _, e := f.deliveryTerminal(scene, execution, "", 1,
		revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
		"revision_failed/upload_failed"); e != nil {
		t.Fatalf("the failed delivery result must be taken over: %v", e)
	}
	if got := f.runPhase(scene.runID); got != "delivering" {
		t.Fatalf("with no limits configured the run must keep delivering, got %q", got)
	}

	f.store.DeliveryGiveUpAfter = time.Nanosecond
	must(t, f.store.GiveUpStaleDeliveriesOnce(context.Background()))
	if got := f.runPhase(scene.runID); got != "releasing" {
		t.Fatalf("the give-up pass must release the stale delivery, got phase %q", got)
	}
	// The whole of D5's settlement, in the three places it is observable: the run's decision, the
	// absence of a Revision, and the release's own delete. Nothing about it is `skipped` — that spelling
	// is reserved for D6's sessionless cancellation, and a delivery that failed must never borrow it.
	result := f.runResult(scene.runID)
	if got := result.S("deliveryState"); got != "failed" {
		t.Fatalf("the pass must record deliveryState=failed, got %q", got)
	}
	if v, ok := result["revisionId"]; !ok || v != nil {
		t.Fatalf("an abandoned delivery must record an explicit null revisionId, got %v (present=%v)", v, ok)
	}
	if row := f.revisionRow(scene.runID); row != nil {
		t.Fatalf("an abandoned delivery must register no Revision, got %v", row)
	}
	if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
		t.Fatalf("the pass must declare exactly one delete intent, got %d", got)
	}
	// A second tick changes nothing: the run has left `delivering`, so it is out of the scan.
	version := f.runVersion(scene.runID)
	must(t, f.store.GiveUpStaleDeliveriesOnce(context.Background()))
	if got := f.runVersion(scene.runID); got != version {
		t.Fatalf("a repeated tick must not release the run twice: version %d → %d", version, got)
	}
	if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
		t.Fatalf("a repeated tick must not declare a second delete, got %d", got)
	}
}

// runVersion reads the run's optimistic-concurrency version, the cheapest witness of "this commit
// wrote nothing".
func (f *fixture) runVersion(runID string) int64 {
	f.t.Helper()
	var version int64
	must(f.t, f.store.Pool.QueryRow(`SELECT version FROM issue_runs WHERE id=$1`, runID).Scan(&version))
	return version
}

// givingUpScene is P5-8's end state promoted to a starting point: the run released (`releasing`),
// `deliveryState=failed`, with its delete intent declared.
func givingUpScene(t *testing.T, f *fixture) (liveThreadScene, string) {
	t.Helper()
	scene := deliveringScene(t, f)
	execution := f.deliverRevision(t, scene)
	f.store.DeliveryGiveUpAfter = time.Nanosecond
	if _, e := f.deliveryTerminal(scene, execution, "", 1,
		revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
		"revision_failed/upload_failed"); e != nil {
		t.Fatalf("the give-up result must be taken over: %v", e)
	}
	if got := f.runPhase(scene.runID); got != "releasing" {
		t.Fatalf("the scene must be releasing, got %q", got)
	}
	return scene, execution
}

// P5-10, §12 — the releasing transition declares the run Workspace's delete through the approved
// seam, in the same commit, with the identity and actor the operation contract fixes. The delete is
// declared, never executed: no transaction reaches a workspace provider.
func TestReleasingDeclaresExactlyOneDeleteOperation(t *testing.T) {
	f := setup(t)
	scene, execution := givingUpScene(t, f)

	ops := deleteOperations(f.runOperations(scene.runID))
	if len(ops) != 1 {
		t.Fatalf("the release must declare exactly one delete_workspace operation, got %d (%v)", len(ops), ops)
	}
	op := ops[0]
	if op.S("state") != "queued" || op.S("step") != "quiesce" {
		t.Fatalf("the declared delete must be a fresh queued quiesce, got state=%q step=%q", op.S("state"), op.S("step"))
	}
	wid := f.runWorkspaceID(scene.runID)
	ws := f.workspaceFields(wid)
	if op.S("actorUserId") != ws.S("ownerUserId") {
		t.Fatalf("the delete must act as the Workspace's owner %q, got %q", ws.S("ownerUserId"), op.S("actorUserId"))
	}
	// Operation D4's identity for a run-Workspace operation is (issue_run_id, kind), generated by
	// Cloud: the Workspace binds one run uniquely, so its id is the same set, and the key must never
	// come from a caller-supplied idempotency key.
	if got := op.S("idempotencyKey"); got != "agent-run:"+scene.runID+":delete_workspace" {
		t.Fatalf("idempotency key = %q, want Cloud's own run-scoped key", got)
	}
	if !strings.Contains(op.S("request"), wid) {
		t.Fatalf("the delete request must carry the Workspace snapshot a refusal restores from, got %s", op.S("request"))
	}
	// Admission is closed by the declaration, not by the operation's first step.
	if ws.B("admissionOpen") || ws.S("desiredState") != "deleted" {
		t.Fatalf("the declaration must close admission for deletion, got %v", ws)
	}

	// A delivery result that arrives after the release is receipted as a fact about a finished attempt
	// and decides nothing. Both shapes a Controller's recovery can produce are asserted, because they
	// take different paths through the takeover: an exact replay of the already-committed event is the
	// A layer's own no-op (identical receipt, identical result — it never reaches the hook), while a
	// genuinely later event has to be committed and is then made inert by the settlement's own
	// `phase != delivering` branch. Neither may declare a second delete or move the phase.
	version := f.runVersion(scene.runID)
	if _, e := f.deliveryTerminal(scene, execution, "", 1,
		revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
		"revision_failed/upload_failed"); e != nil {
		t.Fatalf("an exact replay of the committed delivery event must be a no-op: %v", e)
	}
	if _, e := f.deliveryTerminal(scene, execution, "", 2,
		revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_CHECKOUT_UNAVAILABLE),
		"revision_failed/checkout_unavailable"); e != nil {
		t.Fatalf("a late delivery result for a released run must be receipted, not refused: %v", e)
	}
	if got := f.runVersion(scene.runID); got != version {
		t.Fatalf("a post-release delivery result must not write the run: version %d → %d", version, got)
	}
	if got := f.runPhase(scene.runID); got != "releasing" {
		t.Fatalf("a post-release delivery result must not move the phase, got %q", got)
	}
	if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
		t.Fatalf("a post-release delivery result must not declare a second delete, got %d", got)
	}
}

// P5-11, §13/§14 — the delete operation's terminal state is what settles the run, in the same
// transaction: a *succeeded* delete with the run still releasing is the state the whole hook exists
// to prevent. `done` then carries the status D4 derived, never a status of its own.
func TestDeleteTerminalStateSettlesTheRunToDone(t *testing.T) {
	f := setup(t)
	scene, _ := givingUpScene(t, f)
	wid := f.runWorkspaceID(scene.runID)
	status, version := f.runStatusVersion(scene.runID)

	f.driveDelete(t, scene.runID)

	ops := deleteOperations(f.runOperations(scene.runID))
	if len(ops) != 1 || ops[0].S("state") != "succeeded" || ops[0].S("step") != "done" {
		t.Fatalf("the delete operation must reach succeeded/done, got %v", ops)
	}
	if got := f.runPhase(scene.runID); got != "done" {
		t.Fatalf("a succeeded delete must settle the run to done, got %q", got)
	}
	gotStatus, gotVersion := f.runStatusVersion(scene.runID)
	if gotStatus != status {
		t.Fatalf("`done` must not rewrite the status D4 derived: got %q want %q", gotStatus, status)
	}
	if gotVersion <= version {
		t.Fatalf("the terminal transition must advance the run's version, got %d (before %d)", gotVersion, version)
	}
	// The Workspace the run held is gone, and nothing may be started in it again.
	ws := f.workspaceFields(wid)
	if !ws.B("deleted") || ws.B("admissionOpen") {
		t.Fatalf("the run Workspace must be deleted with admission closed, got %v", ws)
	}
	// The two recovery passes are no-ops on a finished run: `done` is out of both scans.
	done := f.runVersion(scene.runID)
	must(t, f.store.GiveUpStaleDeliveriesOnce(context.Background()))
	must(t, f.store.RedeclareRunWorkspaceDeletesOnce(context.Background()))
	if got := f.runVersion(scene.runID); got != done {
		t.Fatalf("a finished run must be inert: version %d → %d", done, got)
	}
	if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
		t.Fatalf("a finished run must not gain a second delete, got %d", got)
	}
}

// P5-12, §3/§6/§16 — the whole mainline in one run: session end → delivery claim/registration →
// delivery settlement → release → delete → done, with every phase observed in order and none
// skipped. This is the batch's end-to-end acceptance: the phases the approved ADRs define are all
// reachable through production code, in one uninterrupted trace.
func TestFullLifecycleSessionEndToDoneSkipsNoPhase(t *testing.T) {
	f := setup(t)
	f.useRealControlPlane()
	f.useObjectStore()
	scene := seedLiveThreadScene(t, f)
	trace := []string{f.runPhase(scene.runID)}
	if trace[0] != "starting" {
		t.Fatalf("the seeded run must start in `starting`, got %q", trace[0])
	}

	scene.start(t, f)
	// The session declaration alone does not run the session: the first taken-over Node record is
	// what moves the run (D-017/D-019), and that is the stage boundary asserted here.
	if got := f.runPhase(scene.runID); got != "starting" {
		t.Fatalf("declaring a session must not run it, got phase %q", got)
	}
	f.runningThread(t, scene)
	trace = append(trace, f.runPhase(scene.runID))
	if got := f.runPhase(scene.runID); got != "running" {
		t.Fatalf("the first taken-over record must run the session, got %q", got)
	}

	f.sessionEndOK(t, scene, "", 2, controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_USER_ENDED)
	trace = append(trace, f.runPhase(scene.runID))
	execution := f.deliverRevision(t, scene)

	// The delivery succeeds the way the contract defines it: the Node reports a Revision and Cloud
	// confirms the objects it declared before registering the row. The success is what releases the
	// run here — the give-up path is D5's fallback, not the mainline.
	in := f.deliveryInputOf(execution)
	if _, e := f.deliveryTerminal(scene, execution, "", 1,
		deliveredResult(scene, in, revisionFinalCommit), "revision_delivered"); e != nil {
		t.Fatalf("the verified delivery must be taken over: %v", e)
	}
	trace = append(trace, f.runPhase(scene.runID))

	f.driveDelete(t, scene.runID)
	trace = append(trace, f.runPhase(scene.runID))

	want := []string{"starting", "running", "delivering", "releasing", "done"}
	if fmt.Sprint(trace) != fmt.Sprint(want) {
		t.Fatalf("the mainline must visit every stage in order:\n got %v\nwant %v", trace, want)
	}
	assertForwardOnly(t, trace)

	// And the end state is the whole end state: Thread ended, the Revision saved and named by the
	// run's own result, status from the session's own end reason, Workspace released.
	phase, status, state := f.threadRunState(scene.runID)
	if phase != "done" || status != "completed" || state != "ended" {
		t.Fatalf("terminal state = %s/%s/%s, want done/completed/ended", phase, status, state)
	}
	row := f.revisionRow(scene.runID)
	if row == nil {
		t.Fatal("the mainline must end with the run's Revision registered")
	}
	result := f.runResult(scene.runID)
	if got := result.S("deliveryState"); got != "saved" {
		t.Fatalf("deliveryState = %q, want saved", got)
	}
	// D4 spells the run's result `{revisionId | null, deliveryState}`; the null is reserved for the
	// give-up path, so a saved delivery must name the row it registered.
	if got := result.S("revisionId"); got != row.ID {
		t.Fatalf("revisionId = %q, want the registered Revision %q", got, row.ID)
	}
	if got := deleteOperations(f.runOperations(scene.runID)); len(got) != 1 || got[0].S("state") != "succeeded" {
		t.Fatalf("the run must end with one succeeded delete, got %v", got)
	}
}

// P5-13, §8/§11/§16 — the delivery mainline of Cloud Revision D4: a Node reports a Revision and Cloud
// confirms the objects it declared, registers the row, releases the run and declares the delete — all
// in the one takeover transaction, with the two object checks that precede it.
//
// Both wire shapes are covered, because they differ in the one thing the row has to get right: a
// delivered checkout carries a bundle, an unchanged one carries none and its final commit IS the
// baseline it was dispatched with.
func TestRevisionDeliveredIsVerifiedRegisteredAndReleasesTheRun(t *testing.T) {
	cases := []struct {
		name          string
		outcome       string
		deliveryState string
		bundle        bool
		result        func(liveThreadScene, core.Object) *controlpb.ExecutionResult
		// verified are the objects Cloud must spend a HEAD on, in D4's order.
		verified func(core.Object) []core.Object
	}{
		{
			name: "a changed checkout declares the bundle and the history", outcome: "revision_delivered",
			deliveryState: "saved", bundle: true,
			result: func(s liveThreadScene, in core.Object) *controlpb.ExecutionResult {
				return deliveredResult(s, in, revisionFinalCommit)
			},
			verified: func(in core.Object) []core.Object {
				return []core.Object{
					{"key": in.S("bundle_key"), "size": int64(revisionBundleSize), "sha256": revisionBundleSHA},
					{"key": in.S("history_key"), "size": int64(revisionHistorySize), "sha256": revisionHistorySHA},
				}
			},
		},
		{
			name: "an unchanged checkout declares only the history", outcome: "revision_unchanged",
			deliveryState: "unchanged",
			result:        unchangedResult,
			verified: func(in core.Object) []core.Object {
				return []core.Object{{"key": in.S("history_key"), "size": int64(revisionHistorySize), "sha256": revisionHistorySHA}}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			objects := f.useObjectStore()
			scene := deliveringScene(t, f)
			execution := f.deliverRevision(t, scene)
			in := f.deliveryInputOf(execution)
			before := f.runVersion(scene.runID)

			if _, e := f.deliveryTerminal(scene, execution, "p5-revision-saved", 1, c.result(scene, in), c.outcome); e != nil {
				t.Fatalf("a verified delivery must be taken over: %v", e)
			}

			// Step 2 ran before the transaction opened, over exactly the objects the result declares and
			// in D4's order — and only those: the grants path is a different action and must not have run.
			if got, want := fmt.Sprint(objects.probed()), fmt.Sprint(c.verified(in)); got != want {
				t.Fatalf("Cloud must verify exactly the declared objects, in D4's order:\n got %s\nwant %s", got, want)
			}
			if got := objects.issuedKeys(); len(got) != 0 {
				t.Fatalf("a delivery takeover must not sign upload grants, got %v", got)
			}

			// Step 3: one row per run, carrying the run's own placement and the attempt's own input.
			row := f.revisionRow(scene.runID)
			if row == nil {
				t.Fatal("a verified delivery must register a Revision")
			}
			if got := f.scalar(`SELECT count(*) FROM revisions WHERE run_id=$1`, scene.runID); got != 1 {
				t.Fatalf("one logical delivery registers exactly one Revision, got %d", got)
			}
			wid := f.runWorkspaceID(scene.runID)
			projectID, repositoryURL := f.runPlacement(scene.runID)
			if row.TenantID != scene.tenantID || row.RunID != scene.runID {
				t.Fatalf("the Revision must belong to the run's own tenant and run, got tenant=%s run=%s", row.TenantID, row.RunID)
			}
			if row.WorkspaceID != wid || row.ProjectID != projectID || row.RepositoryURL != repositoryURL {
				t.Fatalf("the Revision must carry the run Workspace's placement: got ws=%s project=%s repo=%q, want %s/%s/%q",
					row.WorkspaceID, row.ProjectID, row.RepositoryURL, wid, projectID, repositoryURL)
			}
			if row.BaseCommit != in.S("base_commit") || row.RevisionRef != in.S("revision_ref") {
				t.Fatalf("the Revision must carry the attempt's own baseline and ref, got %s/%s", row.BaseCommit, row.RevisionRef)
			}
			// Invariant 5: the session history is saved for every Revision, including an unchanged one.
			if row.HistoryKey != in.S("history_key") || row.HistorySHA256 != revisionHistorySHA || row.HistorySize != revisionHistorySize {
				t.Fatalf("the history measured by the Node must be recorded verbatim, got %v", row)
			}
			if row.ExpiresAt != nil {
				t.Fatalf("expiry is an explicit non-goal: expires_at must stay NULL, got %v", row.ExpiresAt)
			}
			if c.bundle {
				if row.BundleKey != in.S("bundle_key") || row.BundleSHA256 != revisionBundleSHA {
					t.Fatalf("a changed checkout must record the bundle it declared, got %v", row)
				}
				if row.BundleSize == nil || *row.BundleSize != revisionBundleSize {
					t.Fatalf("bundle_size = %v, want %d", row.BundleSize, revisionBundleSize)
				}
				if row.FinalCommit != revisionFinalCommit {
					t.Fatalf("final_commit = %q, want the delivered commit %q", row.FinalCommit, revisionFinalCommit)
				}
			} else {
				// The bundle columns are all-or-nothing and NULL together when nothing changed, so "no
				// bundle" is a fact of the row rather than a sentinel size or an empty digest.
				if row.BundleKey != "" || row.BundleSHA256 != "" || row.BundleSize != nil {
					t.Fatalf("an unchanged delivery must record no bundle, got %v", row)
				}
				if row.FinalCommit != in.S("base_commit") {
					t.Fatalf("an unchanged delivery's final commit IS the baseline, got %q", row.FinalCommit)
				}
			}

			// The settlement: phase, D4's result pair and D3's status, all from the same transaction.
			phase, status, state := f.threadRunState(scene.runID)
			if phase != "releasing" || status != "completed" || state != "ended" {
				t.Fatalf("a saved delivery must release the run, got %s/%s/%s", phase, status, state)
			}
			result := f.runResult(scene.runID)
			if got := result.S("revisionId"); got != row.ID {
				t.Fatalf("revisionId = %q, want the Revision this transaction registered %q", got, row.ID)
			}
			if got := result.S("deliveryState"); got != c.deliveryState {
				t.Fatalf("deliveryState = %q, want %q", got, c.deliveryState)
			}
			if got := f.runVersion(scene.runID); got <= before {
				t.Fatalf("the release must advance the run's version, got %d (before %d)", got, before)
			}
			// The durable result is the Node's own payload: nothing was rewritten, because everything it
			// declared was confirmed.
			if got := f.nodeResult(execution).S("outcome"); got != c.outcome {
				t.Fatalf("a verified delivery must be recorded as reported, got outcome %q want %q", got, c.outcome)
			}
			if got := len(f.deliveryAttempts(scene.runID)); got != 1 {
				t.Fatalf("a settled delivery must release no retry, got %d attempts", got)
			}
			// §12/§17: the delete is declared in the releasing transition's own transaction, and D4's
			// timeline activity reports the two independent outcomes.
			ops := deleteOperations(f.runOperations(scene.runID))
			if len(ops) != 1 || ops[0].S("state") != "queued" || ops[0].S("step") != "quiesce" {
				t.Fatalf("the release must declare exactly one delete intent, got %v", ops)
			}
			details := f.activityDetails(scene.issueID, "run.completed")
			if got := details.S("deliveryState"); got != c.deliveryState {
				t.Fatalf("the timeline activity must report the delivery outcome, got %v", details)
			}
			if got := details.S("sessionEndReason"); got != "user_ended" {
				t.Fatalf("the timeline activity must report the session end reason, got %v", details)
			}
		})
	}
}

// P5-14, §8/§16/§31 — when Cloud cannot confirm the declared objects it records its OWN verdict, never
// the Node's claim (D4 step 4): `failed{verification_failed}` is written in place of the delivered
// result, no Revision row exists, and the run stays `delivering` under D5's retry policy. That is the
// whole reason `verification_failed` is outside the wire's closed set — a Node may not assert a
// verdict about objects Cloud has not looked at.
//
// The second case is the deployment without an object store (D1 invariant 6): a delivered result then
// cannot be verified at all, which is a rejection rather than a skip.
//
// An exact replay of the Node's original payload afterwards must be a no-op and not a conflict. The
// durable result no longer holds what the Node sent — Cloud rewrote it — so a takeover that compared
// against it would refuse every replay of this delivery forever.
func TestRevisionVerificationFailureIsCloudsOwnVerdict(t *testing.T) {
	cases := []struct {
		name string
		// breakStore makes the declared objects unverifiable for this case.
		breakStore func(f *fixture, objects *revisionObjects, in core.Object)
	}{
		{
			name: "a declared object does not match its declaration",
			breakStore: func(_ *fixture, objects *revisionObjects, in core.Object) {
				objects.rejectAt = in.S("bundle_key")
			},
		},
		{
			name: "the deployment has no object store",
			breakStore: func(f *fixture, _ *revisionObjects, _ core.Object) {
				f.store.RevisionObjects = nil
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			objects := f.useObjectStore()
			scene := deliveringScene(t, f)
			execution := f.deliverRevision(t, scene)
			in := f.deliveryInputOf(execution)
			c.breakStore(f, objects, in)

			res := deliveredResult(scene, in, revisionFinalCommit)
			if _, e := f.deliveryTerminal(scene, execution, "p5-verification-failed", 1, res, "revision_delivered"); e != nil {
				t.Fatalf("an unverifiable delivery must be taken over as Cloud's own verdict: %v", e)
			}

			if row := f.revisionRow(scene.runID); row != nil {
				t.Fatalf("no Revision may be registered for objects Cloud could not confirm, got %v", row)
			}
			stored := f.nodeResult(execution)
			if stored.S("outcome") != "revision_failed" || stored.S("reason") != "verification_failed" {
				t.Fatalf("the durable result must be Cloud's own verdict, got %v", stored)
			}
			// The receipt keeps the Node's bytes and payload untouched: it is the basis for the EventAck,
			// and it is the copy the replay below is compared against. Cloud's verdict must not leak
			// into it, or the Node's own retry would look like a conflicting claim.
			claim := f.receiptResult(execution, 1)
			if got := claim.S("outcome"); got != "revision_delivered" {
				t.Fatalf("the receipt must keep the Node's own claim, got %v", claim)
			}
			if _, rewritten := claim["reason"]; rewritten {
				t.Fatalf("Cloud's verdict must not be written into the Node's receipt, got %v", claim)
			}
			if got := claim.S("revisionRef"); got != in.S("revision_ref") {
				t.Fatalf("the receipt must keep the payload's own Revision ref, got %q", got)
			}
			if got := f.nodeSequence(execution); got != 1 {
				t.Fatalf("the verdict must be receipted once, got sequence %d", got)
			}

			// A failure Cloud recorded is a failure the delivery retries (D5): the run keeps its sandbox
			// and gets one new attempt with D5's backoff — it is not released.
			if got := f.runPhase(scene.runID); got != "delivering" {
				t.Fatalf("a failed verification must keep the run delivering, got %q", got)
			}
			if got := f.runResult(scene.runID).S("deliveryState"); got != "" {
				t.Fatalf("a run still delivering must have decided no delivery state, got %q", got)
			}
			if got := len(f.deliveryAttempts(scene.runID)); got != 2 {
				t.Fatalf("a failed verification must release exactly one retry, got %d attempts", got)
			}
			if got := deleteOperations(f.runOperations(scene.runID)); len(got) != 0 {
				t.Fatalf("a run still delivering must have no delete intent, got %v", got)
			}

			// The Node's retry loop is bounded by the receipt: replaying the very payload that was
			// rejected is a deterministic no-op, not a conflict against the rewritten durable result.
			if _, e := f.deliveryTerminal(scene, execution, "", 1, res, "revision_delivered"); e != nil {
				t.Fatalf("an exact replay of a rewritten result must be a no-op, not a fault: %v", e)
			}
			if got := f.nodeSequence(execution); got != 1 {
				t.Fatalf("a replay must not advance the sequence, got %d", got)
			}
			if got := f.receipts(execution); len(got) != 1 {
				t.Fatalf("a replay must not write a receipt, got %v", got)
			}
			if got := len(f.deliveryAttempts(scene.runID)); got != 2 {
				t.Fatalf("a replay must not release a second attempt, got %d", got)
			}
		})
	}
}

// P5-17, §8/§16 — D4 step 1 is deliberately local and I/O-free, and it runs before step 2, so a result
// Cloud can already tell contradicts its own frozen input never reaches the object store: the two HEADs
// step 2 would spend are requests against a store holding objects Cloud has already decided it will not
// register. The refusal takes the same shape as a failed probe — Cloud's own verdict, no Revision row,
// D5's retry — because a payload that disagrees with what the run was dispatched with may not settle it.
//
// Which fields must agree is `revisionResultMatchesInput`'s matrix (unit level, in core); what this test
// fixes is that the integration path performs the comparison *before* the store, once per address the
// payload and the input share.
func TestRevisionResultContradictingItsInputNeverReachesTheObjectStore(t *testing.T) {
	cases := []struct {
		name string
		// contradict rewrites one address the delivered result and the frozen input must agree on.
		contradict func(r *controlpb.RevisionDelivered, in core.Object)
	}{
		{"revision_ref", func(r *controlpb.RevisionDelivered, _ core.Object) {
			r.RevisionRef = "refs/ora/revisions/someone-else"
		}},
		{"base_commit", func(r *controlpb.RevisionDelivered, _ core.Object) {
			r.BaseCommit = strings.Repeat("f", 40)
		}},
		{"bundle_key", func(r *controlpb.RevisionDelivered, in core.Object) {
			r.Bundle.Key = in.S("history_key")
		}},
		{"history_key", func(r *controlpb.RevisionDelivered, in core.Object) {
			r.History.Key = in.S("bundle_key")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			objects := f.useObjectStore()
			scene := deliveringScene(t, f)
			execution := f.deliverRevision(t, scene)
			in := f.deliveryInputOf(execution)

			res := deliveredResult(scene, in, revisionFinalCommit)
			c.contradict(res.GetRevisionDelivered(), in)
			if _, e := f.deliveryTerminal(scene, execution, "p5-input-mismatch", 1, res, "revision_delivered"); e != nil {
				t.Fatalf("a result contradicting its own input is a verdict for Cloud to record, not a fault: %v", e)
			}

			if probed := objects.probed(); len(probed) != 0 {
				t.Fatalf("step 1 must precede step 2: %d object(s) probed for a result Cloud could already refuse (%v)", len(probed), probed)
			}
			if row := f.revisionRow(scene.runID); row != nil {
				t.Fatalf("no Revision may be registered for a result that contradicts its input, got %v", row)
			}
			stored := f.nodeResult(execution)
			if stored.S("outcome") != "revision_failed" || stored.S("reason") != "verification_failed" {
				t.Fatalf("the durable result must be Cloud's own verdict, got %v", stored)
			}
			// The Node's own payload survives in the receipt as it does on the probe-failure path, so
			// the refusal below is compared against what the Node said rather than against Cloud's
			// rewrite — the Node's retry loop stays bounded by its own bytes.
			if got := f.receiptResult(execution, 1).S("revisionRef"); got != res.GetRevisionDelivered().GetRevisionRef() {
				t.Fatalf("the receipt must keep the payload's own Revision ref, got %q", got)
			}
			if _, e := f.deliveryTerminal(scene, execution, "", 1, res, "revision_delivered"); e != nil {
				t.Fatalf("an exact replay of the refused payload must be a no-op, not a fault: %v", e)
			}
			if got := f.nodeSequence(execution); got != 1 {
				t.Fatalf("a replay must not advance the sequence, got %d", got)
			}

			if got := f.runPhase(scene.runID); got != "delivering" {
				t.Fatalf("a refused input comparison must keep the run delivering, got %q", got)
			}
			if got := len(f.deliveryAttempts(scene.runID)); got != 2 {
				t.Fatalf("a refused input comparison must release exactly one retry, got %d attempts", got)
			}
			if got := deleteOperations(f.runOperations(scene.runID)); len(got) != 0 {
				t.Fatalf("a run still delivering must have no delete intent, got %v", got)
			}
		})
	}
}

// P5-18, §20 — D4's last clause and invariant 9 at the integration boundary: a delivery result that
// arrives after the run left `delivering` decides nothing. The delivered shape is the one that matters,
// because it is the only one that would otherwise register a Revision; the run it arrives at has already
// been abandoned by D5 and released, with its delete declared and its result decided.
//
// The result is receipted — the Node's attempt really did happen — and then stopped by the settlement's
// own `phase != delivering` branch, which is why the run's version does not move even though a row was
// written for the receipt.
func TestLateDeliveredRevisionRegistersNothingOnAReleasedRun(t *testing.T) {
	f := setup(t)
	f.useObjectStore()
	scene, execution := givingUpScene(t, f)
	in := f.deliveryInputOf(execution)
	version := f.runVersion(scene.runID)
	status, _ := f.runStatusVersion(scene.runID)

	res := deliveredResult(scene, in, revisionFinalCommit)
	if _, e := f.deliveryTerminal(scene, execution, "p5-late-delivered", 2, res, "revision_delivered"); e != nil {
		t.Fatalf("a late delivered result must be receipted as a fact, not refused: %v", e)
	}

	if row := f.revisionRow(scene.runID); row != nil {
		t.Fatalf("a run that already decided its delivery must register no Revision, got %v", row)
	}
	if got := f.runPhase(scene.runID); got != "releasing" {
		t.Fatalf("a late delivered result must not move the phase, got %q", got)
	}
	if got := f.runVersion(scene.runID); got != version {
		t.Fatalf("a late delivered result must not write the run: version %d → %d", version, got)
	}
	if got, _ := f.runStatusVersion(scene.runID); got != status {
		t.Fatalf("a late delivered result must not rewrite the status, got %q", got)
	}
	result := f.runResult(scene.runID)
	if got := result.S("deliveryState"); got != "failed" {
		t.Fatalf("the release's own verdict must stand, got %v", result)
	}
	if v, ok := result["revisionId"]; !ok || v != nil {
		t.Fatalf("a released run's revisionId must stay an explicit null, got %v (present=%v)", v, ok)
	}
	if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
		t.Fatalf("a late delivered result must not declare a second delete, got %d", got)
	}
	if got := len(f.deliveryAttempts(scene.runID)); got != 1 {
		t.Fatalf("a late delivered result must not release another attempt, got %d", got)
	}
	// Receipted once, at its own sequence, and replay-stable: the fact is recorded, the decision is not.
	if got := f.nodeSequence(execution); got != 2 {
		t.Fatalf("the late result must be receipted at its own sequence, got %d", got)
	}
	if got := len(f.receipts(execution)); got != 2 {
		t.Fatalf("the attempt's two events must have exactly one receipt each, got %d", got)
	}
	if _, e := f.deliveryTerminal(scene, execution, "", 2, res, "revision_delivered"); e != nil {
		t.Fatalf("replaying the late result must be a no-op: %v", e)
	}
	if got := len(f.receipts(execution)); got != 2 {
		t.Fatalf("a replay must not write a receipt, got %d", got)
	}
	if got := f.runVersion(scene.runID); got != version {
		t.Fatalf("a replay must not write the run: version %d → %d", version, got)
	}
}

// deliveryRevisionRow is a `revisions` row as this file seeds one, so a case can state which outcome
// an earlier attempt already registered without restating fifteen columns.
type deliveryRevisionRow struct {
	finalCommit string
	// withBundle selects the changed-checkout shape: the three bundle columns set together, from the
	// same input the delivered attempt carries.
	withBundle bool
}

// seedRevision writes the Revision an earlier attempt left for this run, through the same schema the
// registration writes, and returns its id. The row is seeded rather than reached through a second
// delivery: under the phase machine the first registration moves the run off `delivering` in the same
// transaction that writes the receipt, so the branches below are backstops — and a backstop can only
// be exercised by putting the state it defends against in place.
func (f *fixture) seedRevision(t *testing.T, scene liveThreadScene, in core.Object, row deliveryRevisionRow) string {
	t.Helper()
	id := uuid.NewString()
	wid := f.runWorkspaceID(scene.runID)
	projectID, repositoryURL := f.runPlacement(scene.runID)
	var bundleKey, bundleSHA *string
	var bundleSize *int64
	if row.withBundle {
		key, size, sha := in.S("bundle_key"), int64(revisionBundleSize), revisionBundleSHA
		bundleKey, bundleSize, bundleSHA = &key, &size, &sha
	}
	_, e := f.store.Pool.Exec(`
		INSERT INTO revisions(id, tenant_id, run_id, workspace_id, project_id, repository_url,
		                      base_commit, final_commit, revision_ref,
		                      bundle_key, bundle_size, bundle_sha256,
		                      history_key, history_size, history_sha256)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		id, scene.tenantID, scene.runID, wid, projectID, repositoryURL,
		in.S("base_commit"), row.finalCommit, in.S("revision_ref"),
		bundleKey, bundleSize, bundleSHA,
		in.S("history_key"), revisionHistorySize, revisionHistorySHA)
	must(t, e)
	return id
}

// P5-15, §19/§21 — invariant 7's backstop, both directions. `UNIQUE (run_id)` makes a run's Revision
// unique however many delivery attempts it took, and the takeover compares the stored row against the
// payload rather than trusting the insert: a row that already describes THIS outcome is reused (the
// registration is idempotent, and the run is settled on the id the earlier attempt registered), while
// a row that describes a DIFFERENT one is an invariant failure rather than a second registration.
//
// The rejection rolls the whole takeover back — receipt, durable result, Revision and settlement —
// because keeping any of them would leave Cloud believing half of a delivery it could not reconcile.
// The reuse case is what makes the comparison a comparison rather than "any existing row is a
// conflict": it is also where the identity round trip is exercised, because the delivered payload and
// the stored row spell the same Revision in different ways and must still reduce to one identity.
func TestRevisionRegistrationReusesAnIdenticalRowAndRollsBackADifferentOne(t *testing.T) {
	cases := []struct {
		name string
		// registered is what an earlier attempt already left for this run.
		registered deliveryRevisionRow
		// delivered is the terminal result this attempt reports.
		delivered func(scene liveThreadScene, in core.Object) *controlpb.ExecutionResult
		// wantRegistered is the delivery state the settlement must record, or "" when the takeover must
		// be refused instead.
		wantRegistered string
	}{
		{
			name:       "the same outcome an earlier attempt already registered",
			registered: deliveryRevisionRow{finalCommit: revisionFinalCommit, withBundle: true},
			delivered: func(scene liveThreadScene, in core.Object) *controlpb.ExecutionResult {
				return deliveredResult(scene, in, revisionFinalCommit)
			},
			wantRegistered: "saved",
		},
		{
			name:       "a different final commit",
			registered: deliveryRevisionRow{finalCommit: strings.Repeat("c", 40), withBundle: true},
			delivered: func(scene liveThreadScene, in core.Object) *controlpb.ExecutionResult {
				return deliveredResult(scene, in, revisionFinalCommit)
			},
		},
		{
			name:       "an unchanged delivery against a Revision that saved a bundle",
			registered: deliveryRevisionRow{finalCommit: revisionFinalCommit, withBundle: true},
			delivered: func(scene liveThreadScene, in core.Object) *controlpb.ExecutionResult {
				return unchangedResult(scene, in)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			f.useObjectStore()
			scene := deliveringScene(t, f)
			execution := f.deliverRevision(t, scene)
			in := f.deliveryInputOf(execution)
			before := f.runVersion(scene.runID)
			registeredID := f.seedRevision(t, scene, in, c.registered)
			res := c.delivered(scene, in)

			takeover := errOnly(f.deliveryTerminal(scene, execution, "p5-revision-conflict", 1, res, "revision_delivered"))
			if c.wantRegistered == "" {
				expectStatus(t, takeover, codes.Unavailable, controlpb.ErrorCode_ERROR_CODE_UNAVAILABLE)

				// Nothing committed: no receipt, no durable result, no sequence move, no retry, no release.
				if got := f.receipts(execution); len(got) != 0 {
					t.Fatalf("a conflicting Revision must roll the receipt back, got %v", got)
				}
				if got := f.nodeResult(execution); got != nil {
					t.Fatalf("a conflicting Revision must roll the durable result back, got %v", got)
				}
				if got := f.nodeSequence(execution); got != 0 {
					t.Fatalf("a conflicting Revision must not advance the sequence, got %d", got)
				}
				if got := len(f.deliveryAttempts(scene.runID)); got != 1 {
					t.Fatalf("a conflicting Revision must release no retry, got %d attempts", got)
				}
				if got := f.runPhase(scene.runID); got != "delivering" {
					t.Fatalf("a conflicting Revision must not move the run, got phase %q", got)
				}
				if got := f.runVersion(scene.runID); got != before {
					t.Fatalf("a conflicting Revision must not write the run: version %d → %d", before, got)
				}
				if got := deleteOperations(f.runOperations(scene.runID)); len(got) != 0 {
					t.Fatalf("a conflicting Revision must declare no delete, got %v", got)
				}
				// The row that was already there is left exactly as it was: a registration never
				// overwrites one, and this one describes an outcome Cloud could not reconcile.
				if got := f.scalar(`SELECT count(*) FROM revisions WHERE run_id=$1`, scene.runID); got != 1 {
					t.Fatalf("the existing Revision must not be duplicated, got %d rows", got)
				}
				row := f.revisionRow(scene.runID)
				if row == nil || row.ID != registeredID || row.FinalCommit != c.registered.finalCommit {
					t.Fatalf("the existing Revision must be left untouched, got %v", row)
				}
				return
			}

			must(t, takeover)
			// The same outcome, registered once: the row the earlier attempt wrote is the Revision this
			// delivery is settled on, and no second row appears.
			if got := f.scalar(`SELECT count(*) FROM revisions WHERE run_id=$1`, scene.runID); got != 1 {
				t.Fatalf("an identical outcome must not register a second Revision, got %d rows", got)
			}
			row := f.revisionRow(scene.runID)
			if row == nil || row.ID != registeredID {
				t.Fatalf("the settlement must reuse the id already registered for this run, got %v", row)
			}
			if got := f.runPhase(scene.runID); got != "releasing" {
				t.Fatalf("a verified delivery must release the run, got phase %q", got)
			}
			result := f.runResult(scene.runID)
			if got := result.S("deliveryState"); got != c.wantRegistered {
				t.Fatalf("deliveryState = %q, want %q", got, c.wantRegistered)
			}
			if got := result.S("revisionId"); got != registeredID {
				t.Fatalf("revisionId = %q, want the registered %q", got, registeredID)
			}
			if got := f.nodeSequence(execution); got != 1 {
				t.Fatalf("the reused registration must be receipted once, got sequence %d", got)
			}
			if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
				t.Fatalf("the release must declare exactly one delete, got %d", got)
			}
		})
	}
}

// P5-16, §18/§22 — D2/D3's upload grants over the contract the Controller actually speaks. Cloud signs
// one presigned PUT per object key of that attempt's frozen input, and each grant names exactly one
// key and one method: the URL carries the capability, so nothing the Node does can widen it to another
// object, and nothing is persisted — the grants exist only in the reply.
//
// The refusals are D3's ordered set, asserted here because the Controller's retry policy depends on
// which one it gets: UNAVAILABLE means "a capability is missing, keep the attempt", 404 means "Cloud
// never registered this execution", and 409 means "this attempt can no longer upload anything".
func TestGrantRevisionUploadSignsOneGrantPerObjectKey(t *testing.T) {
	f := setup(t)
	objects := f.useObjectStore()
	scene := deliveringScene(t, f)
	execution := f.deliverRevision(t, scene)
	in := f.deliveryInputOf(execution)
	client := controlpb.NewAgentRunServiceClient(f.controlConn)
	grant := func(executionID string) (*controlpb.GrantRevisionUploadResponse, error) {
		return client.GrantRevisionUpload(asController("ctrl-a"), &controlpb.GrantRevisionUploadRequest{Epoch: 1, ExecutionId: executionID})
	}
	grantErr := func(executionID string) error {
		_, e := grant(executionID)
		return e
	}

	out, e := grant(execution)
	must(t, e)
	if len(out.GetGrants()) != 2 {
		t.Fatalf("one attempt gets one grant per object key of its input, got %d", len(out.GetGrants()))
	}
	wantKeys := []string{in.S("bundle_key"), in.S("history_key")}
	for i, g := range out.GetGrants() {
		if g.GetObjectKey() != wantKeys[i] {
			t.Fatalf("grant %d names %q, want the input's own key %q", i, g.GetObjectKey(), wantKeys[i])
		}
		if g.GetMethod() != http.MethodPut {
			t.Fatalf("grant %d method = %q, want PUT", i, g.GetMethod())
		}
		if !strings.Contains(g.GetUrl(), wantKeys[i]) {
			t.Fatalf("grant %d URL %q must be bound to its own key %q", i, g.GetUrl(), wantKeys[i])
		}
		if len(g.GetHeaders()) != 0 {
			t.Fatalf("the first version's grant binds no header, got %v", g.GetHeaders())
		}
	}
	// The object store saw the same keys Cloud returned, and the lifetime is D1's configured one.
	if got := fmt.Sprint(objects.issuedKeys()); got != fmt.Sprint(wantKeys) {
		t.Fatalf("Cloud must sign the attempt's own keys:\n got %s\nwant %s", got, fmt.Sprint(wantKeys))
	}
	if got := objects.issuedTTL(); got != core.DefaultRevisionUploadTTL {
		t.Fatalf("grant lifetime = %s, want D1's default %s", got, core.DefaultRevisionUploadTTL)
	}
	// A grant is a capability, not a record: signing one writes nothing. The two durable rows the
	// attempt is made of are compared whole across a second, repeated grant, because "the input is
	// unchanged" is a claim about every column of both — a grant path that stamped a nonce into the
	// work item would break the attempt's idempotency and no narrower assertion would catch it.
	executionRow, workInput := f.deliveryRows(execution)
	repeat, e := grant(execution)
	must(t, e)
	if len(repeat.GetGrants()) != 2 {
		t.Fatalf("an attempt that has not reported can ask for its grants again, got %d", len(repeat.GetGrants()))
	}
	if got, want := repeat.GetGrants()[0].GetObjectKey(), wantKeys[0]; got != want {
		t.Fatalf("a repeated grant names %q, want the same key %q", got, want)
	}
	if afterExecution, afterInput := f.deliveryRows(execution); afterExecution != executionRow || afterInput != workInput {
		t.Fatalf("granting must not write the attempt's own rows:\n execution %s → %s\n work input %s → %s",
			executionRow, afterExecution, workInput, afterInput)
	}
	if got := f.scalar(`SELECT count(*) FROM revisions WHERE run_id=$1`, scene.runID); got != 0 {
		t.Fatalf("granting must register nothing, got %d Revisions", got)
	}
	// And nothing in the schema could hold one: a grant is minted per request and never stored, so no
	// table may have a column an upload URL or a credential could live in.
	if got := f.scalar(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema=current_schema() AND (column_name ILIKE '%grant%' OR column_name ILIKE '%presign%')`); got != 0 {
		t.Fatalf("the schema must have nowhere to persist an upload grant, got %d column(s)", got)
	}

	// Unknown execution: Cloud never registered it.
	expectStatus(t, grantErr("exec-delivery-nobody"), codes.NotFound, controlpb.ErrorCode_ERROR_CODE_NOT_FOUND)

	// A session execution: its identity is not an upload subject, whatever its Node asks for.
	expectStatus(t, grantErr(scene.executionID), codes.Aborted, controlpb.ErrorCode_ERROR_CODE_CONFLICT)

	// An attempt that already has a result is over: its objects can no longer be referred to by any
	// registration, so authorizing a write would only create garbage.
	if _, e := f.deliveryTerminal(scene, execution, "p5-grant-after-result", 1,
		revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
		"revision_failed/upload_failed"); e != nil {
		t.Fatalf("the failed delivery must be taken over: %v", e)
	}
	expectStatus(t, grantErr(execution), codes.Aborted, controlpb.ErrorCode_ERROR_CODE_CONFLICT)

	// A registered attempt that has NOT reported yet, while the run has already been released: the
	// logical delivery is settled, so no further attempt exists to upload for. This is reachable only
	// through D5's give-up pass, which releases the run while an attempt is still outstanding — the
	// attempt that would otherwise wait out its backoff is never granted, and neither is one that was
	// already registered when the window closed.
	//
	// The pass is driven on a run whose only attempt is still live, and it is driven directly rather
	// than through a terminal failure, because that is the state D3's refusal defends against: the
	// execution is addressable and result-free, and the run behind it has moved on.
	f2 := setup(t)
	f2.useObjectStore()
	client2 := controlpb.NewAgentRunServiceClient(f2.controlConn)
	grantErr2 := func(executionID string) error {
		_, e := client2.GrantRevisionUpload(asController("ctrl-a"), &controlpb.GrantRevisionUploadRequest{Epoch: 1, ExecutionId: executionID})
		return e
	}
	scene2 := deliveringScene(t, f2)
	live := f2.deliverRevision(t, scene2)
	f2.store.DeliveryGiveUpAfter = time.Nanosecond
	must(t, f2.store.GiveUpStaleDeliveriesOnce(context.Background()))
	if got := f2.runPhase(scene2.runID); got != "releasing" {
		t.Fatalf("the give-up must release the run, got %q", got)
	}
	expectStatus(t, grantErr2(live), codes.Aborted, controlpb.ErrorCode_ERROR_CODE_CONFLICT)

	// The deployment has no object store: a missing capability, not a bad request. The refusal is the
	// same for every execution, which is why it is decided before the execution is even read.
	f2.store.RevisionObjects = nil
	expectStatus(t, grantErr2(live), codes.Unavailable, controlpb.ErrorCode_ERROR_CODE_UNAVAILABLE)
}

// P5-14, §19 — the replay matrix. Every shape a Controller's retry can take is one effect: a
// submission replay, a byte-identical re-send, and a re-send whose payload disagrees. The conflict
// cases are asserted alongside because they are the same identity: the receipt is what makes a
// re-send a no-op or a conflict, never a second settlement.
func TestDeliveryReplayMatrix(t *testing.T) {
	f := setup(t)
	scene := deliveringScene(t, f)
	execution := f.deliverRevision(t, scene)
	result := revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED)
	const event = "revision_failed/upload_failed"

	if _, e := f.deliveryTerminal(scene, execution, "p5-replay-1", 1, result, event); e != nil {
		t.Fatalf("the first delivery result must be taken over: %v", e)
	}
	attempts := f.deliveryAttempts(scene.runID)
	if len(attempts) != 2 {
		t.Fatalf("the failure must release exactly one retry, got %d", len(attempts))
	}
	version := f.runVersion(scene.runID)
	receipts := f.receipts(execution)

	// (a) Same submission identity: the recorded response is replayed and nothing runs.
	if _, e := f.deliveryTerminal(scene, execution, "p5-replay-1", 1, result, event); e != nil {
		t.Fatalf("a submission replay must succeed, got %v", e)
	}
	// (b) No submission identity, same bytes and result: the receipt makes it a deterministic no-op.
	if _, e := f.deliveryTerminal(scene, execution, "", 1, result, event); e != nil {
		t.Fatalf("a byte-identical replay must be a no-op, not a fault: %v", e)
	}
	// (c) Same sequence, different bytes, and same bytes with a different result.
	expectStatus(t, errOnly(f.deliveryTerminal(scene, execution, "", 1, result, "revision_failed/something_else")),
		codes.Aborted, controlpb.ErrorCode_ERROR_CODE_CONFLICT)
	expectStatus(t, errOnly(f.deliveryTerminal(scene, execution, "", 1,
		revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_BUNDLE_FAILED), event)),
		codes.Aborted, controlpb.ErrorCode_ERROR_CODE_CONFLICT)

	if got := fmt.Sprint(f.deliveryAttempts(scene.runID)); got != fmt.Sprint(attempts) {
		t.Fatalf("a replay must not release a second attempt:\n got %v\nwant %v", got, attempts)
	}
	if got := fmt.Sprint(f.receipts(execution)); got != fmt.Sprint(receipts) {
		t.Fatalf("a replay must not write a receipt: got %v want %v", got, receipts)
	}
	if got := f.nodeSequence(execution); got != 1 {
		t.Fatalf("a replay must not advance the sequence, got %d", got)
	}
	if got := f.runVersion(scene.runID); got != version {
		t.Fatalf("a replay must not write the run: version %d → %d", version, got)
	}
	if got := f.runPhase(scene.runID); got != "delivering" {
		t.Fatalf("a replay must not move the phase, got %q", got)
	}
}

// P5-15, §20 — the serialization matrix. Each case starts every writer from one barrier and asserts
// the complete set of legal outcomes rather than the most common one; the invariant under all of them
// is that one release is one release: one delete intent, one retry, one receipt.
func TestDeliverySerializesWithConcurrentWriters(t *testing.T) {
	t.Run("the same failed result sent twice", func(t *testing.T) {
		f := setup(t)
		scene := deliveringScene(t, f)
		execution := f.deliverRevision(t, scene)

		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		wg.Add(2)
		for i := range errs {
			go func(i int) {
				defer wg.Done()
				<-start
				_, errs[i] = f.deliveryTerminal(scene, execution, "", 1,
					revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
					"revision_failed/upload_failed")
			}(i)
		}
		close(start)
		wg.Wait()

		// Both may succeed: one commits and the other is the byte-identical replay of a committed
		// event. Neither may fault, because a fault would make the Node replay forever.
		for i, e := range errs {
			if e != nil {
				t.Fatalf("concurrent duplicate delivery result %d must be a no-op, not a fault: %v", i, e)
			}
		}
		if got := f.receipts(execution); len(got) != 1 {
			t.Fatalf("the duplicate must produce exactly one receipt, got %v", got)
		}
		if got := f.nodeSequence(execution); got != 1 {
			t.Fatalf("the duplicate must advance the sequence once, got %d", got)
		}
		if got := len(f.deliveryAttempts(scene.runID)); got != 2 {
			t.Fatalf("the duplicate must release exactly one retry, got %d attempts", got)
		}
		if got := f.runPhase(scene.runID); got != "delivering" {
			t.Fatalf("the run must stay delivering, got %q", got)
		}
	})

	t.Run("the give-up pass ticks while the delivery settles", func(t *testing.T) {
		f := setup(t)
		scene := deliveringScene(t, f)
		execution := f.deliverRevision(t, scene)
		f.store.DeliveryGiveUpAfter = time.Nanosecond

		var wg sync.WaitGroup
		start := make(chan struct{})
		var passErr, takeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			passErr = f.store.GiveUpStaleDeliveriesOnce(context.Background())
		}()
		go func() {
			defer wg.Done()
			<-start
			_, takeErr = f.deliveryTerminal(scene, execution, "", 1,
				revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
				"revision_failed/upload_failed")
		}()
		close(start)
		wg.Wait()

		must(t, passErr)
		if takeErr != nil {
			t.Fatalf("the delivery takeover must win or lose legally, got %v", takeErr)
		}
		// Whichever writer won, the outcome is the same single release: one phase move, one delete
		// intent, one recorded failure, and never a second attempt declared after the release.
		//
		// The attempt count is exactly one, not "one or two", because both writers evaluate the same
		// give-up predicate under the same serialization: the pass releases the run, and the takeover
		// either settles it while the window is already open (release, no retry) or arrives after the
		// release and takes the settlement's no-op branch (also no retry). The retry branch is
		// unreachable while `DeliveryGiveUpAfter` is open, so a count that varied would be a real bug.
		if got := f.runPhase(scene.runID); got != "releasing" {
			t.Fatalf("the run must end releasing, got %q", got)
		}
		if got := f.runResult(scene.runID).S("deliveryState"); got != "failed" {
			t.Fatalf("deliveryState = %q, want failed", got)
		}
		if got := f.nodeSequence(execution); got != 1 {
			t.Fatalf("the failure must be receipted exactly once, got sequence %d", got)
		}
		if got := len(f.deliveryAttempts(scene.runID)); got != 1 {
			t.Fatalf("the release must not leave a second attempt behind, got %d", got)
		}
		if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
			t.Fatalf("exactly one delete intent may exist, got %d", got)
		}
	})

	t.Run("the delete reconciliation ticks while the run is released", func(t *testing.T) {
		f := setup(t)
		scene := deliveringScene(t, f)
		execution := f.deliverRevision(t, scene)
		f.store.DeliveryGiveUpAfter = time.Nanosecond

		var wg sync.WaitGroup
		start := make(chan struct{})
		var passErr, takeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			passErr = f.store.RedeclareRunWorkspaceDeletesOnce(context.Background())
		}()
		go func() {
			defer wg.Done()
			<-start
			_, takeErr = f.deliveryTerminal(scene, execution, "", 1,
				revisionFailedResult(scene, controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED),
				"revision_failed/upload_failed")
		}()
		close(start)
		wg.Wait()

		must(t, passErr)
		if takeErr != nil {
			t.Fatalf("the delivery takeover must win or lose legally, got %v", takeErr)
		}
		if got := f.runPhase(scene.runID); got != "releasing" {
			t.Fatalf("the run must end releasing, got %q", got)
		}
		if got := len(deleteOperations(f.runOperations(scene.runID))); got != 1 {
			t.Fatalf("two writers must still declare exactly one delete, got %d", got)
		}
	})
}

// P5-16, §12/§13 — a Node that refuses to quiesce fails the operation and restores the Workspace
// (operation D4's "quiesce 失败并按原规则重试"), and for a run Workspace the party that retries is
// Cloud: the run has no public API and no user. Without the reconciliation pass such a run would sit
// in `releasing` forever with no operation and no hook.
func TestRefusedQuiesceIsRedeclaredAndStillReachesDone(t *testing.T) {
	f := setup(t)
	scene, _ := givingUpScene(t, f)
	wid := f.runWorkspaceID(scene.runID)

	// The scene's Node is seeded as the sandbox's Node; give it the substrate identity and the
	// service subject a real Node credential carries, so the refusal below travels the production
	// node-control path rather than a stub.
	f.bindRunWorkspaceSandbox(t, scene.runID)

	deleteOp := deleteOperations(f.runOperations(scene.runID))
	if len(deleteOp) != 1 {
		t.Fatalf("the scene must hold one delete intent, got %d", len(deleteOp))
	}
	opID := deleteOp[0].S("id")

	// The Node refuses: it still has work in the Workspace. Node control answers the refusal as a
	// well-formed verdict rather than a transport fault — HTTP 200 with `accepted: false` — which is
	// exactly why the operation must interpret it, not merely observe a non-200. The operation fails
	// and the Workspace returns to the state its snapshot recorded.
	body, status, e := f.refuseQuiesce(opID, wid)
	must(t, e)
	if status != http.StatusOK {
		t.Fatalf("a Node's refusal is a verdict, not a fault: got HTTP %d (%v)", status, body)
	}
	if body.B("accepted") || body.S("errorCode") != "resource_in_use" {
		t.Fatalf("the refusal must report resource_in_use, got %v", body)
	}
	ops := deleteOperations(f.runOperations(scene.runID))
	if len(ops) != 1 || ops[0].S("state") != "failed" {
		t.Fatalf("a refused quiesce must fail the operation, got %v", ops)
	}
	if got := ops[0].S("errorCode"); got != "resource_in_use" {
		t.Fatalf("the failed operation must record the Node's own refusal code, got %q", got)
	}
	if got := f.runPhase(scene.runID); got != "releasing" {
		t.Fatalf("the run must stay releasing, got %q", got)
	}
	ws := f.workspaceFields(wid)
	if !ws.B("admissionOpen") || ws.B("deleted") {
		t.Fatalf("a refused quiesce must restore the Workspace, got %v", ws)
	}

	// Cloud re-declares: the intent is durable, only its operation was refused. A second pass with an
	// operation already in flight declares nothing, so the reconciliation is idempotent.
	must(t, f.store.RedeclareRunWorkspaceDeletesOnce(context.Background()))
	ops = deleteOperations(f.runOperations(scene.runID))
	if len(ops) != 2 {
		t.Fatalf("the reconciliation must declare a fresh delete, got %d", len(ops))
	}
	if ops[1].S("state") != "queued" || ops[1].S("idempotencyKey") != ops[0].S("idempotencyKey") {
		t.Fatalf("the re-declaration must be the same intent, got %v", ops[1])
	}
	must(t, f.store.RedeclareRunWorkspaceDeletesOnce(context.Background()))
	if got := len(deleteOperations(f.runOperations(scene.runID))); got != 2 {
		t.Fatalf("a second pass with an operation in flight must declare nothing, got %d", got)
	}

	// And the run still finishes: this time the Node confirms idle.
	f.driveDelete(t, scene.runID)
	if got := f.runPhase(scene.runID); got != "done" {
		t.Fatalf("the re-declared delete must settle the run, got %q", got)
	}
	ops = deleteOperations(f.runOperations(scene.runID))
	if ops[len(ops)-1].S("state") != "succeeded" {
		t.Fatalf("the re-declared delete must reach succeeded, got %v", ops)
	}
}

// refuseQuiesce reports a Node's refusal through the production node-control action: the credential
// is the Node's own, the admission epoch is the current one, and the request names the operation
// whose quiesce it is refusing. It returns the endpoint's own body and status so the caller asserts
// the contract's answer rather than a transport symptom.
func (f *fixture) refuseQuiesce(opID, wid string) (core.Object, int, error) {
	f.t.Helper()
	var version int64
	must(f.t, f.store.Pool.QueryRow(`SELECT version FROM node_instances WHERE workspace_id=$1 AND ended_at IS NULL`, wid).Scan(&version))
	body := core.Object{"version": version, "admissionEpoch": f.workspaceFields(wid).N("admissionEpoch"), "operationId": opID, "idle": false}
	return f.client.Call(context.Background(), "POST", "/internal/v1/nodes/idle", "node", f.node(wid), nil, "", body)
}
