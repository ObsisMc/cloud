package controlgrpc

import (
	"math"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/wanglongan587/cloud/internal/controlpb"
)

// A delivery result's declared measurements are what Cloud later verifies the object store against
// (Cloud Revision D4 step 2), so the translation from the wire message into the stored result has to
// preserve them exactly. The one thing it can silently get wrong is the size's Go type: the proto
// declares `uint64`, while every consumer reads the value with `core.Object.N`, which recognizes only
// int64/float64/int/json.Number and answers 0 for anything else. A stored `uint64` therefore reads
// back as "the Node declared a zero-byte object" — a claim the Node never made, and one Cloud would
// then faithfully verify against the store.
func TestStoredObjectKeepsTheDeclaredSizeReadable(t *testing.T) {
	bundle := &controlpb.StoredObject{Key: "revisions/t/r/a/revision.bundle", Size: 4096, Sha256: strings.Repeat("a", 64)}
	history := &controlpb.StoredObject{Key: "revisions/t/r/a/session.jsonl", Size: 512, Sha256: strings.Repeat("b", 64)}
	result := &controlpb.ExecutionResult{
		Node: &controlpb.NodeIdentity{NodeId: "node-1", NodeIncarnationId: "inc-1"},
		Outcome: &controlpb.ExecutionResult_RevisionDelivered{RevisionDelivered: &controlpb.RevisionDelivered{
			FinalCommit: strings.Repeat("1", 40), BaseCommit: strings.Repeat("2", 40),
			RevisionRef: "refs/ora/revisions/run-1", Bundle: bundle, History: history,
		}},
	}

	out, err := resultObject(result)
	if err != nil {
		t.Fatalf("a well-formed delivered result must translate: %v", err)
	}
	for _, c := range []struct {
		field string
		want  int64
		key   string
	}{
		{"bundle", 4096, bundle.GetKey()},
		{"history", 512, history.GetKey()},
	} {
		object := out.O(c.field)
		if got := object.S("key"); got != c.key {
			t.Errorf("%s key = %q, want %q", c.field, got, c.key)
		}
		if got := object.N("size"); got != c.want {
			t.Errorf("%s size reads back as %d, want the declared %d", c.field, got, c.want)
		}
	}

	// An unchanged delivery declares no bundle at all: an absent message must stay an absent key
	// rather than become a zero-valued object.
	unchanged := &controlpb.ExecutionResult{
		Node: &controlpb.NodeIdentity{NodeId: "node-1", NodeIncarnationId: "inc-1"},
		Outcome: &controlpb.ExecutionResult_RevisionUnchanged{RevisionUnchanged: &controlpb.RevisionUnchanged{
			FinalCommit: strings.Repeat("2", 40), BaseCommit: strings.Repeat("2", 40),
			RevisionRef: "refs/ora/revisions/run-1", History: history,
		}},
	}
	out, err = resultObject(unchanged)
	if err != nil {
		t.Fatalf("a well-formed unchanged result must translate: %v", err)
	}
	if object, present := out["bundle"]; present && object != nil {
		t.Errorf("an unchanged delivery must store no bundle, got %v", object)
	}
	if got := out.O("history").N("size"); got != 512 {
		t.Errorf("history size = %d, want 512", got)
	}

	// A size the stored integer cannot hold is not a length Cloud can verify; refusing it here keeps
	// a truncated value from ever being compared against an object.
	_, err = resultObject(&controlpb.ExecutionResult{
		Node: &controlpb.NodeIdentity{NodeId: "node-1", NodeIncarnationId: "inc-1"},
		Outcome: &controlpb.ExecutionResult_RevisionDelivered{RevisionDelivered: &controlpb.RevisionDelivered{
			FinalCommit: strings.Repeat("1", 40), BaseCommit: strings.Repeat("2", 40),
			RevisionRef: "refs/ora/revisions/run-1",
			Bundle:      &controlpb.StoredObject{Key: bundle.GetKey(), Size: math.MaxUint64, Sha256: strings.Repeat("a", 64)},
			History:     history,
		}},
	})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("an unrepresentable size must be refused as invalid input, got %s (%v)", got, err)
	}
}

// A Node may not report Cloud's own verification verdict (Cloud Revision D4 step 4). The proto has to
// name `VERIFICATION_FAILED` because Cloud stores and renders it, so the enum alone cannot enforce the
// rule: the translation from the wire is where it is enforced, and a Node that sends it must be
// refused rather than recorded as having verified anything.
func TestRevisionFailureRefusesCloudsOwnVerificationVerdict(t *testing.T) {
	failed := func(r controlpb.RevisionFailureReason) *controlpb.ExecutionResult {
		return &controlpb.ExecutionResult{
			Node:    &controlpb.NodeIdentity{NodeId: "node-1", NodeIncarnationId: "inc-1"},
			Outcome: &controlpb.ExecutionResult_RevisionFailed{RevisionFailed: &controlpb.RevisionFailed{Reason: r}},
		}
	}

	// The closed set a Node may report, each stored under its own snake_case name.
	for _, reason := range []controlpb.RevisionFailureReason{
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_SESSION_NOT_SETTLED,
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_CHECKOUT_UNAVAILABLE,
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_SNAPSHOT_FAILED,
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_BUNDLE_FAILED,
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_HISTORY_UNAVAILABLE,
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UPLOAD_FAILED,
	} {
		out, err := resultObject(failed(reason))
		if err != nil {
			t.Fatalf("a reason a Node may report must translate: %v", err)
		}
		if got := out.S("reason"); got != revisionFailureReasonStored(reason) {
			t.Errorf("%s stored as %q, want %q", reason, got, revisionFailureReasonStored(reason))
		}
	}

	// Cloud's own verdict, and the enum's unspecified value, are both outside it.
	for _, reason := range []controlpb.RevisionFailureReason{
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_VERIFICATION_FAILED,
		controlpb.RevisionFailureReason_REVISION_FAILURE_REASON_UNSPECIFIED,
	} {
		_, err := resultObject(failed(reason))
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Errorf("%s must be refused at the wire boundary, got %s (%v)", reason, got, err)
		}
	}
}

// revisionFailureReasonStored is the snake_case spelling the failing case above expects, derived from
// the enum rather than restated, so the assertion and the mapping cannot drift apart.
func revisionFailureReasonStored(r controlpb.RevisionFailureReason) string {
	return strings.ToLower(strings.TrimPrefix(r.String(), "REVISION_FAILURE_REASON_"))
}
