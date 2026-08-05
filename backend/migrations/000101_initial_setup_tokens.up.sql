ALTER TABLE users
  ADD COLUMN IF NOT EXISTS initial_setup_completed_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS initial_setup_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_initial_setup_tokens_user_active
  ON initial_setup_tokens (user_id, expires_at DESC)
  WHERE used_at IS NULL;
