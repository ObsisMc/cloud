package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

// ReadExecutionCursor returns the latest committed event position after scope/owner checks.
// A concurrent event may advance it immediately; callers reject only cursors ahead of this value.
func (s *Store) ReadExecutionCursor(ctx context.Context, scope execution.Scope, sid string) (uint64, error) {
	var sequence uint64
	err := s.pool.QueryRow(ctx, `SELECT event_sequence FROM v2_execution_sessions WHERE id=$1 AND tenant_id=$2::uuid AND user_id=$3::uuid`, sid, scope.TenantID, scope.UserID).Scan(&sequence)
	return sequence, executionError(err)
}

// ReadExecutionEvents returns retained events after a session-wide sequence. An ahead cursor
// is invalid. The runtime follows this durable cursor; notifications are only wake-up hints.
func (s *Store) ReadExecutionEvents(ctx context.Context, scope execution.Scope, sid string, after uint64, limit int) ([]execution.Event, error) {
	events := []execution.Event{}
	if after > math.MaxInt64 || limit < 1 || limit > 1000 {
		return nil, execution.ErrInvalidRequest
	}
	// A read-only snapshot keeps cursor validation and event rows consistent without
	// taking the session lock needed by writers and terminal result commits.
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var current uint64
		if err := tx.QueryRow(ctx, `SELECT event_sequence FROM v2_execution_sessions WHERE id=$1 AND tenant_id=$2::uuid AND user_id=$3::uuid`, sid, scope.TenantID, scope.UserID).Scan(&current); err != nil {
			return err
		}
		if after > current {
			return execution.ErrInvalidRequest
		}
		rows, err := tx.Query(ctx, `SELECT data FROM v2_execution_events WHERE session_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3`, sid, int64(after), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var event execution.Event
			if err := json.Unmarshal(raw, &event); err != nil {
				return err
			}
			normalizeExecutionResult(event.Result)
			events = append(events, event)
		}
		return rows.Err()
	})
	return events, executionError(err)
}

// AppendExecutionEvent retains a nonterminal event, assigning its gap-free session sequence.
// Terminal events must use FinishExecutionTurn/EndExecutionSession to update state atomically.
// This runtime write checks admitted ownership, even when current membership has been revoked.
func (s *Store) AppendExecutionEvent(ctx context.Context, scope execution.Scope, event execution.Event) (execution.Event, error) {
	if event.Result != nil || (event.Kind != execution.EventTextDelta && event.Kind != execution.EventHistorySnapshot && event.Kind != execution.EventHistoryPreview && event.Kind != execution.EventPreparation) {
		return execution.Event{}, execution.ErrInvalidRequest
	}
	err := s.executionRuntimeTransaction(ctx, scope, event.Ref.SessionID, func(tx pgx.Tx, session execution.Session) error {
		if session.EndedAt != nil {
			return execution.ErrSessionEnded
		}
		if event.Ref.TurnID != "" {
			turn, err := readExecutionTurn(ctx, tx, event.Ref)
			if err != nil {
				return err
			}
			if turn.Result != nil && event.Kind == execution.EventTextDelta {
				return execution.ErrConflict
			}
		} else if event.Kind == execution.EventTextDelta {
			return execution.ErrInvalidRequest
		}
		var err error
		event, err = insertExecutionEvent(ctx, tx, event)
		return err
	})
	return event, err
}

func readExecutionTurn(ctx context.Context, tx pgx.Tx, ref execution.Ref) (execution.Turn, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT data FROM v2_execution_turns WHERE id=$1 AND session_id=$2`, ref.TurnID, ref.SessionID).Scan(&raw); err != nil {
		return execution.Turn{}, executionError(err)
	}
	var turn execution.Turn
	err := json.Unmarshal(raw, &turn)
	normalizeExecutionResult(turn.Result)
	return turn, err
}

// The caller holds the session row lock. Counter allocation rolls back with the event.
func insertExecutionEvent(ctx context.Context, tx pgx.Tx, event execution.Event) (execution.Event, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `UPDATE v2_execution_sessions SET event_sequence=event_sequence+1 WHERE id=$1 RETURNING event_sequence,now()`, event.Ref.SessionID).Scan(&event.Sequence, &now); err != nil {
		return event, err
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return event, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v2_execution_events(session_id,sequence,data) VALUES($1,$2,$3)`, event.Ref.SessionID, int64(event.Sequence), raw)
	return event, err
}

