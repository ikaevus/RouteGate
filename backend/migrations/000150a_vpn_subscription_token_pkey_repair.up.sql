-- Repair a production-like schema drift exposed by RG-116.
--
-- The first RG-116 production-like rollback used pg_restore --clean directly
-- against a database that had already gained newer FK-bearing objects. The
-- restore aborted while dropping vpn_accounts_pkey, but pg_restore may have
-- already removed earlier constraints from the pre-deploy schema. Migration
-- 000151 adds a foreign key from deliveries.subscription_token_id to
-- vpn_subscription_tokens(id), so that referenced column must still have its
-- canonical primary-key uniqueness.
--
-- This repair sorts after 000150 and before 000151. Clean installations are a
-- no-op. If the canonical primary key is missing, restore it only after
-- verifying that no different primary key is present and that id contains no
-- NULL/duplicate values. Fail rather than guessing if the table is in a more
-- seriously divergent state.
DO $$
DECLARE
    id_attnum SMALLINT;
    primary_key_columns SMALLINT[];
BEGIN
    SELECT attnum::SMALLINT
      INTO id_attnum
      FROM pg_attribute
     WHERE attrelid = 'vpn_subscription_tokens'::regclass
       AND attname = 'id'
       AND NOT attisdropped;

    IF id_attnum IS NULL THEN
        RAISE EXCEPTION 'vpn_subscription_tokens.id is missing; cannot repair primary key';
    END IF;

    SELECT conkey
      INTO primary_key_columns
      FROM pg_constraint
     WHERE conrelid = 'vpn_subscription_tokens'::regclass
       AND contype = 'p'
     LIMIT 1;

    IF primary_key_columns = ARRAY[id_attnum]::SMALLINT[] THEN
        RETURN;
    END IF;

    IF primary_key_columns IS NOT NULL THEN
        RAISE EXCEPTION 'vpn_subscription_tokens has an unexpected primary key; refusing automatic repair';
    END IF;

    IF EXISTS (SELECT 1 FROM vpn_subscription_tokens WHERE id IS NULL) THEN
        RAISE EXCEPTION 'vpn_subscription_tokens.id contains NULL values; refusing primary-key repair';
    END IF;

    IF EXISTS (
        SELECT id
          FROM vpn_subscription_tokens
         GROUP BY id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'vpn_subscription_tokens.id contains duplicate values; refusing primary-key repair';
    END IF;

    ALTER TABLE vpn_subscription_tokens
        ADD CONSTRAINT vpn_subscription_tokens_pkey PRIMARY KEY (id);
END
$$;
