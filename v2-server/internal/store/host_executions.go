package store

import (
	"context"
	"fmt"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

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

// HostExecutionRunning reports whether the execution is on record and not settled.
func (s *Store) HostExecutionRunning(ctx context.Context, id string) (bool, error) {
	var running bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM v2_host_executions WHERE id = $1 AND ended_at IS NULL)`, id).Scan(&running); err != nil {
		return false, fmt.Errorf("read host execution %s: %w", id, err)
	}
	return running, nil
}

// EndHostExecution settles a running execution with the reason it ended. One already settled keeps
// its reason, and an unknown one stays unknown.
func (s *Store) EndHostExecution(ctx context.Context, id, reason string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE v2_host_executions SET ended_at = now(), reason = $2 WHERE id = $1 AND ended_at IS NULL`, id, reason); err != nil {
		return fmt.Errorf("end host execution %s: %w", id, err)
	}
	return nil
}
