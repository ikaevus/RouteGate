# Traffic accounting checkpoint — 2026-10-31

## Completed

- PR #669 merged as 36af753e490e019df196e7aa5f4eeeb9db7caf9d. Feature head 58485650c42a394ae1305fdff1a7896e2f3c1b93 passed RouteGate CI 41137852704 and Workspace integration 41137852705.
- Native sing-box per-account counters, durable Agent outbox, transactional Manager stream deduplication, migration 160, and Dashboard snapshot validation are implemented.
- Real disposable VLESS transfer, lost-acknowledgement/restart, concurrent retry, raw-retention, and Dashboard tests passed. Changed Agent paths passed govulncheck with Go 1.26.10 and patched dependencies.
- PR #670 merged as 2c34c4a966dda8eed612585c4dadff112c2ea73d: scoped rollout helper and recovery tests.
- PR #671 merged as 7c2769b67c12e412c622281bff40d9857166e09f: PostgreSQL URI decoding, verified by a real read-only PostgreSQL test in CI.

## Live state

No Manager/Agent installation or VPN config apply has been dispatched. US remains on schema 159. Read-only diagnostics run 41136740772 succeeded with healthy services. The first scoped inspect run 41140354048 failed before mutation because libpq does not expand a URI in the PGDATABASE environment variable; PR #671 fixes this. FI/RU were not changed.

## Rollout blocker and safe continuation

A local build of a real trimmed Manager binary showed that linker flags are not retained as searchable build-metadata strings. Therefore bootstrap_metadata() cannot safely recover old identities using its current flag regex.

More importantly, preserving only the old bootstrap URL and installer digest while changing Manager Version/GitCommit is not sufficient: the generated onboarding command uses Manager GitCommit to select install-agent.sh and Manager Version to select bundle filenames. Mixed identities could break future node onboarding.

Install/apply modes are disabled in both the workflow choices and the runner entry point until this is corrected. Do not bypass the guard.

Prefer the normal release/bootstrap publication path for a tested main commit, then install Manager/Agent from that exact published, verified bundle with matching Version, GitCommit, installer checksum, and bundle base URL. The feature merge 36af753e490e019df196e7aa5f4eeeb9db7caf9d has successful main push CI 41138307042 and Workspace integration 41138306976. Preserve existing published artifacts. Use private random staging when invoking publication helpers.

An alternative is to explicitly separate Manager build identity from bootstrap bundle identity, with tests; do not misreport Manager GitCommit or guess old values.

After fixing and testing the rollout: repeat read-only US inspection, publish/verify matching artifacts as needed, install the control plane with recovery, then normal render/validate/apply only if runtime payloads differ solely in v2ray_api and the active config has no drift. A normal multi-protocol apply may briefly interrupt US client sessions. Verify actual native reports and Dashboard daily/monthly availability; never fabricate historical traffic.

The local execution environment and browser became unavailable during preparation. Repository changes and this checkpoint are durable; live deployment remains unfinished.
