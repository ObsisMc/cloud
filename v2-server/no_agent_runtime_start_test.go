package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/projectrepos"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// Over real starts without an Agent runtime (no V2_NODE_ASSETS_DIR): a parked interactive node's
// session is ended without an Agent to stop. A node that still waits validly fails as interrupted
// by the restart, resumable, rather than as a missing Agent, since such a start offers no executor
// at all; one that no longer waits is swept as before. Each start serves, and so does the next.
func TestAStartWithoutAnAgentRuntimeEndsParkedSessions(t *testing.T) {
	for name, starter := range map[string]bool{"valid wait": true, "swept wait": false} {
		t.Run(name, func(t *testing.T) { startWithoutAgentRuntime(t, starter) })
	}
}

func startWithoutAgentRuntime(t *testing.T, starter bool) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	admin, err := pgx.Connect(ctx, dsn)
	must(err)
	defer admin.Close(ctx)
	schema := fmt.Sprintf("boot_no_agent_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	must(err)
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	dsn = withSearchPath(t, dsn, schema+",public")
	preset, err := store.LoadPreset("users.json")
	must(err)
	db, err := store.Open(ctx, dsn)
	must(err)
	defer db.Close()
	must(db.SeedUsers(ctx, preset))
	root := t.TempDir()
	projects, err := projectrepos.New(db, projectrepos.Options{Root: filepath.Join(root, "repositories")})
	must(err)
	scope, project, err := prepareDemo(ctx, db, projects, preset.Users[0].Email)
	must(err)

	// The held session of a parked interactive node, whose Agent an earlier process started. A run
	// without a starter no longer waits: the sweep fails it.
	w, _, err := db.CreateWorkflow(ctx, scope.TenantID, "启动", `{"nodes":[
 {"id":"start","data":{"kind":"start"}},
 {"id":"a","data":{"kind":"agent","agentConfig":{"executor":{"agentCli":"official/ora-space.opencode","modelId":"fixture"},"prompt":"起草","interactive":true}}}
],"edges":[{"source":"start","target":"a"}]}`)
	must(err)
	published, err := db.PublishWorkflow(ctx, scope.TenantID, w.ID, "v1", nil)
	must(err)
	run, err := db.CreateWorkflowRun(ctx, scope.TenantID, w.ID, published.ID, store.WorkflowRunInput{ProjectID: project.ID})
	must(err)
	if starter {
		must(db.SetWorkflowStarter(ctx, scope.TenantID, run.ID, scope.UserID))
	}
	_, err = db.StartWorkflowRun(ctx, scope.TenantID, run.ID)
	must(err)
	row, err := db.CreateWorkflowNodeRun(ctx, scope.TenantID, run.ID, store.WorkflowNodeRunInput{NodeID: "a", NodeType: "agent"})
	must(err)
	session := execution.Session{ID: "parked-session", WorkspaceID: "parked-workspace", Target: &execution.Target{ProjectID: project.ID}}
	turn := execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: "parked-turn"}, Prompt: execution.TextPrompt("起草"),
		Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: run.ID, NodeID: "a", AttemptID: row.ID}}
	must(db.CreateExecutionSession(ctx, scope, session, turn))
	_, err = db.BindWorkflowNodeSession(ctx, scope.TenantID, run.ID, row.ID, session.ID)
	must(err)
	_, err = db.FinishExecutionTurn(ctx, scope, turn.Ref, execution.Result{Status: execution.StatusCompleted, Answer: "初稿", AnswerComplete: true, StopReason: "end_turn"})
	must(err)
	moved, err := db.MoveWorkflowEngineNode(ctx, run.ID, row.ID, store.WorkflowStatusRunning, store.WorkflowStatusPending,
		func([]byte) ([]byte, error) { return []byte(`{"interactive":{"turns":1}}`), nil })
	if err != nil || !moved {
		t.Fatalf("park = %v %v", moved, err)
	}

	for name, value := range map[string]string{"V2_DATA_DIR": root, "V2_LOGIN": "email", "V2_USERS_FILE": "users.json",
		"V2_NODE_ASSETS_DIR": "", "V2_FRONTEND_DIR": "", "V2_PLUGIN_MARKETPLACE_URL": ""} {
		t.Setenv(name, value)
	}
	// What a start serves with, read once it listens: it has swept and recovered by then.
	type outcome struct {
		node    store.WorkflowNodeRun
		run     store.WorkflowRun
		session execution.Session
	}
	read := func() (o outcome, err error) {
		rows, err := db.WorkflowEngineNodeRuns(ctx, run.ID)
		if err != nil {
			return o, err
		}
		if len(rows) != 1 || rows[0].ID != row.ID {
			return o, fmt.Errorf("node runs = %+v", rows)
		}
		o.node = rows[0]
		if o.run, err = db.WorkflowRun(ctx, scope.TenantID, run.ID); err != nil {
			return o, err
		}
		o.session, err = db.ReadExecutionSession(ctx, scope, session.ID)
		return o, err
	}
	served := errors.New("served")
	start := func() outcome {
		t.Helper()
		var o outcome
		var readErr error
		err := runWithListener("127.0.0.1:0", dsn, false, false, true, func(*http.Server) error {
			o, readErr = read()
			return served
		})
		if !errors.Is(err, served) {
			t.Fatalf("start: %v", err)
		}
		must(readErr)
		return o
	}

	first := start()
	if first.node.Status != store.WorkflowStatusFailed || first.run.Status != store.WorkflowStatusFailed {
		t.Fatalf("after the start: node %s, run %s, want both failed", first.node.Status, first.run.Status)
	}
	var payload struct {
		Detail struct {
			Kind      string `json:"kind"`
			Resumable bool   `json:"resumable"`
		} `json:"error_detail"`
	}
	must(json.Unmarshal(first.node.Payload, &payload))
	if payload.Detail.Kind != "interrupted_by_restart" || !payload.Detail.Resumable {
		t.Errorf("the node failed as %+v in %s, want interrupted_by_restart and resumable", payload.Detail, first.node.Payload)
	}
	if first.session.EndedAt == nil {
		t.Error("the parked session is still open after the start")
	}

	// The next start finds nothing left to end and serves too, keeping the failure.
	second := start()
	if second.node.Status != store.WorkflowStatusFailed || string(second.node.Payload) != string(first.node.Payload) {
		t.Errorf("after the next start: node %s %s, want it kept as %s", second.node.Status, second.node.Payload, first.node.Payload)
	}
}
