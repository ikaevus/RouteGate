# ADR-0001: Use monorepo for RouteGate Foundation

## Status

Accepted for Foundation v0.1.

## Context

RouteGate includes Manager, Agent, Admin UI, User Portal, deployment files and operational documentation. Component boundaries are still evolving.

## Decision

Use one repository with top-level directories:

- `backend/`
- `agent/`
- `frontend/`
- `deploy/`
- `docs/`
- `scripts/`

## Consequences

Positive:

- simpler initial development;
- easier cross-component changes;
- one place for documentation and decisions;
- fewer repository management costs.

Negative:

- repository may grow large later;
- release boundaries must be disciplined.

This can be revisited after MVP.
