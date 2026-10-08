-- Node Thread event receipts (Phase 4B; controller-integration D6 node_event_receipts table).
--
-- A receipt is the durable, control-plane-owned proof that Cloud took over one Node event with the
-- exact identity (execution_id, sequence). It is the ONLY basis for the EventAck the Controller
-- sends back to the Node (protocol root D4), so it must be committed before any ack. Non-terminal
-- Thread events and the single terminal event of an execution share this table and the same
-- per-execution sequence space (Node protocol D2 invariant 2); last_event_sequence on
-- node_executions mirrors the largest receipt and is the replay boundary a Node resumes from.
--
-- event holds the canonical JSON of the taken-over Node ThreadEvent ({sequence, turnId, record,
-- truncated}), never the raw wire bytes: the identity comparison that separates an idempotent
-- replay from a payload conflict is defined on this canonical object (D-022). A terminal
-- TakeOverNodeEvent stores its own canonical event shape in the same column.
--
-- The FK binds a receipt to an already-registered execution: a receipt can never exist for an
-- execution Cloud never registered (no "receipt without execution"). Phase 4B is append-only here;
-- nothing in this phase deletes or rewrites a receipt, so a Node replay after a lost ack always
-- finds its original fact.
CREATE TABLE node_event_receipts (
  execution_id text NOT NULL REFERENCES node_executions(execution_id),
  sequence bigint NOT NULL CHECK (sequence > 0),
  event jsonb NOT NULL CHECK (jsonb_typeof(event) = 'object'),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (execution_id, sequence)
);
-- Retention scan key (plan §4B.10): receipts are held for the whole life of the run because a Node
-- replay needs them, so no cleanup runs in Phase 4B and G-015 stays PARTIAL. The index exists so a
-- later, separately decided retention pass can find candidates by age without a full scan.
CREATE INDEX node_event_receipts_retention ON node_event_receipts(created_at);
