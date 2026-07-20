-- Adds a role column for RBAC. Existing and new users default to 'player';
-- administrators must be promoted explicitly (e.g. UPDATE users SET role='admin').
ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'player';

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
