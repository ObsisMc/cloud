package core

import (
	"strings"
	"testing"
	"time"
)

// Offline unit tests for the Revision half of the delivery path (Cloud Revision D1–D4). They cover
// only the decisions that are pure functions of their arguments: which terminal results the wire
// contract accepts, which ones step 1's input comparison demands, which objects a result declares,
// and how an identity is reduced and compared. The transaction-shaped obligations — registration,
// rollback, idempotency, the object-store verdict's effect on the run — need real PostgreSQL rows and
// live in integration/agent_run_delivery_test.go.

const (
	testRef       = "refs/ora/revisions/11111111-1111-1111-1111-111111111111"
	testBase      = "0123456789abcdef0123456789abcdef01234567"
	testFinal     = "89abcdef0123456789abcdef0123456789abcdef"
	testBundleKey = "revisions/t/r/a/revision.bundle"
	testHistKey   = "revisions/t/r/a/session.jsonl"
)

var (
	testBundleSHA = strings.Repeat("a", 64)
	testHistSHA   = strings.Repeat("b", 64)
)

// deliveryInput is the frozen DeliverRevisionSpec one attempt carries, shaped exactly as Cloud writes
// it (agent_run_session_settle.go): the two object keys, the run's Revision ref and the baseline.
func deliveryInput() Object {
	return Object{
		"revision_ref": testRef,
		"base_commit":  testBase,
		"bundle_key":   testBundleKey,
		"history_key":  testHistKey,
	}
}

// deliveredResult is a well-formed `revision_delivered` result against deliveryInput.
func deliveredResult() Object {
	return Object{
		"outcome":     revisionDeliveredOutcome,
		"revisionRef": testRef,
		"baseCommit":  testBase,
		"finalCommit": testFinal,
		"bundle":      Object{"key": testBundleKey, "size": 4096, "sha256": testBundleSHA},
		"history":     Object{"key": testHistKey, "size": 512, "sha256": testHistSHA},
	}
}

// unchangedResult is a well-formed `revision_unchanged` result: nothing changed, so there is no
// bundle and the final commit is the baseline.
func unchangedResult() Object {
	return Object{
		"outcome":     revisionUnchangedOutcome,
		"revisionRef": testRef,
		"baseCommit":  testBase,
		"finalCommit": testBase,
		"history":     Object{"key": testHistKey, "size": 512, "sha256": testHistSHA},
	}
}

// TestDeliveryResultAcceptsOnlyTheWireClosedSet: the reason a Node reports is a closed set, and
// Cloud's own `verification_failed` is deliberately outside it — accepting it from the wire would let
// a Node assert a verdict about objects Cloud has not looked at (D4 step 4).
func TestDeliveryResultAcceptsOnlyTheWireClosedSet(t *testing.T) {
	for _, reason := range []string{
		"session_not_settled", "checkout_unavailable", "snapshot_failed",
		"bundle_failed", "history_unavailable", "upload_failed",
	} {
		outcome, ok := deliveryResult(Object{"outcome": revisionFailedOutcome, "reason": reason})
		if !ok || outcome.Kind != DeliveryFailed || outcome.Reason != reason {
			t.Errorf("revision_failed{%s}: got (%v, %v), want a failed settlement carrying the reason", reason, outcome, ok)
		}
	}
	cases := map[string]Object{
		"delivered":                 {"outcome": revisionDeliveredOutcome},
		"unchanged":                 {"outcome": revisionUnchangedOutcome},
		"verification_failed":       {"outcome": revisionFailedOutcome, "reason": "verification_failed"},
		"unspecified reason":        {"outcome": revisionFailedOutcome, "reason": "unspecified"},
		"reason absent":             {"outcome": revisionFailedOutcome},
		"unknown outcome":           {"outcome": "revision_something_else"},
		"outcome absent":            {},
		"outcome is not a revision": {"outcome": "session_ended"},
	}
	for name, result := range cases {
		outcome, ok := deliveryResult(result)
		if name == "delivered" {
			if !ok || outcome.Kind != DeliverySaved {
				t.Errorf("%s: got (%v, %v), want saved", name, outcome, ok)
			}
			continue
		}
		if name == "unchanged" {
			if !ok || outcome.Kind != DeliveryUnchanged {
				t.Errorf("%s: got (%v, %v), want unchanged", name, outcome, ok)
			}
			continue
		}
		if ok {
			t.Errorf("%s: accepted as %v; want a refusal", name, outcome)
		}
	}
}

