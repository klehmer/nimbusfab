-- Auth Phase 1: bcrypt password hashes on users, prefix lookup on api_tokens.
-- Postgres version.

ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash BYTEA;

ALTER TABLE api_tokens ADD COLUMN IF NOT EXISTS prefix TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_prefix ON api_tokens (prefix);
