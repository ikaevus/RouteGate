-- Intentionally non-destructive.
--
-- The uniqueness invariant and dirty-state trigger restored by 000150b are
-- canonical invariants owned by earlier migrations. Rolling this repair back
-- must not deliberately reintroduce physical schema drift.
SELECT 1;
