-- Auth Phase 1: bcrypt password hashes on users, prefix lookup on api_tokens.
-- SQLite version.

ALTER TABLE users ADD COLUMN password_hash BLOB;

ALTER TABLE api_tokens ADD COLUMN prefix TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_prefix ON api_tokens (prefix);
