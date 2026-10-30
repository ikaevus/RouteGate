# Scoped US traffic accounting rollout

Candidate: `58485650c42a394ae1305fdff1a7896e2f3c1b93` (PR #669).
Exact candidate CI: RouteGate CI `41137852704`, Workspace integration `41137852705`.

The manual **Traffic accounting scoped US rollout** workflow runs only from
trusted main, under the existing production-like environment and deployment
lock. It uses the existing SSH identity and verifies both candidate CI runs.
It does not target FI or RU and does not publish bootstrap artifacts.

1. `inspect`: read-only schema/unfinished transfer and apply-job checks,
   installed sing-box build-tag verification, active-config statistics flags,
   and availability of the loopback statistics port. Output contains booleans
   and schema version, never tokens, credentials, or runtime config contents.
   It also reports the existing public bootstrap base URL and installer SHA256
   from Manager build metadata. Supply these exact public values as the
   `bootstrap_base_url` and `installer_sha256` inputs for install/apply. The new
   Manager preserves them, and installation refuses any mismatch, so onboarding
   is not redirected to unpublished artifacts or changed by this repair.
2. `install`: reproducibly build the pinned Manager and Agent with Go 1.26.10;
   retain root-only backups of their previous binaries and Agent config;
   add migration 160; enable Agent collection; restart only Manager and Agent.
   Verify Manager health/schema and unchanged VPN/nginx files and processes.
   This phase does not apply a VPN config or restart VPN services.
3. `apply`: verify installed binaries match the candidate payload; wait for a
   compatible authenticated Agent heartbeat; authenticate to the local Manager
   using the existing bootstrap admin credential without printing it. Render
   and validate through the normal Manager API. Compare all rendered runtime
   payloads with the previous applied version, ignoring only `v2ray_api`, and
   compare the active sing-box config as well. Any other change or drift stops
   before applying. Apply through the ordinary Manager job, then verify native
   Agent reports and the Dashboard's daily/monthly availability.

Applying the normal multi-protocol job can briefly interrupt US VPN client
sessions. No account credentials, routes, protocol selections, or FI/RU node
configurations are changed by this rollout. If the stored administrator
credential is unavailable or no longer valid, the workflow stops; use the
authenticated admin UI for the normal render/validate/apply flow.

Installation failures restore the previous binaries and Agent config. The
additive checkpoint migration is retained if Manager startup applied it;
rolling back application files must never erase newly reported traffic or
checkpoint high-water marks. The old Manager can start with this extra table,
but its update-preflight expected schema can report a mismatch until the new
candidate is reinstalled. Backups are retained for inspection and recovery.

If a runtime apply fails or its outcome is unresolved, the helper does not
enqueue a competing second job. Inspect the normal config job and use existing
config recovery in the admin UI. If applying succeeds but there is no client
traffic during the observation window, the workflow reports that it is waiting
for traffic; historical usage is never fabricated.
