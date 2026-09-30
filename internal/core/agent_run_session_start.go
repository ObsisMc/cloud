package core

import (
	"context"
	"encoding/json"
	"fmt"
)

// agentSessionStartBatchSize bounds the Phase 3A retry-loop scan so a single tick releases at most
// this many first prompts. Ordering is deterministic (created_at, id) so every tick drains the
// oldest start first; work beyond the bound is released on the next tick (§19).
const agentSessionStartBatchSize = 100

// AgentRunSessionStart is the B-owned Session Start core (IssueRun D3, D-012..D-015, G-007). For a
// run that has settled into phase='starting' (the run Workspace is provisioned and admitted), it
// hands the first prompt to the Agent exactly once: it writes the immutable first produce as the
// Thread's first entry (thread_entries seq=1, source='system', kind='user_turn') and, in the same
// transaction, declares exactly one 'agent_session' execution_work item (D-014). It never touches
// issue_runs.phase/status (§16): the run leaves 'starting' only in Phase 4 upon authoritative
// session-start/Thread-takeover evidence (D-014). Retry/recovery is a separate post-settle starting
// scan owned by this type (D-015).
//
// Ownership boundary: this is the B side. The A seam it calls (AgentRunControlPlane.
// EnqueueExecutionWork) runs and commits inside the caller's transaction; a non-nil seam error
// panics databaseFailure so the shared transaction rolls back the seq=1 write together with the
// declaration — never an orphan first produce, never double work.
type AgentRunSessionStart struct {
	store *Store
}

// renderAgentInitialTurn produces the deterministic first-prompt content from the frozen run-create
// snapshot (D-013, G-007). It reads only the immutable issue_runs.input the business layer froze at
// run creation (issue_run_lifecycle.buildRunContext + agent_target.snapshotAgentRunInput), never the
// current roster. Determinism: the section order is fixed and each embedded structure is
// canonicalised with encoding/json, which sorts map keys, so two renders of the same snapshot are
// byte-identical — the replay once-guard depends on the content being stable.
func renderAgentInitialTurn(input Object) string {
	s := input.S("task")
	if s == "" {
		s = "[no task statement]"
	}
	return fmt.Sprintf(
		"Begin this task for the Space agent.\n\nTask\n%s\n\nContext\n%s",
		s,
		canonicalJSON(Object{
			"inputs":      input["interactionValues"],
			"target":      input["target"],
			"contextRefs": input["contextRefs"],
		}),
	)
}

// canonicalJSON marshals v deterministically (encoding/json sorts map keys) so identical objects
// produce identical bytes. A marshaling failure (a snapshot value no JSON can represent) returns ""
// rather than panicking a business transaction over a cosmetic renderer.
func canonicalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// runWorkspaceLive reports whether the run's Workspace is alive for a session start: it still
// exists, is bound to this exact run, and is not soft-deleted. The workspaces.issue_run_id FK plus
// the issue_runs_workspace_id_uniq unique already rule out dangling or cross-run bindings; this
// predicate closes the soft-delete case (G-011). Callers fail closed when it is false — the run
// stays 'starting' and the retry loop resurfaces it, recording no terminal state (§6/§11/G-011).
func runWorkspaceLive(t *transaction, wid, runID string) bool {
	return t.one(`SELECT id FROM workspaces WHERE id=$1 AND issue_run_id=$2 AND deleted_at IS NULL`, wid, runID) != nil
}

// sessionStartTarget derives the minimal deterministic execution-work target for an AgentSession
// (D6 AgentSession target): the run Workspace always, plus the connected sandbox/node identities
// when the provisioning already exposed them (so a Controller with connectivity can act
// immediately). It is the latest-read view, not a frozen snapshot; the control plane correlates by
// these ids and the controller validates them against the Workspace's live sandbox.
func sessionStartTarget(t *transaction, wid string) Object {
	target := Object{"workspace_id": wid}
	if s := t.one(`SELECT id FROM sandbox_instances WHERE workspace_id=$1 AND terminated_at IS NULL ORDER BY created_at LIMIT 1`, wid); s != nil {
		target["sandbox_instance_id"] = s.S("id")
	}
	if n := t.one(`
		SELECT n.id FROM node_instances n
		JOIN sandbox_instances sb ON sb.id = n.sandbox_instance_id
		WHERE sb.workspace_id = $1 AND sb.terminated_at IS NULL
		  AND n.connection_state = 'connected' AND n.ended_at IS NULL
		ORDER BY n.id LIMIT 1`, wid); n != nil {
		target["node_id"] = n.S("id")
	}
	return target
}

