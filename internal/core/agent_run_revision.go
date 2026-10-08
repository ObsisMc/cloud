package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The Revision half of the delivery path: what Cloud must be able to do to a Revision object before
// it may register one (Cloud Revision D1–D4).
//
// The decision separates verification from registration on purpose. Steps 1 and 2 — the input
// consistency and shape comparison, and the object-store HEAD that confirms every declared object
// exists with the declared size and SHA-256 — happen OUTSIDE the takeover transaction, because this
// repository forbids holding a transaction across network I/O (AGENTS.md "Keep transactions short and
// database-only"). Step 3 — the receipt, the durable result, the `revisions` row and the business hook
// — then commits as one transaction, so there is no state in which a Revision exists without the run
// having advanced, or the run advanced without its Revision.
//
// The read that step 1 needs is deliberately made before the transaction too: the delivery input is
// the authority the result is compared against, and the comparison must hold before Cloud spends any
// request on the objects. The takeover re-reads the same row and re-runs the same comparison under the
// advisory lock, so a change between the two reads is an invariant failure rather than a registration
// against an input Cloud no longer believes.

// UploadGrant is one single-key, single-method, time-limited capability to write one Revision object
// (Cloud Revision D2/D3). It carries no credential: a Node that holds it can write exactly the key it
// names, exactly once over the grant's lifetime, and nothing else (invariant 2).
type UploadGrant struct {
	// ObjectKey is the one key the grant authorizes.
	ObjectKey string

	// URL is the presigned request to send; Method is the method to send it with.
	URL    string
	Method string

	// Headers are the headers the upload must carry unchanged. It is empty in the first version: the
	// only header the delivery contract requires the Node to send is the digest it computes itself,
	// which Cloud cannot know when it signs (see objectstore.signPresignedPut).
	Headers map[string]string

	// ExpiresAt is the instant the grant stops working. A Node that still needs to upload after it
	// asks for a new grant rather than failing the delivery (D3).
	ExpiresAt time.Time
}

// RevisionObjects is the object-store boundary the delivery path consumes (Cloud Revision D1). It is
// defined here, at the consuming boundary: the production implementation is internal/objectstore's
// S3 client, and a Store with a nil RevisionObjects is a Cloud with no object store configured —
// legal to run (D1), but unable to issue a grant or to register a Revision.
type RevisionObjects interface {
	// PresignPut returns the upload grant for exactly one object key.
	PresignPut(objectKey string, ttl time.Duration) (UploadGrant, error)

	// Verify reports whether the object at objectKey exists with exactly the declared size and
	// lowercase hex SHA-256. Any failure — absent object, differing size or digest, unreachable
	// endpoint — is an error, because all of them mean the same thing to the delivery (D1).
	Verify(ctx context.Context, objectKey string, size int64, sha256 string) error
}

// DefaultRevisionUploadTTL is Cloud Revision D1's first-version upload-grant lifetime, applied when
// the deployment leaves `object_store.upload_grant_ttl` unset or non-positive. It lives here, beside
// the field it defaults, so config and the zero-value Store cannot disagree about it.
const DefaultRevisionUploadTTL = 15 * time.Minute

// revisionUploadTTL is the configured grant lifetime, or D1's default when a Store was built without
// the deployment wiring (a zero value is "not configured", never a zero-length grant).
func (s *Store) revisionUploadTTL() time.Duration {
	if s.RevisionUploadTTL > 0 {
		return s.RevisionUploadTTL
	}
	return DefaultRevisionUploadTTL
}

// revisionVerificationFailure is the one delivery failure reason Cloud records itself: the objects
// the Node declared do not exist, or do not match what it declared (D4 step 4). It is deliberately
// absent from revisionFailureReasons — the wire's closed set — because accepting it from a Node would
// let the Node assert a verdict about objects Cloud has not looked at.
const revisionVerificationFailure = "verification_failed"

// revisionVerdict is ADR D4 steps 1–2's answer, computed before the registration transaction opens.
type revisionVerdict int

const (
	// revisionNotVerified: the result needs no object verification — it is a Node-reported failure, or
	// the request is not a delivery registration the takeover would accept at all.
	revisionNotVerified revisionVerdict = iota

	// revisionObjectsVerified: every declared object exists with the declared size and SHA-256, so the
	// takeover may register the Revision.
	revisionObjectsVerified

	// revisionObjectsRejected: step 1 or step 2 did not hold, so the takeover records
	// `failed{verification_failed}` instead of registering anything.
	revisionObjectsRejected
)

