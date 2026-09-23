-- Intentionally non-destructive.
--
-- The triggers and functions restored by 000155 are canonical invariants owned
-- by 000105 and 000133. Rolling this repair back must not deliberately
-- reintroduce physical schema drift.
SELECT 1;
