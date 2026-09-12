# RG-116 stabilization status

Current branch status: partial safety fix under review. Migrations 000150 and 000151 now keep their physical rollback and migration-history removal in one PostgreSQL transaction. Migration 000149 and the full production-like failure/recovery validation remain blockers.