// TestRevisionResultMatchesInputIsTotal is D4 step 1: a local, I/O-free comparison that must hold
// before Cloud spends a request on any object. Every key it reads may be absent, and no shape a
// malformed or contradictory result can take is allowed through — including the two the shape check
// alone catches: an "unchanged" claim that carries a bundle, and one whose final commit moved.
func TestRevisionResultMatchesInputIsTotal(t *testing.T) {
	matches := map[string]bool{
		"delivered":                      true,
		"delivered with uppercase sha":   true,
		"unchanged":                      true,
		"different revision ref":         false,
		"different base commit":          false,
		"final commit is not a commit":   false,
		"final commit absent":            false,
		"bundle key is the history key":  false,
		"bundle key absent":              false,
		"bundle size is negative":        false,
		"bundle digest is not a digest":  false,
		"bundle digest absent":           false,
		"history key is the bundle key":  false,
		"history digest is not a digest": false,
		"unchanged declares a bundle":    false,
		"unchanged moved the commit":     false,
		"unknown outcome":                false,
		"result is empty":                false,
	}
	for name, want := range matches {
		t.Run(name, func(t *testing.T) {
			var result Object
			switch name {
			case "delivered":
				result = deliveredResult()
			case "delivered with uppercase sha":
				result = deliveredResult()
				result["bundle"] = Object{"key": testBundleKey, "size": 4096, "sha256": strings.ToUpper(testBundleSHA)}
			case "unchanged":
				result = unchangedResult()
			case "different revision ref":
				result = deliveredResult()
				result["revisionRef"] = "refs/ora/revisions/someone-else"
			case "different base commit":
				result = deliveredResult()
				result["baseCommit"] = strings.Repeat("f", 40)
			case "final commit is not a commit":
				result = deliveredResult()
				result["finalCommit"] = "not-a-commit"
			case "final commit absent":
				result = deliveredResult()
				delete(result, "finalCommit")
			case "bundle key is the history key":
				result = deliveredResult()
				result["bundle"] = Object{"key": testHistKey, "size": 4096, "sha256": testBundleSHA}
			case "bundle key absent":
				result = deliveredResult()
				result["bundle"] = Object{"size": 4096, "sha256": testBundleSHA}
			case "bundle size is negative":
				result = deliveredResult()
				result["bundle"] = Object{"key": testBundleKey, "size": -1, "sha256": testBundleSHA}
			case "bundle digest is not a digest":
				result = deliveredResult()
				result["bundle"] = Object{"key": testBundleKey, "size": 4096, "sha256": "abc"}
			case "bundle digest absent":
				result = deliveredResult()
				result["bundle"] = Object{"key": testBundleKey, "size": 4096}
			case "history key is the bundle key":
				result = deliveredResult()
				result["history"] = Object{"key": testBundleKey, "size": 512, "sha256": testHistSHA}
			case "history digest is not a digest":
				result = deliveredResult()
				result["history"] = Object{"key": testHistKey, "size": 512, "sha256": strings.Repeat("z", 64)}
			case "unchanged declares a bundle":
				result = unchangedResult()
				result["bundle"] = Object{"key": testBundleKey, "size": 4096, "sha256": testBundleSHA}
			case "unchanged moved the commit":
				result = unchangedResult()
				result["finalCommit"] = testFinal
			case "unknown outcome":
				result = deliveredResult()
				result["outcome"] = "revision_something_else"
			case "result is empty":
				result = Object{}
			}
			if got := revisionResultMatchesInput(result, deliveryInput()); got != want {
				t.Errorf("revisionResultMatchesInput(%s) = %v, want %v", name, got, want)
			}
		})
	}

	// The input side is Cloud's own frozen state: an input Cloud cannot compare against is corrupted
	// state and never a match, whatever the result says.
	for name, input := range map[string]Object{
		"input is empty":              {},
		"ref absent":                  {"base_commit": testBase, "bundle_key": testBundleKey, "history_key": testHistKey},
		"base commit is not a commit": {"revision_ref": testRef, "base_commit": "nope", "bundle_key": testBundleKey, "history_key": testHistKey},
		"bundle key absent":           {"revision_ref": testRef, "base_commit": testBase, "history_key": testHistKey},
		"history key absent":          {"revision_ref": testRef, "base_commit": testBase, "bundle_key": testBundleKey},
	} {
		if revisionResultMatchesInput(deliveredResult(), input) {
			t.Errorf("a result must not match an input Cloud cannot compare against: %s", name)
		}
	}
}

