package integration

import (
	"context"
	"testing"

	"github.com/wanglongan587/cloud/internal/core"
)

// TestNewUserRegistrationSucceedsAndEntersTenant registers a fresh identity and
// verifies the user is active and enrolled as an ordinary tenant member — with
// no workspace, project, or collaboration-space side effects (SD2).
func TestNewUserRegistrationSucceedsAndEntersTenant(t *testing.T) {
	f := setup(t)
	u, e := f.store.RegisterIdentity(context.Background(), f.tid, "dev", "bob@example.com", "Bob")
	must(t, e)
	if u.S("displayName") != "Bob" || u.S("status") != "active" {
		t.Fatalf("registered user mismatch: %v", u)
	}
	var role, status string
	must(t, f.store.Pool.QueryRow(
		"SELECT role,status FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2",
		f.tid, u.S("id"),
	).Scan(&role, &status))
	if role != "member" || status != "active" {
		t.Fatalf("registration must enroll a plain active member, got role=%s status=%s", role, status)
	}
}

// TestDuplicateExactEmailConflicts verifies a second registration with the exact
// same email is a stable 409 conflict, never a silent reuse of the first user.
func TestDuplicateExactEmailConflicts(t *testing.T) {
	f := setup(t)
	_, e := f.store.RegisterIdentity(context.Background(), f.tid, "dev", "bob@example.com", "Bob")
	must(t, e)
	_, e = f.store.RegisterIdentity(context.Background(), f.tid, "dev", "bob@example.com", "Bob Two")
	fault := core.ErrorCode(e)
	if fault.Status != 409 || fault.Code != "user_already_exists" {
		t.Fatalf("duplicate exact email: want 409 user_already_exists, got %d %s", fault.Status, fault.Code)
	}
}

// TestDuplicateCaseVariantEmailConflicts verifies the case-insensitive uniqueness
// rule (SD3): "Alice@Example.com" and "alice@example.com" are the same account.
func TestDuplicateCaseVariantEmailConflicts(t *testing.T) {
	f := setup(t)
	_, e := f.store.RegisterIdentity(context.Background(), f.tid, "dev", "Alice@Example.com", "Alice")
	must(t, e)
	_, e = f.store.RegisterIdentity(context.Background(), f.tid, "dev", "alice@example.com", "Alice Two")
	fault := core.ErrorCode(e)
	if fault.Status != 409 || fault.Code != "user_already_exists" {
		t.Fatalf("case-variant duplicate: want 409 user_already_exists, got %d %s", fault.Status, fault.Code)
	}
}

// TestInvalidEmailRejected verifies the lightweight email gate: empty, missing @,
// empty local/domain, multi-@ and dotless domains are all 400 invalid_email.
func TestInvalidEmailRejected(t *testing.T) {
	f := setup(t)
	for _, email := range []string{"", "   ", "not-an-email", "a@b", "a@@b.com", "@example.com", "a@example"} {
		_, e := f.store.RegisterIdentity(context.Background(), f.tid, "dev", email, "Name")
		fault := core.ErrorCode(e)
		if fault.Status != 400 || fault.Code != "invalid_email" {
			t.Fatalf("email %q: want 400 invalid_email, got %d %s", email, fault.Status, fault.Code)
		}
	}
}

// TestEmptyNameRejected verifies a blank display name is 400 name_required.
func TestEmptyNameRejected(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"", "   "} {
		_, e := f.store.RegisterIdentity(context.Background(), f.tid, "dev", "carl@example.com", name)
		fault := core.ErrorCode(e)
		if fault.Status != 400 || fault.Code != "name_required" {
			t.Fatalf("name %q: want 400 name_required, got %d %s", name, fault.Status, fault.Code)
		}
	}
}

// TestRegisteredUserResolvesViaExistingLoginPath proves the registered identity
// is the same row the login path (EnsureMember -> identity) resolves, so the new
// user can enter the existing current-user flow without a second account.
func TestRegisteredUserResolvesViaExistingLoginPath(t *testing.T) {
	f := setup(t)
	u, e := f.store.RegisterIdentity(context.Background(), f.tid, "dev", "dana@example.com", "Dana")
	must(t, e)
	again, e := f.store.EnsureMember(context.Background(), f.tid, "dev", "dana@example.com", "Dana")
	must(t, e)
	if again.S("id") != u.S("id") {
		t.Fatalf("registered identity must resolve through login: want %s got %s", u.S("id"), again.S("id"))
	}
}
