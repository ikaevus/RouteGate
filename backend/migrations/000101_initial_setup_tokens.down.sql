DROP TABLE IF EXISTS initial_setup_tokens;

ALTER TABLE users
  DROP COLUMN IF EXISTS initial_setup_completed_at;
