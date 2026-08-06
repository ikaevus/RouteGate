CREATE TABLE IF NOT EXISTS vpn_client_profiles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vpn_account_id UUID NOT NULL UNIQUE REFERENCES vpn_accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL DEFAULT 'Default',
  client_type TEXT NOT NULL DEFAULT 'other',
  device_type TEXT NOT NULL DEFAULT 'other',
  fingerprint_mode TEXT NOT NULL DEFAULT 'auto',
  fingerprint TEXT NOT NULL DEFAULT 'firefox',
  server_name_override TEXT,
  spider_x TEXT NOT NULL DEFAULT '/',
  mtu INTEGER,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT vpn_client_profiles_fingerprint_mode_check
    CHECK (fingerprint_mode IN ('auto', 'manual')),
  CONSTRAINT vpn_client_profiles_fingerprint_check
    CHECK (fingerprint IN ('chrome', 'firefox', 'safari', 'ios', 'android', 'edge', 'random', 'randomized')),
  CONSTRAINT vpn_client_profiles_mtu_check
    CHECK (mtu IS NULL OR (mtu BETWEEN 576 AND 9000))
);

INSERT INTO vpn_client_profiles (vpn_account_id)
SELECT id
FROM vpn_accounts
ON CONFLICT (vpn_account_id) DO NOTHING;
