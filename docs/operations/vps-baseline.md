# VPS baseline and recovery checklist

This is an **operator runbook**, not an installer requirement or a copy-paste
hardening script. Validated with a real remote VPN Node after reinstalling
Ubuntu 24.04.5 LTS on a HostKey VPS (RG-139, 2026-10-05). Distribution and
provider defaults vary; verify before changing access paths.

## Scope and boundaries

- **VPN Node** runs RouteGate Agent and managed VPN runtimes. It does **not**
  host Manager, Admin UI, PostgreSQL, or Manager nginx.
- The existing Manager is independent; do not open inbound Agent ports on the
  VPN Node. Agent initiates outbound HTTPS.
- This runbook does not apply a VPN config. Always render/validate/apply through
  RouteGate and verify Agent jobs; CLI manual edits to managed sing-box config
  will drift from Manager.
- Never commit bootstrap commands, Agent credentials, SSH private keys,
  plaintext subscriptions, device QR codes, tokens, or private Reality keys.

## 1. Clean OS and network baseline

1. Reinstall a supported Ubuntu LTS image from your selected VPS provider.
   Expect a **new SSH host key** after reprovisioning; verify its fingerprint
   out of band before trusting it.
2. Confirm supported architecture and OS:
   `lsb_release -a`, `uname -m`, `uname -r`, `df -h`, `free -h`.
3. Determine **which network manager owns the public interface**:
   `ip -br addr`, `ip route`, `networkctl status <interface>`,
   `nmcli device status` (if installed). Never enable/disable a manager based
   on the mere presence of its systemd unit.
4. Verify DHCP/static address, gateway, DNS, external HTTPS, and that these
   survive a reboot. An idle or unowned `systemd-networkd-wait-online` can
   time out even when NetworkManager manages the link; disable that specific
   obsolete wait unit only **after proving ownership and connectivity**.
5. Check `systemctl --failed`, `journalctl -b -p warning`, and investigate
   blocking errors before installing Agent. Virtual-hardware warnings must be
   judged against real device/service health; don't hide them indiscriminately.

### HostKey FI observation (not a universal default)

For the verified FI rebuild, `ens1` was controlled by NetworkManager, while
`systemd-networkd-wait-online.service` was waiting on an unmanaged interface.
Disabling **only** that wait-online service corrected the boot timeout.
DNS and routing were independently checked before and after reboot.

## 2. Administrative access and SSH

1. Create a named sudo-capable user and verify its sudo privileges.
2. Add a locally generated Ed25519 public key to the user's
   `~/.ssh/authorized_keys` (directory `700`, file `600`).
3. **Open and test a second SSH session using that key** before modifying
   password/root access. Keep a proven recovery path/console available.
4. Place intentional SSH policy in a small drop-in under
   `/etc/ssh/sshd_config.d/`, for example:

   ```text
   PermitRootLogin no
   PasswordAuthentication no
   PubkeyAuthentication yes
   KbdInteractiveAuthentication no
   X11Forwarding no
   ```

5. Validate `sshd -t` and effective `sshd -T -C ...` for both the named
   user and root. Reload rather than blind restart, then verify a new SSH
   session **before closing the old one**.

Do not disable root/password login until the public-key path is confirmed
working with the intended client (MobaXterm, OpenSSH, etc.).

## 3. Firewall and exposure

Use the approved firewall implementation for the installation
(`ufw` on the validated Ubuntu image). Establish rules **before** enabling
default-deny inbound; avoid remote SSH lockout.

- Inbound default: deny. Outbound default: allow unless explicitly designed
  otherwise.
- `22/tcp`: administration, restrict to known source IPs where feasible.
- VLESS/Reality: **the configured listener TCP port** (e.g. `443/tcp` on FI),
  not always `8443`. A **standalone VPN Node** does not inherently reserve
  443 for Manager HTTPS. A Hybrid Node may have a real collision.
- Hysteria2, WireGuard, MTProto, ACME HTTP-01 or other services require their
  own explicitly validated UDP/TCP/firewall/DNS/TLS rules. Never open extra
  inbound ports just in case.
- Check both host firewall and any provider security groups. After reboot,
  verify active rules, IPv4/IPv6 policy, and listener with
  `ss -lntup`. External reachability tests must note whether the probe
  itself uses another VPN.

## 4. Updates and operating-system policy

- Record whether APT periodic package lists/unattended upgrades are actually
  enabled, rather than assuming them from installed packages or timers.
- Choose either documented, monitored security updates or an explicit
  planned/manual patch window and assign an owner. Disabled unattended
  upgrades without an alternative schedule are a **remaining security debt**.
- Do not automatically accept a new major Ubuntu LTS upgrade just because the
  login banner offers it. Treat upgrades and kernel reboots as separately
  reviewed changes with backup/snapshot and a recovery plan.

## 5. RouteGate onboarding

1. Register the real VPN Node in Manager or re-use the existing inventory
   entry for a rebuilt machine; **avoid creating a duplicate FI node**.
2. Generate a **new single-use, time-limited** Agent bootstrap command.
   Verify expected source/artifact checksum trust and do not publish it.
3. Run the exact generated command through the proven sudo path and wait for
   terminal success. Check `systemctl status routegate-agent`, its registration
   and accepted heartbeats. Agent communication is outbound.
4. Install the required VPN runtime using Manager's supported Agent workflow.
   Configure VLESS/Reality or other supported protocol on the node.
5. Create or assign a **test account** before the first successful apply.
   Use `Render → Validate → Apply`, wait for the terminal Agent result, and
   verify the current applied version. Never send saved-but-unapplied settings.
6. Check `systemctl is-active sing-box`, `systemctl is-enabled sing-box`,
   the configured TCP/UDP listener, firewall, and
   `systemctl --failed`. A successful static render is not runtime proof.
7. Only after apply, issue a **device-scoped** subscription for a test client.
   Treat the link as a bearer secret, and avoid putting it in screenshots.

## 6. Acceptance and restart test

- [ ] SSH key access works; prohibited root/password access verified.
- [ ] Network and DNS stable; zero unexplained failed boot units.
- [ ] UFW (or approved firewall) active; exact intended rules survive reboot.
- [ ] Agent active/enabled; heartbeats accepted.
- [ ] Runtime active/enabled and the expected VPN listener reachable.
- [ ] Manager shows a **succeeded**, current applied configuration version.
- [ ] Real Android/iOS clients can connect, browse, and prove correct **exit
      public IP** (rather than just showing a locally connected tunnel).
- [ ] VPS reboot and **subsequent real VPN client traffic** succeed without
      re-applying the configuration or re-importing the existing subscription.
- [ ] Rollback/recovery path and patching policy documented.

## Follow-up product/operations debt from RG-139

- [RG-140 subscription continuity and safe migration](https://github.com/ikaevus/RouteGate/issues/509):
  moving US ↔ FI must preserve issued links, but manual assignment and
  deployment are not yet a zero-interruption migration operation.
- [RG-139 technical closeout / remaining debt](https://github.com/ikaevus/RouteGate/issues/510):
  node deployment action guards, topology-aware protocol warnings, redacted
  config review, secure link rotation UX, patch policy and extra external tests.

**No production credentials or exact one-time bootstrap material belong in
this public runbook.**
