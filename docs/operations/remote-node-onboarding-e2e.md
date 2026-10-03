# Isolated new-node onboarding E2E

The `remote-node-bootstrap` job in RouteGate CI runs only on a disposable
GitHub-hosted Ubuntu runner with its own PostgreSQL 16 service. It has no
production environment or production secrets. It never calls US Manager,
RU, or FI. **Do not run its shell script on any real host:** it replaces APT
sources, installs a test CA, Agent and sing-box, and creates system services.
Explicit isolation markers, loopback endpoints, the actual dedicated database
name, and a fresh runner work directory are checked before host changes.

The test uses the exact checked-out commit's release bundle and the real
Manager-generated registration command. It covers:

1. Corrupt bundle checksums fail before registration; a fresh command retries.
2. Installation consumes the registration token, persists the Agent token,
   and produces a heartbeat without silently installing VPN runtimes.
3. Manager requests sing-box installation through the real Agent task API.
4. Recommended Reality settings are saved using a local TLS 1.3 handshake
   target on port 443. An active account has no link before first application.
5. Render, Manager validation, and Agent application succeed. A real sing-box
   client is built from the served VLESS link, without substituting endpoint,
   SNI, public key, or Short ID. A fresh HTTP request traverses its SOCKS
   listener and VLESS/Reality to the test origin. Curl cannot bypass SOCKS.
6. Saving an NXDOMAIN Reality target leaves the served link unchanged.
   Manager syntax validation succeeds, but Agent application fails at
   `validate`. The active config hash, service PID/start time/restart count,
   client link, and a second real traffic request remain unchanged/working.

The local target avoids dependencies on third-party TLS sites. This verifies
onboarding and data-plane correctness, **not** public firewall reachability,
iPhone/Hiddify compatibility, or regional access to YouTube/Instagram.

API responses, bootstrap commands, client links, config files, keys, and
runtime logs remain private on the ephemeral runner and are not uploaded.
CI prints only fixed milestone messages and sanitized failure descriptions.
The continuation stops its client and TLS/origin fixtures on exit; bootstrap
cleanup stops its Manager, proxy, and Agent. The hosted runner is then discarded.

Safe local checks (no installation):

```bash
bash -n scripts/remote-node-bootstrap-e2e.sh
python3 -m unittest scripts/test_remote_node_vpn_e2e.py
```

Only the real CI job establishes that traffic and config preservation work.
The unit tests alone are not an end-to-end success result.

## UI and manual acceptance

The frontend browser regression test (`npm run test:node-onboarding-browser`)
uses mocked Manager responses to check the real UI/router: installer retry
guidance, a stopped runtime before first apply, displayed validation reasons,
queued application polling, failure recovery and successful current-version
refresh. It does not execute an installer or establish VPN traffic.

Follow [Add a remote VPN Node](../guides/add-vpn-node.md) for public firewall
and real-client acceptance. Run the manual check only on an explicitly
authorized test node: check the selected public port from another network,
import its device link into the actual client, connect, and record the result.
Do not claim success for Hiddify/iPhone or regional service reachability from
CI alone.
