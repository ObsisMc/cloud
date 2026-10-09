-- Durable Thread entries (Thread D1/D2/D3, IssueRun D6).
--
-- The Thread is the durable, ordered log of what a run's agent session said and was told. It is the
-- only thing that advances a client's read cursor and the only durable proof that a first prompt was
-- ever handed to an Agent, which is why it belongs to the business layer (D6 table-ownership
-- invariant): the control plane never reads or writes it, and the A→B hook receives a taken-over
-- Node event and appends here.
--
-- One row per (run, seq). `seq` is Cloud-assigned and gapless within a Thread; a Node-sourced entry
-- is additionally idempotent on (node_execution_id, node_sequence), which is what makes a replayed
-- Node batch append nothing. `kind` is the ora-history HistoryLine type tag — the known set is owned
-- by the business layer (Thread D2), so the schema bounds only its length. `record` is validated as
-- a JSON object of at most 256 KiB (Thread D2) rather than as a typed payload, so a Node record the
-- business layer does not yet recognise still round-trips byte for byte.
--
-- Purely additive over 0024..0030: a new table, no existing table altered. Retry-safe: the table and
-- its indexes are created once and guarded by this migration's own checksum.

CREATE TABLE thread_entries (
  run_id uuid NOT NULL REFERENCES issue_runs(id),
  seq bigint NOT NULL CHECK (seq > 0),
  source text NOT NULL CHECK (source IN ('node','user','system')),
  kind text NOT NULL CHECK (length(kind) BETWEEN 1 AND 200),
  record jsonb NOT NULL CHECK (jsonb_typeof(record) = 'object' AND octet_length(record::text) <= 262144),
  turn_id uuid,
  -- Thread D3's per-turn lifecycle, nullable rather than defaulted so "this entry has no turn
  -- lifecycle" stays a fact of the schema instead of a sentinel value.
  status text CHECK (status IS NULL OR status IN ('queued','delivered','discarded')),
  node_execution_id text CHECK (length(node_execution_id) BETWEEN 1 AND 200),
  node_sequence bigint CHECK (node_sequence IS NULL OR node_sequence >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (run_id, seq),
  -- A Node-sourced entry carries both halves of its idempotency key or neither: an execution id
  -- without a sequence could not be deduplicated, and a sequence without an execution could not say
  -- which session it came from.
  CHECK ((node_execution_id IS NULL) = (node_sequence IS NULL)),
  -- The turn lifecycle exists exactly for user turns (Thread D3/invariant 4): a node/system entry
  -- can never carry one, and a user turn can never be missing one. This is what makes "no user turn
  -- still queued" a two-valued idle predicate (D-4C-09) rather than a three-valued one.
  CHECK ((source = 'user') = (status IS NOT NULL))
);

-- The business duplicate guard for a replayed Node batch (Thread D2): appending the same
-- (execution, sequence) twice is a no-op, enforced by the database rather than by a read-then-write.
CREATE UNIQUE INDEX thread_entries_node_uniq ON thread_entries(node_execution_id, node_sequence)
  WHERE node_execution_id IS NOT NULL AND node_sequence IS NOT NULL;

-- Backs the idle predicate's per-run "any queued turn left?" probe (D-4C-09) without scanning a
-- Thread's whole history.
CREATE INDEX thread_entries_queued ON thread_entries(run_id) WHERE status = 'queued';
