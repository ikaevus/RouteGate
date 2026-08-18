# backend/internal/servers

The servers module is the compatibility API and persistence surface for
RouteGate's managed node inventory.

RG-114 adds explicit `management`, `vpn`, and `hybrid` deployment roles. The
resource remains named `servers` while UI and API terminology migrate toward
nodes without a breaking route or table rename. Deployment role records intent;
Agent-reported capabilities record what a connected host can manage now.
