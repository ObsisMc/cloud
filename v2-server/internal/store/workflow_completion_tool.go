package store

import (
	"context"
	"fmt"
)

// WorkflowNodeTurnLive reports whether a node run of a running run executes a turn of its own open
// session while the host execution its Agent runs in does: only then may that Agent's completion
// tool be called. It reads stored state alone, so a session, turn or execution that ended stays
// refused whatever the process that asks remembers.
func (s *Store) WorkflowNodeTurnLive(ctx context.Context, nodeRunID, sessionID, executionID string) (bool, error) {
	var live bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM v2_workflow_node_runs n
		JOIN v2_workflow_runs r ON r.id = n.run_id
		JOIN v2_execution_sessions s ON s.id = n.session_id AND s.tenant_id = n.tenant_id
		WHERE n.id = $1 AND n.session_id = $2 AND n.status = 'running' AND n.deleted_at IS NULL
			AND r.status = 'running' AND r.deleted_at IS NULL AND s.ended_at IS NULL
			AND EXISTS (SELECT 1 FROM v2_execution_turns t WHERE t.session_id = s.id AND t.ended_at IS NULL)
			AND EXISTS (SELECT 1 FROM v2_host_executions x WHERE x.id = $3 AND x.session_id = s.id AND x.ended_at IS NULL))`,
		nodeRunID, sessionID, executionID).Scan(&live)
	if err != nil {
		return false, fmt.Errorf("read the turn of node run %s: %w", nodeRunID, err)
	}
	return live, nil
}
