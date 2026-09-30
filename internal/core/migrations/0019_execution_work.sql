-- Agent-run execution work persistence (Phase 3B, controller-integration D6 execution_work table).
--
-- The control-plane A side owns execution_work: it is the authoritative, durable declaration that a
-- run has exactly one piece of Agent work (first session: kind='agent_session') waiting for the
-- lease-holding Controller to pick up. B writes it only through the approved
-- EnqueueExecutionWork seam inside the caller's transaction; A reads it through the pickup
-- (agent_work_claim) and later writes execution_id when it registers the execution (Phase 4). It is
-- independent of the clone_requests/clone_executions registry and of the Effect-level operations
-- model until a later decision aligns them.
--
-- run_id is a semantic reference to the IssueRun (issue_runs.id) per D6; the FK gives integrity and
-- preserves tenant scope. workspace_id is the run Workspace the work targets (workspaces.id), the
-- plane the Controller already reads. input/target are fixed JSON snapshots the Enqueue caller
-- passes verbatim — the control plane must consume them as-is and never re-read the current
-- space_agents/space_plugins version to override them (IssueRun D1/D6).
--
-- The partial unique index execution_work_unregistered_once is the authoritative exactly-once guard
-- (G-008, D-012): a run may have at most ONE un-registered work item. Once the Controller registers
-- an execution (writes execution_id), the slot frees so a later deliver_revision retry is a new
-- work item (IssueRun D5); while still un-registered, a duplicate declaration is impossible at the
-- database layer regardless of any application-level once-guard.
CREATE TABLE execution_work (
  id uuid PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  run_id uuid NOT NULL REFERENCES issue_runs(id),
  workspace_id uuid NOT NULL REFERENCES workspaces(id),
  kind text NOT NULL CHECK (kind IN ('agent_session','deliver_revision')),
  input jsonb NOT NULL CHECK (jsonb_typeof(input) = 'object'),
  target jsonb NOT NULL CHECK (jsonb_typeof(target) = 'object'),
  available_at timestamptz NOT NULL DEFAULT now(),
  execution_id text CHECK (execution_id IS NULL OR length(execution_id) BETWEEN 1 AND 200),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
-- At most one un-registered work item per run (D6 partial-unique index). Explicit exclusion to make
-- the predicate self-describing and to support ON CONFLICT (run_id) WHERE execution_id IS NULL.
CREATE UNIQUE INDEX execution_work_unregistered_once
  ON execution_work(run_id) WHERE execution_id IS NULL;
-- Pickup ordering for a Controller claim: oldest eligible (available_at reached, not yet registered)
-- work of the requested kind first, ties broken by creation order.
CREATE INDEX execution_work_pickup
  ON execution_work(kind, available_at, created_at) WHERE execution_id IS NULL;