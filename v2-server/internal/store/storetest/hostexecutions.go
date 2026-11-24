package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// HostExecution is a row of v2_host_executions. EndedAt is nil while the execution runs.
type HostExecution struct {
	ID, SessionID string
	StartedAt     time.Time
	EndedAt       *time.Time
	Reason        string
}

// HostExecutions reads a session's host executions, oldest first. db is a connection on the test's
// schema.
func HostExecutions(t *testing.T, db *pgx.Conn, sessionID string) []HostExecution {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT id, session_id, started_at, ended_at, reason FROM v2_host_executions
		WHERE session_id = $1 ORDER BY started_at, id`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	executions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[HostExecution])
	if err != nil {
		t.Fatal(err)
	}
	return executions
}
