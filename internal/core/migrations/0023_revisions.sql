-- Revisions: the durable record of one IssueRun's saved outcome (Cloud Revision D4; IssueRun D5).
--
-- A Revision is written by the control plane inside the delivery execution's terminal-event takeover
-- transaction — the same transaction as the receipt, `node_executions.result` and the run's move to
-- `releasing` — and only after Cloud has verified, outside that transaction, that every object the
-- Node declared exists with the declared size and SHA-256. There is therefore no state in which a
-- Revision row exists without the run having advanced, or the run advanced without its Revision, and
-- no row can ever point at an object Cloud did not check (Cloud Revision invariant 8).
--
-- The row records the Revision's CONTENT identity: `final_commit` plus the object digests. The
-- `revision_ref` is stored for traceability only — a retry moves the same per-run ref to a new
-- snapshot commit, so "where the ref points now" is never what this row means (D4).
--
-- Purely additive: no existing table is altered and no existing row is rewritten.

CREATE TABLE revisions (
  id uuid PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  -- One Revision per logical delivery, and one logical delivery per run: the uniqueness is the
  -- database backstop for the registration rule that a run registers at most one Revision, however
  -- many delivery attempts it took (D4, invariant 7). NOT the primary key, because the attempt that
  -- registers is not the identity of the outcome.
  run_id uuid NOT NULL UNIQUE REFERENCES issue_runs(id),
  workspace_id uuid NOT NULL REFERENCES workspaces(id),
  project_id uuid NOT NULL REFERENCES projects(id),
  -- The project's repository URL at registration time, copied rather than joined at read time: the
  -- Project may later be repointed at another repository, and this row must keep describing the
  -- checkout the Revision was actually taken from.
  repository_url text NOT NULL CHECK (length(repository_url) BETWEEN 1 AND 2048),
  base_commit text NOT NULL CHECK (base_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  final_commit text NOT NULL CHECK (final_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  -- Cloud is the only generator of this name and it is always under `refs/ora/revisions/` (D4); the
  -- CHECK is the database-level backstop for that promise, not a parser for the run id inside it.
  revision_ref text NOT NULL CHECK (revision_ref LIKE 'refs/ora/revisions/%'),
  -- The bundle exists exactly when the checkout changed. All three columns are NULL together or set
  -- together, so "no bundle" is a fact of the row rather than a sentinel size or an empty digest.
  bundle_key text,
  bundle_size bigint,
  bundle_sha256 text,
  -- The session history is written for every Revision, including an unchanged checkout (D4/invariant 5).
  history_key text NOT NULL,
  history_size bigint NOT NULL CHECK (history_size >= 0),
  history_sha256 text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  -- Always NULL in the first version: object expiry and cleanup are an explicit non-goal of the
  -- approved decision, and a defaulted window here would promise a retention rule nothing enforces.
  expires_at timestamptz,
  CONSTRAINT revisions_bundle_all_or_nothing CHECK (
    (bundle_key IS NULL) = (bundle_size IS NULL)
    AND (bundle_key IS NULL) = (bundle_sha256 IS NULL)),
  CONSTRAINT revisions_bundle_size CHECK (bundle_size IS NULL OR bundle_size >= 0),
  -- Digests are stored in the one spelling the verification compares in (lowercase hex), so a
  -- reader never has to normalize before comparing a row against a Node's declaration.
  CONSTRAINT revisions_bundle_sha256 CHECK (bundle_sha256 IS NULL OR bundle_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT revisions_history_sha256 CHECK (history_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT revisions_bundle_key CHECK (bundle_key IS NULL OR length(bundle_key) BETWEEN 1 AND 1024),
  CONSTRAINT revisions_history_key CHECK (length(history_key) BETWEEN 1 AND 1024)
);
