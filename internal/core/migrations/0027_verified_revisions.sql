-- Preserve raw Node results; the Cloud verification verdict is a separate authoritative fact.
ALTER TABLE issue_runs ADD CONSTRAINT issue_run_tenant_identity UNIQUE(id,tenant_id);
ALTER TABLE workspaces ADD CONSTRAINT workspace_run_identity UNIQUE(id,issue_run_id,project_id,tenant_id);
CREATE TABLE revision_delivery_skips (
 run_id uuid PRIMARY KEY REFERENCES issue_runs(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE revision_verifications (
 execution_id text PRIMARY KEY REFERENCES node_executions(execution_id),
 outcome text NOT NULL CHECK(outcome IN ('delivered','unchanged','failed')),
 reason text,
 verified_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((outcome='failed') = (reason IS NOT NULL))
);
CREATE TABLE revisions (
 id uuid PRIMARY KEY,
 execution_id text NOT NULL UNIQUE REFERENCES revision_verifications(execution_id),
 tenant_id uuid NOT NULL,
 run_id uuid NOT NULL UNIQUE,
 workspace_id uuid NOT NULL,
 project_id uuid NOT NULL,
 repository_url text NOT NULL,
 base_commit text NOT NULL CHECK(base_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
 final_commit text NOT NULL CHECK(final_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
 revision_ref text NOT NULL,
 bundle_key text,
 bundle_size bigint,
 bundle_sha256 text,
 history_key text NOT NULL,
 history_size bigint NOT NULL CHECK(history_size>=0),
 history_sha256 text NOT NULL CHECK(history_sha256 ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz,
 FOREIGN KEY(run_id,tenant_id) REFERENCES issue_runs(id,tenant_id),
 FOREIGN KEY(workspace_id,run_id,project_id,tenant_id) REFERENCES workspaces(id,issue_run_id,project_id,tenant_id),
 CHECK ((base_commit=final_commit AND num_nonnulls(bundle_key,bundle_size,bundle_sha256)=0) OR
  (base_commit<>final_commit AND num_nonnulls(bundle_key,bundle_size,bundle_sha256)=3 AND bundle_size>0 AND bundle_sha256 ~ '^[0-9a-f]{64}$'))
);
