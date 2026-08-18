# ADR-0003: VPN Core Adapter Boundary

- Status: Accepted
- Date: 2026-08-18
- Workstream: RG-114C

## Context

RouteGate already has the correct deployment trust boundary: Manager produces
desired state, Agent performs allow-listed host operations, and the VPN Core
owns the data plane. The implementation nevertheless specialized both sides of
that boundary for sing-box and VLESS/Reality:

- Manager rendered sing-box inbounds, routes, and Reality settings inside its
  general Config Service;
- Manager's general validation and apply-readiness checks inspected VLESS
  fields directly;
- Agent's task loop selected sing-box staging, validation, service control, and
  VLESS listener checks directly;
- Agent capability reporting duplicated the managed protocol combination.

Adding more protocols through these branches would mix protocol, transport,
security, and host-operation concerns and make capability claims prone to
drift.

## Decision

### Manager owns a protocol-aware adapter registry

The Manager Config Service retains the `routegate.config.v1` envelope and its
generic server, Agent, routing metadata, versioning, hashing, and audit flow.
VPN Core-specific behavior is delegated to a registered adapter with four
separate descriptor dimensions:

| Dimension | Current value |
| --- | --- |
| VPN Core | `sing-box` |
| Protocol | `vless` |
| Transport | `tcp` |
| Security | `none`, `reality` |

The adapter owns VPN account projection, VPN Core rendering, protocol-specific
validation, routing translation, and apply-readiness evaluation. Registry
resolution succeeds only for a fully declared composition.

### Agent applies through the matching adapter boundary

The Agent task loop invokes a VPN Core adapter for:

- extracting and staging the VPN Core payload;
- running the VPN Core's native config validation;
- restart, active-state, and persistence checks;
- protocol listener health.

Atomic promotion, backup, rollback, task authentication, and result reporting
remain shared Agent behavior. The initial Agent adapter delegates to the
existing hardened sing-box/VLESS implementations.

The Agent's advertised `capabilities.routegate.vpnCoreAdapters` are built from
its managed adapter descriptors. Detecting an installed binary does not create
a managed adapter capability.

### Compatibility is an invariant

RG-114C does not change:

- the `routegate.config.v1` JSON shape or schema version;
- existing sing-box field names or rendering semantics;
- Manager APIs, persistence schema, or config version hashing;
- Agent configuration keys and default paths;
- backward compatibility with plain sing-box task payloads.

Contract tests pin the current envelope and the one supported adapter
composition.

## Consequences

- New protocols can add adapters without branching the general Config Service
  or Agent task loop.
- Protocol, transport, and security compatibility is explicit rather than
  inferred from VPN Core binaries.
- Shared deployment safety remains centralized while protocol health checks can
  differ.
- Client credential/profile delivery is still VLESS-specific and must move
  behind protocol adapters when the first additional protocol is implemented.
- `routegate.config.v1` remains specialized enough to contain `singBox`; a
  future envelope version may generalize payload storage, but is not required
  for this boundary.

## Follow-up

1. Add certificate lifecycle and recovery operations without weakening the
   adapter boundary.
2. Implement WireGuard as the first additional end-to-end adapter, including
   credentials and client delivery.
3. Version the config envelope only when a new adapter cannot be represented
   compatibly; do not change it merely to rename current fields.
