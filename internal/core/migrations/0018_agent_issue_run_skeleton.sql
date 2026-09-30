-- Agent IssueRun skeleton (business side): schema only, no state transitions.
--
-- Adds the IssueRun phase/workspace/cancel columns and the Thread state columns
-- (IssueRun D2/D3/D6, Thread D1/D4), the per-space agent registry (IssueRun D1)
-- and the durable Thread entry table (Thread D1). The business transitions that
-- write these tables arrive with the AgentRunDispatcher and the controller
-- integration D6 hooks; this migration is purely additive over 0010..0017 and
-- leaves existing columns and status values unchanged.

-- IssueRun D1: one space_agents row per (space, plugin). Installing an agent-kind
-- plugin converges to 'active'; removal retires the row without deleting it so
-- historical IssueRuns keep resolving their executor. plugin_id is the canonical
-- namespace/identifier identity.
CREATE TABLE space_agents (
  id uuid PRIMARY KEY,
  space_id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  plugin_id text NOT NULL CHECK (plugin_id ~ '^[^/]+/[^/]+$' AND length(plugin_id) <= 200),
  display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
  status text NOT NULL CHECK (status IN ('active','retired')),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  retired_at timestamptz,
  FOREIGN KEY (space_id, tenant_id) REFERENCES collab_workspaces(id, tenant_id),
  UNIQUE (space_id, plugin_id)
);
CREATE INDEX space_agents_space ON space_agents(space_id, status);

-- IssueRun D2/D3/D6 and Thread D1/D4: agent-only columns on issue_runs. They stay
-- NULL for team/workflow runs; phase and thread_state are written by the business
-- transitions, never by the control plane (D6 table-ownership invariant). status
-- keeps its existing meaning and is derived from phase by the business layer.
ALTER TABLE issue_runs
  ADD COLUMN phase text CHECK (phase IN ('provisioning','starting','running','delivering','releasing','done')),
  ADD COLUMN workspace_id uuid REFERENCES workspaces(id),
  ADD COLUMN cancel_requested_at timestamptz,
  ADD COLUMN thread_state text CHECK (thread_state IN ('pending','active','idle','ending','ended')),
  ADD COLUMN idle_since timestamptz;
-- IssueRun D3: phase is only used for executor_type = 'agent'; the run workspace
-- and Thread belong to Agent IssueRuns as well.
ALTER TABLE issue_runs ADD CONSTRAINT issue_runs_agent_columns
  CHECK (executor_type = 'agent' OR (phase IS NULL AND workspace_id IS NULL AND thread_state IS NULL));
ALTER TABLE issue_runs ADD CONSTRAINT issue_runs_workspace_id_uniq UNIQUE (workspace_id);

-- IssueRun D2: the run Workspace is exclusive to one IssueRun and vice versa.
-- Migrate applies every pending migration in one transaction, and 0016's UPDATE on
-- workspaces leaves deferred trigger events pending that forbid ALTER on workspaces
-- afterwards; flushing the constraints now clears those events before the ALTER.
SET CONSTRAINTS ALL IMMEDIATE;
ALTER TABLE workspaces ADD COLUMN issue_run_id uuid REFERENCES issue_runs(id);
ALTER TABLE workspaces ADD CONSTRAINT workspaces_issue_run_id_uniq UNIQUE (issue_run_id);

-- Thread D1: one durable entry per (run, seq); seq is Cloud-assigned and gapless
-- within a Thread. Node-sourced entries are idempotent on
-- (node_execution_id, node_sequence); user/system entries leave both NULL.
-- kind is the ora-history HistoryLine type tag; the known set is owned by the
-- business layer (Thread D2), so the schema only bounds its length. record is
-- validated as a JSON object of at most 256 KiB (Thread D2).
CREATE TABLE thread_entries (
  run_id uuid NOT NULL REFERENCES issue_runs(id),
  seq bigint NOT NULL CHECK (seq > 0),
  source text NOT NULL CHECK (source IN ('node','user','system')),
  kind text NOT NULL CHECK (length(kind) BETWEEN 1 AND 200),
  record jsonb NOT NULL CHECK (jsonb_typeof(record) = 'object' AND octet_length(record::text) <= 262144),
  turn_id uuid,
  node_execution_id text CHECK (length(node_execution_id) BETWEEN 1 AND 200),
  node_sequence bigint CHECK (node_sequence IS NULL OR node_sequence >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (run_id, seq),
  CHECK ((node_execution_id IS NULL) = (node_sequence IS NULL))
);
CREATE UNIQUE INDEX thread_entries_node_uniq ON thread_entries(node_execution_id, node_sequence)
  WHERE node_execution_id IS NOT NULL AND node_sequence IS NOT NULL;
