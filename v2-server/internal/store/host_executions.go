package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

// HostExecution is a Node execution whose Agent was given host tools. EndedAt is nil while it runs;
// Reason names why it ended.
type HostExecution struct {
	ID, SessionID string
	StartedAt     time.Time
	EndedAt       *time.Time
	Reason        string
}

// RecordHostExecution records an execution before its start is sent to Node. It fails for a session
// that ended, which takes no new execution.
func (s *Store) RecordHostExecution(ctx context.Context, id, sessionID string) error {
	tag, err := s.pool.Exec(ctx, `INSERT INTO v2_host_executions (id, session_id)
		SELECT $1, id FROM v2_execution_sessions WHERE id = $2 AND ended_at IS NULL FOR SHARE`, id, sessionID)
	if err != nil {
		return fmt.Errorf("record host execution %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("record host execution %s: %w", id, execution.ErrSessionEnded)
	}
	return nil
}

// EndHostExecution settles a running execution with the reason it ended. One already settled keeps
// its reason, and an unknown one stays unknown.
func (s *Store) EndHostExecution(ctx context.Context, id, reason string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE v2_host_executions SET ended_at = now(), reason = $2 WHERE id = $1 AND ended_at IS NULL`, id, reason); err != nil {
		return fmt.Errorf("end host execution %s: %w", id, err)
	}
	return nil
}

// HostExecutions lists a session's executions with host tools, oldest first.
func (s *Store) HostExecutions(ctx context.Context, sessionID string) ([]HostExecution, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, session_id, started_at, ended_at, reason FROM v2_host_executions
		WHERE session_id = $1 ORDER BY started_at, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list host executions: %w", err)
	}
	executions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[HostExecution])
	if err != nil {
		return nil, fmt.Errorf("list host executions: %w", err)
	}
	return executions, nil
}
