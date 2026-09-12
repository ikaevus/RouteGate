# RG-116 stabilization note

This note is authoritative for the post-merge stabilization pass until the older `access-devices.md` wording is reconciled.

## Credential migration semantics

A pre-RG-116 active account subscription remains a legacy account credential with `device_id IS NULL`. Migration `000149_vpn_account_devices` creates a visible `Default device`, but does not attach the historical token to that device. The Default device starts without a device-scoped access token.

## Device PublicURL policy

New per-device access links require a valid canonical HTTPS `ROUTEGATE_PUBLIC_URL`. Device creation or rotation fails before mutating access state when that setting is missing or invalid. There is no request-host or forwarded-header fallback for device links. The legacy account-level subscription path keeps its older request-derived behavior for compatibility.

## Rollback stabilization

The RG-116 rollback chain is being hardened so physical schema rollback and migration-history rollback cannot drift apart. Migrations `000150` and `000151` now own explicit PostgreSQL transactions that include removal of their own `schema_migrations` rows. `000149` already owns a transaction for its physical rollback, but its migration-history coupling remains tracked by issue #423 and must be resolved before the stabilization hold is lifted.

## Product hold

Do not mix new RG-116 feature work into the stabilization branch. In particular, `max_devices` semantics remain an explicit product decision rather than an inferred quota rule.