// FinishExecutionTurn commits a result and its terminal event together. The first terminal
// result wins; cancellation or cleanup racing a completed result cannot rewrite it.
// This runtime write checks admitted ownership, even when current membership has been revoked.
func (s *Store) FinishExecutionTurn(ctx context.Context, scope execution.Scope, ref execution.Ref, result execution.Result) (execution.Result, error) {
	var actual execution.Result
	err := s.executionRuntimeTransaction(ctx, scope, ref.SessionID, func(tx pgx.Tx, _ execution.Session) error {
		var err error
		actual, err = finishExecutionTurn(ctx, tx, ref, result)
		return err
	})
	return actual, err
}

func finishExecutionTurn(ctx context.Context, tx pgx.Tx, ref execution.Ref, result execution.Result) (execution.Result, error) {
	turn, err := readExecutionTurn(ctx, tx, ref)
	if err != nil {
		return execution.Result{}, err
	}
	if turn.Result != nil {
		return *turn.Result, nil
	}
	if err := validateTerminalResult(&result); err != nil {
		return execution.Result{}, err
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		return execution.Result{}, err
	}
	turn.Result, turn.EndedAt = &result, &now
	raw, err := json.Marshal(turn)
	if err != nil {
		return execution.Result{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE v2_execution_turns SET data=$2,ended_at=$3 WHERE id=$1`, ref.TurnID, raw, now); err != nil {
		return execution.Result{}, err
	}
	_, err = insertExecutionEvent(ctx, tx, execution.Event{Ref: ref, Kind: execution.EventTurnEnded, OccurredAt: now, Result: &result})
	return result, err
}

func validateTerminalResult(result *execution.Result) error {
	normalizeExecutionResult(result)
	switch result.Status {
	case execution.StatusCompleted:
		if !result.AnswerComplete {
			return execution.ErrInvalidRequest
		}
	case execution.StatusCancelled, execution.StatusFailed, execution.StatusInterrupted:
	default:
		return execution.ErrInvalidRequest
	}
	return nil
}

// EndExecutionSession marks a quiescent session ended and retains its final event once.
// The runtime must stop and finish active work first; SQL never claims execution has stopped.
// This runtime write checks admitted ownership, even when current membership has been revoked.
func (s *Store) EndExecutionSession(ctx context.Context, scope execution.Scope, sid string) error {
	return s.executionRuntimeTransaction(ctx, scope, sid, func(tx pgx.Tx, session execution.Session) error {
		return endExecutionSession(ctx, tx, session)
	})
}

func endExecutionSession(ctx context.Context, tx pgx.Tx, session execution.Session) error {
	if session.EndedAt != nil {
		return nil
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v2_execution_turns WHERE session_id=$1 AND ended_at IS NULL)`, session.ID).Scan(&active); err != nil {
		return err
	}
	if active {
		return execution.ErrConflict
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		return err
	}
	session.EndedAt = &now
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE v2_execution_sessions SET data=$2,ended_at=$3 WHERE id=$1`, session.ID, raw, now); err != nil {
		return err
	}
	// The driver settles what it knows the end of before; a session never ends with one running.
	if _, err := tx.Exec(ctx, `UPDATE v2_host_executions SET ended_at=$2, reason=$3 WHERE session_id=$1 AND ended_at IS NULL`, session.ID, now, execution.HostSessionEnded); err != nil {
		return err
	}
	_, err = insertExecutionEvent(ctx, tx, execution.Event{Ref: execution.Ref{SessionID: session.ID}, Kind: execution.EventSessionEnded, OccurredAt: now})
	return err
}

// RecoveredSession is a session RecoverExecutions kept open: the held session of an interactive
// workflow node parked awaiting input. Its Agent is gone with the old process; the next turn
// starts a new Agent on the same checkout and hands it the recorded conversation.
type RecoveredSession struct {
	Scope   execution.Scope
	Session execution.Session
	// Caller is the session's first turn's caller: the workflow run that owns it.
	Caller execution.Caller
	// CloneExecution is the clone execution of the checkout the session's conversation ran in,
	// bound when it first survived a restart; empty when its checkout had none. Only
	// RecoverExecutions sets it.
	CloneExecution string
}

// parkedInteractiveSession is the SQL condition, on the session aliased s, that the session is the
// held session of an interactive workflow node parked awaiting input in a running run: the node
// row is pending with a session summary (only a parked interactive node has both). The workflow
// host decides on startup whether the wait is valid and ends the session when it is not.
const parkedInteractiveSession = `EXISTS (SELECT 1 FROM v2_workflow_node_runs n JOIN v2_workflow_runs r ON r.id = n.run_id
	WHERE n.session_id = s.id AND n.deleted_at IS NULL AND n.status = 'pending' AND n.node_type = 'agent'
		AND n.payload ? 'interactive' AND r.status = 'running' AND r.deleted_at IS NULL)`

// RecoverExecutions runs before admission and reads are exposed by the single-process runtime.
// It ends old live sessions without replaying prompts and never rewrites completed results. The
// one exception is the held session of an interactive workflow node parked awaiting input
// (desktop keeps a valid interactive wait across a restart): it has no turn in flight and stays
// open, and is returned so the runtime can start a new Agent for it on the next turn.
// Process and filesystem cleanup, when applicable, belongs to the runtime before this call.
func (s *Store) RecoverExecutions(ctx context.Context) ([]RecoveredSession, error) {
	return s.recoverExecutions(ctx, false)
}

// RecoverExecutionsWithDeliveries concludes old sessions while retaining work for boot delivery.
func (s *Store) RecoverExecutionsWithDeliveries(ctx context.Context) ([]RecoveredSession, error) {
	return s.recoverExecutions(ctx, true)
}

func (s *Store) recoverExecutions(ctx context.Context, deliver bool) ([]RecoveredSession, error) {
	var kept []RecoveredSession
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		kept = nil
		// Every Node of the previous process has been stopped, so no execution it recorded still runs,
		// also that of a session kept below: the session's next turn starts a new one. In this
		// transaction, so a start that cannot settle them recovers nothing and serves nothing.
		if _, err := tx.Exec(ctx, `UPDATE v2_host_executions SET ended_at = now(), reason = $1 WHERE ended_at IS NULL`, execution.FailureServerRestarted); err != nil {
			return fmt.Errorf("settle host executions: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT s.data, s.tenant_id::text, s.user_id::text, `+parkedInteractiveSession+`
			AND NOT EXISTS (SELECT 1 FROM v2_execution_turns t WHERE t.session_id = s.id AND t.ended_at IS NULL)
			FROM v2_execution_sessions s WHERE s.ended_at IS NULL ORDER BY s.id FOR UPDATE OF s`)
		if err != nil {
			return err
		}
		var sessions []execution.Session
		for rows.Next() {
			var raw []byte
			var scope execution.Scope
			var parked bool
			if err := rows.Scan(&raw, &scope.TenantID, &scope.UserID, &parked); err != nil {
				rows.Close()
				return err
			}
			var session execution.Session
			if err := json.Unmarshal(raw, &session); err != nil {
				rows.Close()
				return err
			}
			if parked && session.Target != nil {
				kept = append(kept, RecoveredSession{Scope: scope, Session: session})
				continue
			}
			sessions = append(sessions, session)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range kept {
			var caller []byte
			if err := tx.QueryRow(ctx, `SELECT caller FROM v2_execution_turns WHERE session_id=$1 ORDER BY position LIMIT 1`, kept[i].Session.ID).Scan(&caller); err != nil {
				return fmt.Errorf("read recovered session caller: %w", err)
			}
			if err := json.Unmarshal(caller, &kept[i].Caller); err != nil {
				return fmt.Errorf("read recovered session caller: %w", err)
			}
			// The first restart a session survives binds it to its workspace's current clone. That is
			// the clone its conversation ran in unless the record moved before this restart: Node lost
			// its ledger while this server kept running, a later node found the clone unknown, and the
			// run cloned again. The session is then bound to the new clone and continues there without
			// its earlier edits, and nothing corrects the binding later. Binding when the session's
			// Agent first starts on a checkout would close that gap; it is a known limit (README).
			if err := tx.QueryRow(ctx, `UPDATE v2_execution_sessions s SET clone_execution = CASE WHEN s.clone_execution <> '' THEN s.clone_execution
					ELSE COALESCE((SELECT w.clone_execution FROM v2_workspaces w WHERE w.id = s.data->>'WorkspaceID'), '') END
				WHERE s.id = $1 RETURNING s.clone_execution`, kept[i].Session.ID).Scan(&kept[i].CloneExecution); err != nil {
				return fmt.Errorf("bind recovered session checkout: %w", err)
			}
		}
		for _, session := range sessions {
			var tid string
			err := tx.QueryRow(ctx, `SELECT id FROM v2_execution_turns WHERE session_id=$1 AND ended_at IS NULL`, session.ID).Scan(&tid)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			if err == nil {
				ref := execution.Ref{SessionID: session.ID, TurnID: tid}
				result := execution.Result{Status: execution.StatusInterrupted, FailureCode: execution.FailureServerRestarted}
				if deliver {
					staged, err := recoverDeliveryWork(ctx, tx, session, ref)
					if err != nil {
						return err
					}
					if staged != nil {
						result = *staged
					}
				}
				if result.Status == execution.StatusInterrupted {
					if err := tx.QueryRow(ctx, `SELECT COALESCE(string_agg(data->>'TextDelta','' ORDER BY sequence),'') FROM v2_execution_events WHERE session_id=$1 AND data->>'Kind'='text_delta' AND data#>>'{Ref,TurnID}'=$2`, session.ID, tid).Scan(&result.Answer); err != nil {
						return err
					}
				}
				if _, err := finishExecutionTurn(ctx, tx, ref, result); err != nil {
					return err
				}
			}
			if err := endExecutionSession(ctx, tx, session); err != nil {
				return err
			}
		}
		return nil
	})
	return kept, err
}