// verifyDeliveryObjects runs ADR D4 steps 1–2 for one delivery terminal event. It is called by
// Control before the transaction opens, and it never rejects a request itself: anything it cannot
// judge (an unknown outcome, an unknown execution, a result from another Node) is answered
// revisionNotVerified, and the takeover decides it against authoritative rows.
//
// A delivered or unchanged result while Cloud has no object store configured is a rejection rather
// than a skip: Cloud cannot have verified objects it has no way to look at, so the delivery fails and
// IssueRun D5's window closes the run out (D1 invariant 6).
func (s *Store) verifyDeliveryObjects(ctx context.Context, r *ControlRequest) revisionVerdict {
	result := r.Body.O("result")
	outcome, ok := deliveryResult(result)
	if !ok || outcome.Kind == DeliveryFailed {
		return revisionNotVerified
	}
	execution := s.deliveryExecution(ctx, r.Body.S("executionId"), r.Body.S("operationId"))
	if execution == nil || execution.S("kind") != "deliver_revision" {
		return revisionNotVerified
	}
	if result.O("node").S("nodeId") != execution.S("nodeId") {
		// A result naming another Node is a conflict the takeover refuses; verifying objects for it
		// would be work spent on a payload Cloud is about to reject.
		return revisionNotVerified
	}
	if !revisionResultMatchesInput(result, execution.O("input")) {
		return revisionObjectsRejected
	}
	if s.RevisionObjects == nil {
		return revisionObjectsRejected
	}
	for _, object := range declaredRevisionObjects(result) {
		if err := s.RevisionObjects.Verify(ctx, object.S("key"), object.N("size"), object.S("sha256")); err != nil {
			return revisionObjectsRejected
		}
	}
	return revisionObjectsVerified
}

// deliveryExecution reads one registered execution's verification inputs before the takeover
// transaction opens: its kind, its Node and the immutable input the result is compared against. A
// missing row (or a malformed execution id) answers nil, leaving the refusal to the takeover, which
// owns the Fault contract. The read is not transactional and is not trusted for anything but this
// pre-flight; every fact the takeover commits is re-read inside it.
func (s *Store) deliveryExecution(ctx context.Context, executionID, operationID string) Object {
	if executionID == "" || len(executionID) > 200 || !validID(operationID) {
		return nil
	}
	var raw []byte
	err := s.Pool.QueryRowContext(ctx, `
		SELECT row_to_json(e) FROM (
			SELECT execution_id, kind, node_id, input FROM node_executions
			WHERE execution_id=$1 AND operation_id=$2) e`, executionID, operationID).Scan(&raw)
	if err != nil {
		// sql.ErrNoRows is the ordinary "not registered" case; any other failure is also answered as
		// "nothing to verify" so the transaction reports it through the Fault contract it owns.
		if !errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return nil
	}
	var row Object
	if json.Unmarshal(raw, &row) != nil {
		return nil
	}
	out := Object{}
	for k, v := range row {
		out[camel(k)] = v
	}
	return out
}

// revisionResultMatchesInput is ADR D4 step 1: the delivery input consistency and shape check. It is
// a local comparison with no I/O, and it is total — every key it reads may be absent, and an absent
// key never matches a non-empty input.
//
// It deliberately does NOT check anything the ADR excludes: no Git lineage between the two commits,
// no bundle applicability, no JSONL parseability, no content type. Ownership needs no check either:
// the keys carry the tenant and the run, and the grant only ever authorized those two keys.
func revisionResultMatchesInput(result, input Object) bool {
	if len(input) == 0 || len(result) == 0 {
		return false
	}
	ref, base := input.S("revision_ref"), input.S("base_commit")
	bundleKey, historyKey := input.S("bundle_key"), input.S("history_key")
	// A delivery input Cloud cannot compare against is Cloud's own corrupted state: an input without
	// these four is not a DeliverRevisionSpec, and never a match.
	if ref == "" || !commitID(base) || bundleKey == "" || historyKey == "" {
		return false
	}
	if result.S("revisionRef") != ref || result.S("baseCommit") != base {
		return false
	}
	if !commitID(result.S("finalCommit")) {
		return false
	}
	if !validRevisionObject(result.O("history"), historyKey) {
		return false
	}
	switch result.S("outcome") {
	case revisionUnchangedOutcome:
		// "Nothing changed" is a claim Cloud can check locally, and it must: an unchanged result that
		// declared a bundle, or a different final commit, would register a Revision against a commit
		// no object backs.
		return len(result.O("bundle")) == 0 && result.S("finalCommit") == base
	case revisionDeliveredOutcome:
		return validRevisionObject(result.O("bundle"), bundleKey)
	default:
		return false
	}
}

