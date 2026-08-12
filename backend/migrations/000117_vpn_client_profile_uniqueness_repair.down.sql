-- Irreversible safety repair.
--
-- Migration 117 removes duplicate client profiles and restores the uniqueness
-- invariant required by GetOrCreateClientProfile. Reintroducing the historical
-- drift on rollback would make valid VPN access fail again, and deleted duplicate
-- rows cannot be reconstructed safely.
SELECT 1;
