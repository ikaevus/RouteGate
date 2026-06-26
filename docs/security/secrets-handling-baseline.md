# Secrets Handling Baseline

RouteGate treats secret handling as a product-level security boundary.

This document defines the first baseline for classifying, masking, redacting, and exposing sensitive values.

## Classification

### Public

Public values can be returned through API responses when needed.

Examples:

- Reality public key
- server name
- server hostname
- server public IP
- routing profile name
- config version ID

### Sensitive

Sensitive values are not full secrets, but should still be handled carefully.

Examples:

- masked token previews
- last four characters of a token
- audit metadata that helps identify an action without exposing a secret

Allowed example:

```json
{
  "token_preview": "rgsub_abcd...wxyz"
}
```

### Secret

Secret values must not be logged, stored in audit metadata, or returned from API responses unless the endpoint is explicitly designed to return the value once.

Examples:

- passwords
- password hashes
- JWT/session tokens
- Agent bearer tokens
- Agent registration tokens
- subscription tokens
- subscription URLs
- token hashes
- Reality private keys
- credentials
- private keys of any kind

## Rules

1. Store only hashes for bearer-style tokens whenever possible.
2. Return raw tokens only once at creation/registration time.
3. Never put raw tokens or private keys in audit metadata.
4. Use masked previews for audit metadata and UI hints.
5. Treat public/private key pairs explicitly:
   - public key may be returned when needed;
   - private key must stay server-side.
6. Do not include generated configs in audit metadata if they may contain credentials.
7. Do not add generic debug logging for request/response bodies.
8. API DTOs should be reviewed before adding fields with names containing:
   - password
   - token
   - secret
   - privateKey/private_key
   - credential
   - hash

## Shared backend helpers

The shared backend helpers live in:

```text
backend/internal/secrets
```

Current helpers:

- `Mask(value string)`
- `SanitizeMetadata(metadata map[string]any)`
- `ClassifyKey(key string)`
- `IsSecretKey(key string)`

Existing token-specific helpers should delegate to the shared helpers where practical:

- `audit.SanitizeMetadata`
- `audit.MaskSecret`
- `vpnaccounts.MaskSubscriptionToken`
- `agents.MaskToken`

## Current intentional one-time exposures

The following raw values may be returned once because the caller needs to copy/store them:

- subscription token after creation/rotation;
- Agent bearer token after successful Agent registration;
- server registration token after admin-generated registration token creation.

These raw values must not be stored in audit metadata or logs.

## Future work

This baseline does not implement encryption-at-rest yet.

Future hardening can add:

- encrypted secret storage;
- key rotation workflows;
- stronger DTO leak tests;
- centralized secret field schema;
- production log scanning checks;
- signed update channel secret handling.