// declaredRevisionObjects lists the objects a delivered or unchanged result claims, in the order the
// ADR names them: the bundle (only when the checkout changed) and then the session history. An absent
// measurement is skipped rather than turned into a zero-valued one, so a malformed result cannot make
// Cloud probe an empty key.
func declaredRevisionObjects(result Object) []Object {
	out := make([]Object, 0, 2)
	if bundle := result.O("bundle"); len(bundle) > 0 {
		out = append(out, bundle)
	}
	if history := result.O("history"); len(history) > 0 {
		out = append(out, history)
	}
	return out
}

// validRevisionObject checks one declared object measurement: it must name exactly the key the input
// assigned it, and carry a usable size and digest. The digest is normalized rather than compared as
// text, so a Node that spells the same digest in upper case is accepted (D4 compares digests) while
// anything that is not a 64-digit hex digest is refused before it can reach the object store.
func validRevisionObject(object Object, key string) bool {
	if object.S("key") != key || object.N("size") < 0 {
		return false
	}
	_, ok := revisionDigest(object.S("sha256"))
	return ok
}

// revisionDigest normalizes a declared SHA-256 to the lowercase hex form D4 compares in. ok=false
// means the value is not a digest at all — including the empty string, which is the case that matters
// most, because an absent digest must never be compared against a stored one.
func revisionDigest(value string) (string, bool) {
	if len(value) != 64 {
		return "", false
	}
	out := make([]byte, 0, 64)
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
			c += 'a' - 'A'
		default:
			return "", false
		}
		out = append(out, c)
	}
	return string(out), true
}

// revisionUploadGrant answers Controller grant requests (control action "agent_revision_grant",
// served by AgentRunService.GrantRevisionUpload). It is a pure read — presigning writes nothing, and
// it is crypto rather than I/O, so it may run inside the control transaction.
//
// Cloud's refusals, in the order they are decided (D3):
//
//	object store unconfigured  → UNAVAILABLE: a missing capability, not a bad request. The refusal
//	                             writes nothing, so the delivery simply never gets a grant and
//	                             IssueRun D5's window closes the run out.
//	unknown execution          → 404: the Controller named an execution Cloud never registered.
//	not a delivery execution   → 409: a session execution's identity is not an upload subject.
//	already has a result       → 409: the attempt is over; a grant would authorize a write whose
//	                             object no registration can ever refer to.
//	run left `delivering`      → 409: the logical delivery is settled, so no further attempt exists.
func (s *Store) revisionUploadGrant(t *transaction, r *ControlRequest) Object {
	executionID := r.Body.S("executionId")
	require(executionID != "" && len(executionID) <= 200, 400, "invalid_grant")
	if s.RevisionObjects == nil {
		panic(databaseFailure{errors.New("revision upload grant: object_store is not configured")})
	}
	e := t.one("SELECT * FROM node_executions WHERE execution_id=$1", executionID)
	require(e != nil, 404, "not_found")
	require(e.S("kind") == "deliver_revision", 409, "grant_conflict")
	require(e["result"] == nil, 409, "grant_conflict")
	o := t.one("SELECT * FROM issue_runs WHERE id=$1 AND deleted_at IS NULL", e.S("operationId"))
	require(o != nil && o.S("executorType") == "agent" && o.S("phase") == "delivering", 409, "grant_conflict")

	input := e.O("input")
	keys := []string{input.S("bundle_key"), input.S("history_key")}
	grants := make([]Object, 0, len(keys))
	ttl := s.revisionUploadTTL()
	for _, key := range keys {
		if key == "" {
			// The input Cloud itself froze for this attempt does not carry both keys: an invariant
			// failure, not something a Controller can fix by retrying differently.
			panic(databaseFailure{fmt.Errorf("revision upload grant: execution %s has an incomplete delivery input", executionID)})
		}
		grant, err := s.RevisionObjects.PresignPut(key, ttl)
		if err != nil {
			panic(databaseFailure{fmt.Errorf("revision upload grant: execution %s: %w", executionID, err)})
		}
		headers := Object{}
		for name, value := range grant.Headers {
			headers[name] = value
		}
		grants = append(grants, Object{
			"objectKey": grant.ObjectKey,
			"url":       grant.URL,
			"method":    grant.Method,
			"headers":   headers,
			"expiresAt": grant.ExpiresAt.UTC(),
		})
	}
	return Object{"grants": grants}
}