// ParkedWorkflowSessions lists the sessions, open or already ended, of every interactive workflow
// node parked awaiting input in a running run, with their owner and first caller. A node whose
// session ended before the restart (its turn was stopped, its Agent lost) still survives it and
// can be completed, so its run's later sessions must find the run's directory as well.
func (s *Store) ParkedWorkflowSessions(ctx context.Context) ([]RecoveredSession, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.data, s.tenant_id::text, s.user_id::text,
			(SELECT t.caller FROM v2_execution_turns t WHERE t.session_id = s.id ORDER BY t.position LIMIT 1)
		FROM v2_execution_sessions s WHERE `+parkedInteractiveSession+` ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("list parked workflow sessions: %w", err)
	}
	parked, err := collectWorkflowSessions(rows)
	if err != nil {
		return nil, fmt.Errorf("list parked workflow sessions: %w", err)
	}
	return parked, nil
}

// RunningWorkflowSessions lists, for every running workflow run, the session of its most recently
// created Agent node run, open or already ended. A run's sessions share one workspace, which only
// the process that started them remembers. A run the restart sweep then absorbs (an interrupted
// Iteration, a pending LoopExit cleanup) resumes with no parked node to remember it by, and its
// later nodes must start on the run's checkout, not a fresh one. Runs owned by a task (its
// handling runs and decomposition runs) are left out: their owners settle them on startup before
// the sweep, a settled one is never resumed in place, and a decomposition kept at a submitted
// review runs no further Agent (see package boot).
func (s *Store) RunningWorkflowSessions(ctx context.Context) ([]RecoveredSession, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (n.run_id) s.data, s.tenant_id::text, s.user_id::text,
			(SELECT t.caller FROM v2_execution_turns t WHERE t.session_id = s.id ORDER BY t.position LIMIT 1)
		FROM v2_workflow_node_runs n
		JOIN v2_workflow_runs r ON r.id = n.run_id
		JOIN v2_execution_sessions s ON s.id = n.session_id AND s.tenant_id = n.tenant_id
		WHERE r.status = 'running' AND r.deleted_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM v2_task_workflow_runs tw WHERE tw.run_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM v2_task_decompositions d WHERE d.workflow_run_id = r.id)
		ORDER BY n.run_id, n.created_at DESC, n.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list running workflow sessions: %w", err)
	}
	running, err := collectWorkflowSessions(rows)
	if err != nil {
		return nil, fmt.Errorf("list running workflow sessions: %w", err)
	}
	return running, nil
}

