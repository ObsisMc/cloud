-- Agent IssueRun business lifecycle columns (IssueRun D2/D3/D6, Thread D4).
--
-- The control plane (0024..0029) owns the *execution* side of an Agent IssueRun: execution_work,
-- node_executions, node_event_receipts, thread_commands, space_agents, workspaces.issue_run_id and
-- the verified-Revision tables. This migration adds only the BUSINESS half those tables did not
-- carry: the run's lifecycle phase, its exclusive run-Workspace binding, its cancellation request
-- and its Thread state. Every one of them is written by the business layer alone (D6 table-ownership
-- invariant), which is why they are a separate forward migration rather than a change to 0024.
--
-- Purely additive over 0024..0029: no existing column changes meaning, no existing row is rewritten,
-- and every statement is retry-safe (the columns are added once and guarded by the migration's own
-- checksum, and the constraints are expressed on columns this file creates).

-- IssueRun D3: phase is the business stage of an agent run, derived alongside the pre-existing
-- `status` (which keeps its 0010 meaning and value set). It stays NULL for team/workflow runs.
ALTER TABLE issue_runs
  ADD COLUMN phase text CHECK (phase IN ('provisioning','starting','running','delivering','releasing','done')),
  -- IssueRun D2: the run Workspace this run owns. The reverse binding (workspaces.issue_run_id,
  -- 0024) is what the control plane resolves operations by; this column is the business layer's
  -- direct handle on it, and UNIQUE below makes the binding exclusive in both directions.
  ADD COLUMN workspace_id uuid REFERENCES workspaces(id),
  -- IssueRun D6: a cancel request recorded during provisioning releases the run without waiting for
  -- a session that will never start. Read by the settle, dispatch and session-start predicates.
  ADD COLUMN cancel_requested_at timestamptz,
  -- Thread D4: the Thread's own lifecycle state, independent of the run's phase (a Thread can be
  -- `ending` while the run is still `running`). NULL means no session was ever declared.
  ADD COLUMN thread_state text CHECK (thread_state IN ('pending','active','idle','ending','ended')),
  -- Thread D4: when the Thread last went idle. Only meaningful while thread_state='idle', and
  -- cleared with the state, so a stale instant can never be read as a live idle window.
  ADD COLUMN idle_since timestamptz;

-- IssueRun D3: these columns describe agent runs only. A team/workflow run must not carry a phase or
-- a Thread — the business transitions never write them and the control plane never reads them, so a
-- value here could only be a bug.
ALTER TABLE issue_runs ADD CONSTRAINT issue_runs_agent_columns
  CHECK (executor_type = 'agent' OR (phase IS NULL AND workspace_id IS NULL AND thread_state IS NULL));

-- IssueRun D2: one Workspace belongs to at most one run. Multiple NULLs are allowed (the column is
-- NULL for every non-agent run and for an agent run before its Workspace is created), which is
-- exactly "no binding yet" rather than a second binding.
ALTER TABLE issue_runs ADD CONSTRAINT issue_runs_workspace_id_uniq UNIQUE (workspace_id);

-- D-4C-04/D-4C-10: the idle-window scan looks for Threads whose idle_since is older than the
-- configured window. Partial, because idle_since is only ever read while the Thread is idle.
CREATE INDEX issue_runs_idle_threads ON issue_runs(idle_since) WHERE thread_state = 'idle';

-- No backfill: every column above is created by this same migration, so no pre-existing row can
-- carry a phase, a Thread state or an idle instant to migrate. A run that predates this file is a
-- non-agent run (executor_type <> 'agent') and stays exactly as it was.