// TestDeclaredRevisionObjectsSkipsAbsentMeasurements: an absent measurement is skipped rather than
// turned into a zero-valued one, so a malformed result can never make Cloud probe an empty key.
func TestDeclaredRevisionObjectsSkipsAbsentMeasurements(t *testing.T) {
	cases := []struct {
		name   string
		result Object
		want   []string
	}{
		{"delivered declares both", deliveredResult(), []string{testBundleKey, testHistKey}},
		{"unchanged declares only the history", unchangedResult(), []string{testHistKey}},
		{"a result with neither declares neither", Object{"outcome": revisionDeliveredOutcome}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := declaredRevisionObjects(c.result)
			var keys []string
			for _, object := range objects {
				keys = append(keys, object.S("key"))
			}
			if strings.Join(keys, ",") != strings.Join(c.want, ",") {
				t.Errorf("declared objects = %v, want %v (the bundle precedes the history)", keys, c.want)
			}
		})
	}
}

// TestRevisionDigestNormalizesRatherThanComparesText: D4 compares digests, so the same digest in
// either hex case is the same digest — while anything that is not a 64-digit hex digest, the empty
// string above all, is refused before it can be compared against a stored one.
func TestRevisionDigestNormalizesRatherThanComparesText(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 64), strings.Repeat("A", 64)} {
		got, ok := revisionDigest(value)
		if !ok || got != strings.Repeat("a", 64) {
			t.Errorf("revisionDigest(%q) = (%q, %v), want the lowercase digest", value, got, ok)
		}
	}
	for _, value := range []string{
		"", "abc", strings.Repeat("a", 63), strings.Repeat("a", 65),
		strings.Repeat("z", 64), strings.Repeat(" ", 64), strings.Repeat("a", 63) + "\n",
	} {
		if got, ok := revisionDigest(value); ok {
			t.Errorf("revisionDigest(%q) accepted as %q; want a refusal", value, got)
		}
	}
}

// TestRevisionIdentityDiffNamesTheFirstDifference: the takeover rolls back on any difference, but the
// log has to say which field disagreed — a Revision that already exists for a run is a conflict
// between two deliveries of the same logical Revision, and the field is what makes it actionable.
func TestRevisionIdentityDiffNamesTheFirstDifference(t *testing.T) {
	base := revisionIdentity{
		tenantID: "t", runID: "r", workspaceID: "w", projectID: "p", repositoryURL: "https://example.invalid/repo",
		baseCommit: testBase, finalCommit: testFinal, revisionRef: testRef,
		bundleKey: testBundleKey, bundleSize: 4096, bundleSHA256: testBundleSHA,
		historyKey: testHistKey, historySize: 512, historySHA256: testHistSHA,
	}
	if field := base.diff(&base); field != "" {
		t.Errorf("an identity differs from itself on %q", field)
	}
	mutations := map[string]func(*revisionIdentity){
		"tenant_id":      func(r *revisionIdentity) { r.tenantID = "other" },
		"run_id":         func(r *revisionIdentity) { r.runID = "other" },
		"workspace_id":   func(r *revisionIdentity) { r.workspaceID = "other" },
		"project_id":     func(r *revisionIdentity) { r.projectID = "other" },
		"repository_url": func(r *revisionIdentity) { r.repositoryURL = "other" },
		"base_commit":    func(r *revisionIdentity) { r.baseCommit = strings.Repeat("f", 40) },
		"final_commit":   func(r *revisionIdentity) { r.finalCommit = strings.Repeat("f", 40) },
		"revision_ref":   func(r *revisionIdentity) { r.revisionRef = "other" },
		"bundle_key":     func(r *revisionIdentity) { r.bundleKey = "other" },
		"bundle_size":    func(r *revisionIdentity) { r.bundleSize = 0 },
		"bundle_sha256":  func(r *revisionIdentity) { r.bundleSHA256 = strings.Repeat("c", 64) },
		"history_key":    func(r *revisionIdentity) { r.historyKey = "other" },
		"history_size":   func(r *revisionIdentity) { r.historySize = 0 },
		"history_sha256": func(r *revisionIdentity) { r.historySHA256 = strings.Repeat("c", 64) },
	}
	for want, mutate := range mutations {
		other := base
		mutate(&other)
		if got := base.diff(&other); got != want {
			t.Errorf("a difference in %s was reported as %q", want, got)
		}
	}
}

