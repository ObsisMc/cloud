package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/projectrepos"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// A start that cannot settle the executions an earlier process left running is refused as a whole:
// nothing is recovered and nothing is served. The completion tool is served only after that step
// (TestTheCompletionToolIsServedAfterEarlierNodesStopped). A start that can serves with none running.
func TestAStartThatCannotSettleHostExecutionsIsRefused(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("boot_host_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	dsn = withSearchPath(t, dsn, schema+",public")
	preset, err := store.LoadPreset("users.json")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SeedUsers(ctx, preset); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	projects, err := projectrepos.New(db, projectrepos.Options{Root: filepath.Join(root, "repositories")})
	if err != nil {
		t.Fatal(err)
	}
	scope, project, err := prepareDemo(ctx, db, projects, preset.Users[0].Email)
	if err != nil {
		t.Fatal(err)
	}
	session := execution.Session{ID: "hosted-session", WorkspaceID: "hosted-workspace", Target: &execution.Target{ProjectID: project.ID}}
	turn := execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: "hosted-turn"}, Prompt: execution.TextPrompt("起草"),
		Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: "run", NodeID: "a", AttemptID: "attempt"}}
	if err := db.CreateExecutionSession(ctx, scope, session, turn); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishExecutionTurn(ctx, scope, turn.Ref, execution.Result{Status: execution.StatusCompleted, Answer: "初稿", AnswerComplete: true, StopReason: "end_turn"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordHostExecution(ctx, "hosted-execution", session.ID); err != nil {
		t.Fatal(err)
	}
	read := func() store.HostExecution {
		t.Helper()
		executions, err := db.HostExecutions(ctx, session.ID)
		if err != nil || len(executions) != 1 {
			t.Fatalf("executions = %+v %v", executions, err)
		}
		return executions[0]
	}
	t.Setenv("V2_DATA_DIR", root)
	t.Setenv("V2_LOGIN", "email")
	t.Setenv("V2_USERS_FILE", "users.json")
	t.Setenv("V2_NODE_ASSETS_DIR", "")
	t.Setenv("V2_FRONTEND_DIR", "")
	t.Setenv("V2_PLUGIN_MARKETPLACE_URL", "")

	if _, err := admin.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE FUNCTION refuse_settlement() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'settlement refused'; END $$;
		CREATE TRIGGER refuse_settlement BEFORE UPDATE ON v2_host_executions FOR EACH ROW EXECUTE FUNCTION refuse_settlement()`); err != nil {
		t.Fatal(err)
	}
	err = runWithListener("127.0.0.1:0", dsn, false, false, true, func(*http.Server) error {
		t.Error("a start that settled nothing served")
		return errors.New("served")
	})
	if err == nil || !strings.Contains(err.Error(), "settlement refused") {
		t.Fatalf("start = %v", err)
	}
	if x := read(); x.EndedAt != nil {
		t.Fatalf("execution after the refused start = %+v", x)
	}
	if s, err := db.ReadExecutionSession(ctx, scope, session.ID); err != nil || s.EndedAt != nil {
		t.Fatalf("session after the refused start = %+v %v, want it as it was", s, err)
	}

	if _, err := admin.Exec(ctx, "DROP TRIGGER refuse_settlement ON v2_host_executions"); err != nil {
		t.Fatal(err)
	}
	checked := errors.New("first listener request checked")
	err = runWithListener("127.0.0.1:0", dsn, false, false, true, func(*http.Server) error {
		if x := read(); x.EndedAt == nil || x.Reason != execution.FailureServerRestarted {
			return fmt.Errorf("execution when the server serves = %+v", x)
		}
		return checked
	})
	if !errors.Is(err, checked) {
		t.Fatalf("start: %v", err)
	}
}
