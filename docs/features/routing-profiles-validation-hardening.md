# RG-63 — Routing Profiles Validation & Hardening

Status: Implemented

## Summary

RG-63 hardens Routing Profiles before they become part of critical rendered routing behavior.

The feature keeps the existing Routing Profiles Admin API, Routing Profile Rules API, Admin UI, and server assignment flow, but adds stricter validation and safer conflict handling.

## Backend hardening

Routing profile payloads now validate:

- non-empty trimmed profile name
- profile name length
- description length
- duplicate profile names through a case-insensitive database index

Routing rule payloads now validate:

- non-empty trimmed rule name
- supported action: `direct`, `vpn`, or `block`
- non-negative bounded priority
- at least one matcher on create
- valid CIDR prefixes in `ipCidrs`
- domain-like values in exact domain and suffix matchers
- safe non-empty loose matcher values for keyword, GeoSite, and GeoIP lists

## Assignment and deletion behavior

Server routing profile assignment remains an upsert by `server_id`, so replacing a server assignment does not create duplicates.

Deleting a routing profile that is currently assigned to at least one server now returns `409 Conflict` instead of falling through to a database foreign key error.

Clearing a server routing profile assignment remains idempotent for existing servers.

## Database hardening

A new migration adds constraints for profile/rule names, description length, rule priority bounds, required rule matchers, and a case-insensitive unique profile name index.

## Frontend hardening

The Admin UI now shows API validation messages more clearly and prevents saving a rule without any matcher values.

The UI layout is intentionally unchanged.
