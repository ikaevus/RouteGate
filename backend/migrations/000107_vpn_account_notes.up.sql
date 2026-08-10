CREATE TABLE vpn_account_notes (
    vpn_account_id UUID PRIMARY KEY REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    notes TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT vpn_account_notes_length CHECK (char_length(notes) <= 4000)
);

CREATE INDEX idx_vpn_account_notes_notes_trgm
    ON vpn_account_notes USING gin (notes gin_trgm_ops);
