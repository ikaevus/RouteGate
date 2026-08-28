-- Raw SQL may have retained rollout/entry row locks from an earlier statement
-- before it reaches one of the admission DML triggers. A blocking attempt to
-- acquire the global admission mutex at that point can deadlock with Manager,
-- which correctly owns the mutex before waiting for those rows. Statement-level
-- guards therefore acquire the mutex fail-fast: if another admission transaction
-- owns it, abort this raw statement/transaction so its pre-existing row locks are
-- released instead of waiting while holding them.
CREATE OR REPLACE FUNCTION try_lock_platform_update_admission_global()
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT pg_try_advisory_xact_lock(722096142::bigint) THEN
        RAISE EXCEPTION 'platform update admission global mutex is busy; retry the transaction from its admission boundary';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION lock_platform_update_admission_global_statement()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM try_lock_platform_update_admission_global();
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
    PERFORM try_lock_platform_update_admission_global();

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
    PERFORM try_lock_platform_update_admission_global();
    RETURN NULL;
END;
$$;
