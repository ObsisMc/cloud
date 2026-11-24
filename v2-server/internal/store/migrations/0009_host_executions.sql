-- A host execution is one Node execution whose Agent was given host tools (the completion tool of an
-- interactive workflow node). It is recorded before its start is sent to Node and settled once, with a
-- named reason, when it ends: Cloud ended its session (session_ended, or the failure that made Cloud
-- end it), Node reported its end (Node's reason), its Node was lost (agent_transport_closed), or this
-- server restarted (server_restarted). A session that survives a restart gets a new execution, so it
-- may have several; none is running when the server starts to serve host tools. Executions without host
-- tools, and those started before this table existed, have no row. started_at is when the record was
-- written, and the start may never have been sent; ended_at of server_restarted is when the next start
-- settled it.
CREATE TABLE v2_host_executions (
	id         text PRIMARY KEY CHECK (id <> ''),
	session_id text NOT NULL REFERENCES v2_execution_sessions(id),
	started_at timestamptz NOT NULL DEFAULT now(),
	ended_at   timestamptz,
	reason     text NOT NULL DEFAULT '',
	CHECK ((ended_at IS NULL) = (reason = ''))
);
CREATE INDEX v2_host_executions_running ON v2_host_executions (session_id) WHERE ended_at IS NULL;
