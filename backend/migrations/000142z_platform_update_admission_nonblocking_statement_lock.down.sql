CREATE OR REPLACE FUNCTION lock_platform_update_admission_global_statement()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM lock_platform_update_admission_global();
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_update_lock_order()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_server_id TEXT;
    transaction_rollout_id TEXT;
BEGIN
    PERFORM lock_platform_update_admission_global();

    previous_server_id := current_setting('routegate.platform_update_admission_last_server_id', true);
    transaction_rollout_id := current_setting('routegate.platform_update_admission_rollout_id', true);
    IF previous_server_id IS NOT NULL
        AND previous_server_id <> ''
        AND (transaction_rollout_id IS NULL OR transaction_rollout_id = '') THEN
        RAISE EXCEPTION 'platform update rollout parent must be established before binding update after server admission lock';
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION lock_platform_update_rollout_parent_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM lock_platform_update_admission_global();
    RETURN NULL;
END;
$$;

DROP FUNCTION IF EXISTS try_lock_platform_update_admission_global();