// StartSession releases the first prompt for a single run that has settled into phase='starting'. It
// is the exact-once, cancel-first, atomic switch the ADR and D-012..D-015 require:
//
//	queried contract: executor_type='agent' AND phase='starting' AND status='dispatched' AND a
//	                   live run Workspace, not cancelled, not soft-deleted — otherwise no-op.
//	cancel first:     cancel_requested_at is excluded by the WHERE, so a cancelled run never reaches
//	                   a declaration; a cancel that lands while this transaction waits on the
//	                   advisory lock is re-checked by the re-read (§5). Zero execution work.
//	workspace alive:  a missing/soft-deleted Workspace fails closed (runWorkspaceLive) and stays
//	                   'starting' for the retry loop — no failure_reason, no terminal state (G-011).
//	once guard:       thread_entries seq=1 (source='system', kind='user_turn') is B's durable
//	                   declaration marker (D-013). INSERT ... ON CONFLICT DO NOTHING; a 0-affected
//	                   replay means the first prompt was already declared, so the enqueue is skipped
//	                   and work is never released twice (D-012, D-015).
//	atomicity:        the seq=1 write and the enqueue land in ONE transaction (§14); a seam error
//	                   panics databaseFailure and rolls back the seq=1 write with it (T3-13), so a
//	                   first produce never exists without its declaration.
//	phase/status:     never written here — the run exits 'starting' only in Phase 4 (§16).
//
// It returns an error only for a real failure; cancellations, ineligible runs and already-declared
// replays are nil no-ops that leave the run 'starting' for the loop to skip.
func (ss *AgentRunSessionStart) StartSession(ctx context.Context, runID string) error {
	_, err := ss.store.transact(ctx, func(t *transaction) Object {
		o := t.one(`
			SELECT ir.* FROM issue_runs ir
			WHERE ir.id = $1
			  AND ir.executor_type = 'agent'
			  AND ir.phase = 'starting'
			  AND ir.status = 'dispatched'
			  AND ir.workspace_id IS NOT NULL
			  AND ir.cancel_requested_at IS NULL
			  AND ir.deleted_at IS NULL`, runID)
		if o == nil {
			// Not a startable run: already advanced (Phase 4), cancelled, or not an agent run.
			return Object{}
		}
		wid := o.S("workspaceId")
		if !runWorkspaceLive(t, wid, runID) {
			// Fail closed (G-011): leave retryable, record no terminal state.
			return Object{}
		}
		snap := o.O("input")
		content := renderAgentInitialTurn(snap)
		turnID := newID()
		// Exactly-once marker write. 0 affected rows → a prior StartSession already declared the
		// first produce; keep the run 'starting' and skip the enqueue.
		if t.execRows(`
			INSERT INTO thread_entries(run_id, seq, source, kind, record, turn_id)
			VALUES($1, 1, 'system', 'user_turn', $2, $3)
			ON CONFLICT (run_id, seq) DO NOTHING`,
			runID, jsonText(Object{"content": content}), turnID) == 0 {
			return Object{}
		}
		// Authoritative declaration in the same transaction as seq=1 (§14). The payload fixes the
		// frozen plugin identity/version from the snapshot — never re-reads the roster (D-013,
		// G-007/G-009). A seam error rolls back the seq=1 write too.
		if _, err := ss.store.agentRunControlPlane().EnqueueExecutionWork(t, o, "agent_session",
			AgentSessionWork{
				AgentPluginID:      snap.S("agentPluginId"),
				AgentPluginVersion: snap.S("agentPluginVersion"),
				InitialTurn:        Object{"turn_id": turnID, "content": content},
			}.inputObject(),
			sessionStartTarget(t, wid),
			nil); err != nil {
			panic(databaseFailure{err})
		}
		return Object{}
	})
	return err
}

// scanStartingAgentRuns returns the bounded, deterministically ordered ids of runs that settled into
// 'starting' but have not yet declared their first produce, for the retry loop. It is a read-only
// short scan outside the advisory lock (exactness lives in each StartSession transaction), bounded
// by LIMIT and ordered by (created_at, id). Eligibility mirrors StartSession: an agent run currently
// 'starting'/'dispatched' with a live run Workspace, not cancelled, not soft-deleted, backing a real
// space_agents executor, and with no Thread entry seq=1 yet (so already-started runs are excluded
// from restart; the in-transaction once guard is still authoritative for races).
func (s *Store) scanStartingAgentRuns(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.Pool.QueryContext(ctx, `
		SELECT ir.id FROM issue_runs ir
		WHERE ir.executor_type = 'agent'
		  AND ir.phase = 'starting'
		  AND ir.status = 'dispatched'
		  AND ir.workspace_id IS NOT NULL
		  AND ir.cancel_requested_at IS NULL
		  AND ir.deleted_at IS NULL
		  AND EXISTS (SELECT 1 FROM space_agents sa WHERE sa.id = ir.executor_id AND sa.tenant_id = ir.tenant_id)
		  AND NOT EXISTS (
		    SELECT 1 FROM thread_entries te WHERE te.run_id = ir.id AND te.seq = 1
		  )
		ORDER BY ir.created_at, ir.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// StartQueuedAgentSessionsOnce is one bounded retry-loop pass: scan the eligible 'starting' runs
// (read-only) and start each in its own short StartSession transaction, with the sleep between ticks
// owned by the caller (D-015, G-007). Per-run failures are deliberately not surfaced here —
// StartSession already rolls back on error and keeps the run 'starting' for a later tick, and the
// loop must not flood logs merely because the A seam is unwired (Unavailable) or one run is
// transiently stuck. Only a scan-level failure cancels the pass.
func (s *Store) StartQueuedAgentSessionsOnce(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	ids, err := s.scanStartingAgentRuns(ctx, agentSessionStartBatchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = s.agentRunSessionStart().StartSession(ctx, id)
	}
	return nil
}

// agentRunSessionStart returns the Session Start core for this Store, constructing a fresh
// per-call value (nil-safe like agentRunDispatcher). No Store field is needed: the struct only
// owns a pointer back to the Store.
func (s *Store) agentRunSessionStart() *AgentRunSessionStart {
	return &AgentRunSessionStart{store: s}
}
