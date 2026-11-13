package store_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// Revoking tenant access stops new execution (admission re-checks TenantAccess inside its
// transaction) but never strands admitted work: runtime writes still settle it. Reads do not
// re-check access; the HTTP tenant route authorizes them.
func TestExecutionRuntimeWritesSurviveAccessRevocation(t *testing.T) {
	for _, tc := range []struct {
		name, disable string
	}{
		{"membership", `DELETE FROM memberships WHERE user_id=$1`},
		{"user", `UPDATE users SET status='inactive' WHERE id=$1`},
		{"tenant", `UPDATE tenants SET status='paused' WHERE id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := executionDatabase(t)
			ctx := t.Context()
			turn := executionTurn("accepted", "first")
			createExecution(t, f, "accepted", turn)
			other := executionMember(t, f.s, "other")
			var tenant string
			if err := f.admin.QueryRow(ctx, `INSERT INTO tenants(name) VALUES('other tenant') RETURNING id`).Scan(&tenant); err != nil {
				t.Fatal(err)
			}
			id := f.scope.UserID
			if tc.name == "tenant" {
				id = f.scope.TenantID
			}
			if _, err := f.admin.Exec(ctx, tc.disable, id); err != nil {
				t.Fatal(err)
			}

			// Runtime writes retain the admitted owner/tenant boundary even when access is gone.
			result := execution.Result{Status: execution.StatusCompleted, Answer: "finished after revocation", AnswerComplete: true, StopReason: "stop"}
			for _, wrong := range []execution.Scope{other, {TenantID: tenant, UserID: f.scope.UserID}} {
				if _, err := f.s.AppendExecutionEvent(ctx, wrong, execution.Event{Ref: turn.Ref, Kind: execution.EventTextDelta, TextDelta: "forged"}); !errors.Is(err, execution.ErrNotFound) {
					t.Fatalf("wrong scope appended event: %v", err)
				}
				if _, err := f.s.FinishExecutionTurn(ctx, wrong, turn.Ref, result); !errors.Is(err, execution.ErrNotFound) {
					t.Fatalf("wrong scope finished turn: %v", err)
				}
				if err := f.s.EndExecutionSession(ctx, wrong, turn.Ref.SessionID); !errors.Is(err, execution.ErrNotFound) {
					t.Fatalf("wrong scope ended session: %v", err)
				}
			}
			if _, err := f.s.TenantAccess(ctx, f.scope.TenantID, f.scope.UserID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("revoked access still granted: %v", err)
			}
			admissions := map[string]func() error{
				"create": func() error {
					return f.s.CreateExecutionSession(ctx, f.scope, execution.Session{ID: "new"}, executionTurn("new", "new"))
				},
				"append": func() error {
					return f.s.AppendExecutionTurn(ctx, f.scope, "accepted", executionTurn("accepted", "new"))
				},
			}
			for name, admit := range admissions {
				if err := admit(); !errors.Is(err, execution.ErrNotFound) {
					t.Fatalf("revoked access admitted %s: %v", name, err)
				}
			}
			event, err := f.s.AppendExecutionEvent(ctx, f.scope, execution.Event{Ref: turn.Ref, Kind: execution.EventTextDelta, TextDelta: result.Answer})
			if err != nil || event.Sequence != 1 {
				t.Fatalf("runtime event=%+v err=%v", event, err)
			}
			if actual, err := f.s.FinishExecutionTurn(ctx, f.scope, turn.Ref, result); err != nil || !reflect.DeepEqual(actual, result) {
				t.Fatalf("runtime result=%+v err=%v", actual, err)
			}
			for range 2 {
				if err := f.s.EndExecutionSession(ctx, f.scope, "accepted"); err != nil {
					t.Fatalf("runtime end=%v", err)
				}
			}

			// The retained terminal sequence is complete, once.
			events, err := f.s.ReadExecutionEvents(ctx, f.scope, "accepted", 0, 10)
			if err != nil || len(events) != 3 {
				t.Fatalf("retained events=%+v err=%v", events, err)
			}
			if events[0].TextDelta != result.Answer || !reflect.DeepEqual(events[1].Result, &result) || events[1].Kind != execution.EventTurnEnded || events[2].Kind != execution.EventSessionEnded {
				t.Fatalf("lost final state: %+v", events)
			}
			session, err := f.s.ReadExecutionSession(ctx, f.scope, "accepted")
			if err != nil || session.EndedAt == nil {
				t.Fatalf("session=%+v err=%v", session, err)
			}
			if _, err := f.s.ReadExecutionSession(ctx, f.scope, "new"); !errors.Is(err, execution.ErrNotFound) {
				t.Fatalf("rejected admission created a row: %v", err)
			}
		})
	}
}

func TestExecutionScopeUUIDValidation(t *testing.T) {
	f := executionDatabase(t)
	ctx := t.Context()
	turn := executionTurn("uuid", "uuid-turn")
	createExecution(t, f, "uuid", turn)
	for i, invalid := range []string{"", "not-a-uuid", "00000000000000000000000000000000000000", "zzzzzzzz-0000-0000-0000-000000000000", "--000000-0000-0000-0000-000000000000"} {
		for _, scope := range []execution.Scope{{TenantID: invalid, UserID: f.scope.UserID}, {TenantID: f.scope.TenantID, UserID: invalid}} {
			if _, err := f.s.TenantAccess(ctx, scope.TenantID, scope.UserID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("invalid scope %d access=%v", i, err)
			}
			if err := f.s.CreateExecutionSession(ctx, scope, execution.Session{ID: "invalid"}, executionTurn("invalid", "invalid")); !errors.Is(err, execution.ErrNotFound) {
				t.Fatalf("invalid scope %d admission=%v", i, err)
			}
			if err := f.s.EndExecutionSession(ctx, scope, "uuid"); !errors.Is(err, execution.ErrInvalidRequest) {
				t.Fatalf("invalid scope %d runtime=%v", i, err)
			}
		}
	}
	upper := execution.Scope{TenantID: strings.ToUpper(f.scope.TenantID), UserID: strings.ToUpper(f.scope.UserID)}
	if got, err := f.s.ReadExecutionAttempt(ctx, upper, turn.Caller); err != nil || got.Ref != turn.Ref {
		t.Fatalf("uppercase scope=%+v err=%v", got, err)
	}
	if _, err := f.s.AppendExecutionEvent(ctx, upper, execution.Event{Ref: turn.Ref, Kind: execution.EventTextDelta, TextDelta: "ok"}); err != nil {
		t.Fatalf("uppercase runtime scope=%v", err)
	}
}

func TestExecutionAdmissionChecksProjectTenant(t *testing.T) {
	f := executionDatabase(t)
	ctx := t.Context()
	project, err := f.s.CreateProject(ctx, f.scope, store.Project{ID: "project", Name: "project", RepositoryURL: "https://example.com/demo.git", Commit: "fixed", MirrorPath: "/private/demo.git"})
	if err != nil {
		t.Fatal(err)
	}
	other := executionMember(t, f.s, "other")
	if err := f.admin.QueryRow(ctx, `INSERT INTO tenants(name) VALUES('other') RETURNING id`).Scan(&other.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, `INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,'member')`, other.TenantID, other.UserID); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		scope     execution.Scope
		projectID string
		want      error
	}{
		{f.scope, project.ID, nil},
		{other, project.ID, execution.ErrNotFound},
		{f.scope, "missing", execution.ErrNotFound},
		{f.scope, "", execution.ErrInvalidRequest},
	} {
		sid := fmt.Sprintf("project-session-%d", i)
		turn := executionTurn(sid, sid)
		err := f.s.CreateExecutionSession(ctx, tc.scope, execution.Session{ID: sid, Target: &execution.Target{ProjectID: tc.projectID}}, turn)
		if !errors.Is(err, tc.want) {
			t.Fatalf("case %d admission=%v want=%v", i, err, tc.want)
		}
		if tc.want != nil {
			if _, err := f.s.ReadExecutionSession(ctx, tc.scope, sid); !errors.Is(err, execution.ErrNotFound) {
				t.Fatalf("case %d rejected project left session: %v", i, err)
			}
		}
	}
}
