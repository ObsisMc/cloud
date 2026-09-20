-- Gateway-owned authentication tables. The Gateway runtime role needs only DML on these two
-- tables; Cloud business tables are never read by the Gateway and never reference these rows.

CREATE TABLE gateway_login_attempts (
 id uuid PRIMARY KEY,
 secret_hash bytea NOT NULL UNIQUE CHECK(octet_length(secret_hash)=32),
 state_hash bytea NOT NULL UNIQUE CHECK(octet_length(state_hash)=32),
 provider text NOT NULL CHECK(length(provider) BETWEEN 1 AND 64),
 return_to text NOT NULL CHECK(
  length(return_to) BETWEEN 1 AND 2048 AND return_to LIKE '/%' AND return_to NOT LIKE '//%' AND return_to NOT LIKE '/\\%'
 ),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 CHECK(expires_at > created_at AND expires_at <= created_at + interval '1 hour'),
 CHECK(consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX gateway_login_attempts_expires_at ON gateway_login_attempts(expires_at);

CREATE TABLE gateway_sessions (
 id uuid PRIMARY KEY,
 token_hash bytea NOT NULL UNIQUE CHECK(octet_length(token_hash)=32),
 source text NOT NULL CHECK(length(source) BETWEEN 1 AND 128),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 512),
 display_name text CHECK(display_name IS NULL OR length(display_name) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 revoked_reason text CHECK(revoked_reason IN ('logout','identity_revoked','administrative')),
 CHECK(expires_at > created_at AND expires_at <= created_at + interval '90 days'),
 CHECK((revoked_at IS NULL) = (revoked_reason IS NULL)),
 CHECK(revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX gateway_sessions_identity ON gateway_sessions(source, subject) WHERE revoked_at IS NULL;
CREATE INDEX gateway_sessions_expires_at ON gateway_sessions(expires_at);
CREATE INDEX gateway_sessions_revoked_at ON gateway_sessions(revoked_at) WHERE revoked_at IS NOT NULL;