// TestStoredIdentityReadsAnAbsentBundleAsNoBundle: the schema keeps the bundle columns all-or-nothing
// and NULL when the checkout did not change, so a NULL bundle must reduce to the empty identity
// rather than to a zero-sized upload — otherwise an unchanged delivery could never equal itself.
func TestStoredIdentityReadsAnAbsentBundleAsNoBundle(t *testing.T) {
	run := Object{"id": "run-1", "tenantId": "tenant-1", "workspaceId": "ws-1"}
	placement := Object{"projectId": "project-1", "repositoryUrl": "https://example.invalid/repo"}

	identity, ok := declaredIdentity(unchangedResult(), run, placement)
	if !ok {
		t.Fatal("an unchanged result must name a Revision: the session history is always saved")
	}
	if identity.bundleKey != "" || identity.bundleSize != 0 || identity.bundleSHA256 != "" {
		t.Fatalf("an unchanged delivery must declare no bundle, got %+v", identity)
	}
	if identity.projectID != "project-1" || identity.repositoryURL != "https://example.invalid/repo" {
		t.Fatalf("the identity must carry the run Workspace's placement, got %+v", identity)
	}
	// The round trip through a stored row is what the takeover compares: a row written from this
	// identity must reduce back to it, NULL bundle columns included.
	stored := storedIdentity(Object{
		"tenantId": "tenant-1", "runId": "run-1", "workspaceId": "ws-1",
		"projectId": "project-1", "repositoryUrl": "https://example.invalid/repo",
		"baseCommit": testBase, "finalCommit": testBase, "revisionRef": testRef,
		"historyKey": testHistKey, "historySize": 512, "historySha256": testHistSHA,
	})
	if field := identity.diff(&stored); field != "" {
		t.Fatalf("a stored unchanged Revision must equal the result that declared it, differed on %q", field)
	}

	// A result whose history digest is unusable cannot name a Revision at all; the caller reaches this
	// only after the shape check, so it is an internal contradiction rather than a refusal.
	if _, ok := declaredIdentity(Object{"history": Object{"key": testHistKey, "sha256": "nope"}}, run, placement); ok {
		t.Error("a result without a usable history digest must not name a Revision")
	}
}

// TestNullableBundleColumnsMoveTogether: the three bundle columns are written from one identity, so
// the SQL NULL that means "the checkout did not change" can never drift into a size without an object.
func TestNullableBundleColumnsMoveTogether(t *testing.T) {
	if nullableText("") != nil || nullableSize("", 4096) != nil {
		t.Error("an absent bundle key must write NULL for both its key and its size")
	}
	if nullableText(testBundleKey) != testBundleKey || nullableSize(testBundleKey, 0) != int64(0) {
		t.Error("a present bundle key must write its own value and its own size, including a zero one")
	}
}

// TestRevisionUploadTTLFallsBackToTheApprovedDefault: a Store built without the deployment wiring has
// no configured lifetime, and a zero value must mean D1's default rather than a grant that expires as
// it is issued.
func TestRevisionUploadTTLFallsBackToTheApprovedDefault(t *testing.T) {
	if got := (&Store{}).revisionUploadTTL(); got != DefaultRevisionUploadTTL {
		t.Errorf("an unwired store must use D1's default, got %s", got)
	}
	if got := (&Store{RevisionUploadTTL: time.Minute}).revisionUploadTTL(); got != time.Minute {
		t.Errorf("a configured lifetime must be used verbatim, got %s", got)
	}
	if got := (&Store{RevisionUploadTTL: -time.Minute}).revisionUploadTTL(); got != DefaultRevisionUploadTTL {
		t.Errorf("a non-positive lifetime must select the default, got %s", got)
	}
}
