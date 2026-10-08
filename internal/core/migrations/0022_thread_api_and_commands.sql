-- Thread API and Thread command persistence (Phase 4C Slice 1; plan D-4C-01/D-4C-12, G-016/G-022).
--
-- Purely additive over 0018..0021. Two things land here:
--
--   * thread_commands — the control-plane table controller-integration D6 already specifies. It
--     holds a user message or an end-session request from the moment Cloud persists it until a
--     Controller has delivered it to the Node and registered that delivery. It is A-owned: the
--     business layer only ever writes it through the enqueueThreadCommand seam, inside the caller's
--     transaction (D-4C-05), which is what lets the Thread POST commit an entry and its command
--     together.
--
--   * thread_entries.status — the per-turn lifecycle Thread D3 names (`queued`, `delivered`,
--     `discarded`). D3 approves the states and invariant 4 makes them authoritative; D1's column
--     list predates them, which the readiness round registered as G-022 (proposed amendment A1).
--     The semantics are approved; only the column's presence in D1's enumeration is not.
--
-- It also materializes what 4C's lifecycle must be able to read before its writers exist: the
-- `pending` Thread state for runs whose session was already declared (D-4C-01, closing G-016), and
-- the partial index the idle-window scan will use.
--
-- Retry-safe: every statement is idempotent by construction or guarded by a predicate a second run
-- cannot satisfy.

-- Thread D1/D3, D-4C-12: per-turn lifecycle. NULL for node/system entries; a user turn is 'queued'
-- from the moment Cloud persists it until the Node echoes its turn_id ('delivered', D3) or the
-- session ends with it still queued ('discarded', D3/invariant 4). Nullable rather than defaulted so
-- "this entry has no turn lifecycle" stays a fact of the schema instead of a sentinel value.
ALTER TABLE thread_entries ADD COLUMN status text;

-- Safe backfill BEFORE the CHECK (readiness pre-check 3): a pre-existing source='user' row has no
-- recorded lifecycle, and 'queued' is the only honest value — the row was never observed as
-- delivered, and nothing has proven the session ended. No writer produces such a row today (the
-- seq=1 first prompt is source='system'; the user-turn writer arrives with the Thread POST), so this
-- normally updates zero rows. It exists so the constraints below hold on any database rather than
-- only on the ones that happen to be empty.
UPDATE thread_entries SET status = 'queued' WHERE source = 'user' AND status IS NULL;

ALTER TABLE thread_entries ADD CONSTRAINT thread_entries_status_values
  CHECK (status IS NULL OR status IN ('queued','delivered','discarded'));
-- The turn lifecycle exists exactly for user turns: a node/system entry can never carry one, and a
-- user turn can never be missing one. This is what makes "no user turn still queued" a two-valued
-- idle predicate (D-4C-09) rather than a three-valued one.
ALTER TABLE thread_entries ADD CONSTRAINT thread_entries_user_status
  CHECK ((source = 'user') = (status IS NOT NULL));

-- Backs the idle predicate's per-run "any queued turn left?" probe (D-4C-09) without scanning a
-- Thread's whole history.
CREATE INDEX thread_entries_queued ON thread_entries(run_id) WHERE status = 'queued';

-- controller-integration D6: the control-plane table for Thread commands. `id` IS the command_id the
-- proto carries; body is the canonical command payload; delivered_at / delivered_execution_id are
-- written once, by the delivery registration (D3's RecordThreadCommandDelivered), and never cleared.
-- A command is registered delivered at most once even though the Node may receive it more than once
-- — the Node dedupes by turn_id/command_id (D3).
CREATE TABLE thread_commands (
  id uuid PRIMARY KEY,
  run_id uuid NOT NULL REFERENCES issue_runs(id),
  kind text NOT NULL CHECK (kind IN ('submit_user_turn','end_session')),
  body jsonb NOT NULL CHECK (jsonb_typeof(body) = 'object'),
  created_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz,
  delivered_execution_id text REFERENCES node_executions(execution_id)
);
-- D3 registers a delivery in one call carrying both facts, so a row is either undelivered or
-- delivered-with-the-execution-that-took-it; no code path can record half of it. The FK mirrors
-- node_event_receipts: a delivery can never be registered against an execution Cloud never
-- registered.
ALTER TABLE thread_commands ADD CONSTRAINT thread_commands_delivery
  CHECK ((delivered_at IS NULL) = (delivered_execution_id IS NULL));
-- ClaimThreadCommands reads undelivered commands grouped by run in creation order (D3). Partial so
-- the probe stays proportional to the backlog rather than to every command ever persisted.
CREATE INDEX thread_commands_undelivered ON thread_commands(run_id, created_at, id)
  WHERE delivered_at IS NULL;

-- G-016 / D-4C-01: materialize the Thread state for runs whose session was already declared. The
-- predicate is the business fact — a real declaration, i.e. the run's first prompt written as
-- thread_entries seq=1 (source='system', kind='user_turn') by StartSession (D-013) — and deliberately
-- NOT a stage proxy like "phase IS NOT NULL": phase says the run reached a stage, not that a session
-- was ever handed to the Agent.
--
-- It mirrors StartSession's own eligibility predicate (agent, starting, dispatched, a bound
-- Workspace, not cancelled, not soft-deleted) so the backfill materializes exactly the set the
-- runtime would have materialized, and it excludes cancelled and terminal runs, which are past the
-- point where a pending Thread means anything. Re-running is a no-op: every row it would touch has
-- thread_state IS NULL, and this is the only writer of that column in this file.
UPDATE issue_runs ir
SET thread_state = 'pending'
WHERE ir.executor_type = 'agent'
  AND ir.phase = 'starting'
  AND ir.status = 'dispatched'
  AND ir.workspace_id IS NOT NULL
  AND ir.cancel_requested_at IS NULL
  AND ir.deleted_at IS NULL
  AND ir.thread_state IS NULL
  AND EXISTS (
    SELECT 1 FROM thread_entries te
    WHERE te.run_id = ir.id AND te.seq = 1 AND te.source = 'system' AND te.kind = 'user_turn'
  );

-- The idle-window scan (D-4C-04/D-4C-10) looks for Threads whose idle_since is older than the
-- configured window; idle_since is only meaningful while thread_state = 'idle'.
CREATE INDEX issue_runs_idle_threads ON issue_runs(idle_since) WHERE thread_state = 'idle';
