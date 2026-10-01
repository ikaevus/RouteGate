-- Distinguish protocol preferences an administrator chose from rows Manager
-- seeds automatically (from the applied version, the account's primary
-- protocol or a successful apply). Only explicit choices decide what the next
-- render deploys; an account without explicit choices deploys its primary
-- protocol (the node default for "auto"), so switching the node protocol can no
-- longer be silently held back by a seeded row of the previous protocol, such
-- as a seeded MTProto row keeping the node-wide MTProto proxy running.
--
-- Existing rows cannot be told apart and are kept as explicit, so current
-- renders do not change. Rows inserted without the column (seeds and the apply
-- trigger) are implicit.
ALTER TABLE vpn_account_protocols
    ADD COLUMN IF NOT EXISTS desired_explicit BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE vpn_account_protocols
    ALTER COLUMN desired_explicit SET DEFAULT FALSE;

COMMENT ON COLUMN vpn_account_protocols.desired_explicit IS
    'TRUE when desired_enabled was set by an administrator; seeded rows are FALSE and do not decide the render.';
