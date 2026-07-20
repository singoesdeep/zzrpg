-- Refresh tokens for rotating, revocable sessions. Only a hash of the token is
-- stored (the raw token is shown to the client once); a row is deleted when the
-- token is rotated (consumed) or the user logs out, and expired rows are ignored.
CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash TEXT        PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens (user_id);