// collectWorkflowSessions reads rows of session data, tenant, user and first caller, leaving out
// sessions that are not workflow Agent sessions a recovery can serve.
func collectWorkflowSessions(rows pgx.Rows) ([]RecoveredSession, error) {
	defer rows.Close()
	var sessions []RecoveredSession
	for rows.Next() {
		var raw, caller []byte
		var r RecoveredSession
		if err := rows.Scan(&raw, &r.Scope.TenantID, &r.Scope.UserID, &caller); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &r.Session); err != nil || caller == nil || json.Unmarshal(caller, &r.Caller) != nil {
			continue // not a workflow Agent session this recovery can serve
		}
		if r.Session.Target != nil {
			sessions = append(sessions, r)
		}
	}
	return sessions, rows.Err()
}

// LatestHistory returns the settled records of a session's latest history snapshot, the
// conversation the page shows. Only the server's own recovery reads it, so it is not scoped. A
// session that never published a snapshot has none.
func (s *Store) LatestHistory(ctx context.Context, sessionID string) ([]execution.HistoryRecord, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM v2_execution_events WHERE session_id=$1 AND data->>'Kind'='history_snapshot' ORDER BY sequence DESC LIMIT 1`, sessionID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read latest history: %w", err)
	}
	var event execution.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("read latest history: %w", err)
	}
	if event.History == nil {
		return nil, nil
	}
	records := make([]execution.HistoryRecord, 0, len(event.History.Records))
	for _, record := range event.History.Records {
		if !record.Preview {
			records = append(records, record)
		}
	}
	return records, nil
}
