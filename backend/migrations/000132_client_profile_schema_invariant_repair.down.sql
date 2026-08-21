-- Intentionally irreversible repair.
-- The repaired protocol column, validation constraint, uniqueness invariant,
-- and dirty-state trigger are owned by earlier migrations and are required by
-- current client-profile code. Rolling this migration back must not reintroduce
-- historical schema drift.
SELECT 1;
