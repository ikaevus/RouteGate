-- Only remove the canonical primary key if this repair is being rolled back.
-- On a clean installation 000150a was a no-op, but DROP CONSTRAINT IF EXISTS
-- would still remove the long-standing primary key. Therefore the down path
-- deliberately does nothing: the repaired primary key is part of the
-- pre-existing canonical schema and must remain present when rolling back
-- later RG-116 migrations.
SELECT 1;
