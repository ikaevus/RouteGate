# Add a remote VPN Node

This guide is for an administrator who already has a working RouteGate Manager or Hybrid Node and wants to attach another VPS as a VPN server.

The remote VPS does **not** receive PostgreSQL, Manager, Admin UI, or nginx. The onboarding command installs and registers RouteGate Agent. VPN protocol runtimes are installed later by RouteGate when they are needed.

## Before you start

Prepare:

- a clean Ubuntu 24.04 LTS VPS on amd64 or arm64;
- root access or a user with working `sudo`;
- outbound HTTPS access from the VPS to your RouteGate Manager and RouteGate release sources;
- the VPS public IP address;
- an open SSH session to the new VPS.

You do not need to manually install RouteGate Agent, WireGuard, Hysteria, sing-box, or mtg.

## 1. Create the server in RouteGate

In the Admin UI open:

```text
Servers → Add server
```

Enter the server name, public IP address, location/description if useful, and choose:

```text
Role: VPN Node
```

Do not choose **Management Node** unless the server is intended to host the RouteGate management plane.

### Expected result

The new server is created with a state equivalent to:

```text
Waiting for Agent
```

## 2. Open Connect server

Open the new server and go to:

```text
Connection → Connect server
```

RouteGate creates one short-lived registration token for this onboarding session and displays a generated installation command.

The generated command is the primary onboarding path. It already contains the one-time registration token. The raw token and manual Agent configuration are available under technical details for exceptional recovery; they are the same credential, not another registration mechanism. **Generate new token** is an explicit rotation action and invalidates the previous unused token.

## 3. Copy the generated command

Use **Copy installation command**.

Do not manually reconstruct the command and do not copy only the token or the example Agent configuration.

The generated command is tied to the Manager build and verifies the installer before privileged execution.

## 4. Run the command on the new VPS

Paste the generated command into the SSH session on the target VPS and run it exactly as generated.

The command contains `sudo`, so you do not need to switch to an interactive root shell first.

### Expected result

The bootstrap should:

```text
check the supported host
→ check the configured APT repository boundary
→ download and verify the matching RouteGate bundle
→ install RouteGate Agent
→ start Agent
→ exchange the one-time token
→ establish heartbeat
→ prepare the trusted updater boundary
```

It should **not** install VPN protocol runtimes during this step.

## 5. Confirm that the server is connected

Keep the onboarding dialog open. RouteGate checks for the new Agent automatically.

### Success looks like

- the server changes from **Waiting for Agent** to connected/online;
- an Agent identity/version appears;
- a recent heartbeat is visible;
- the onboarding step reports success.

At this point server onboarding is complete.

## 6. Configure the VPN protocol

Choose the protocol you want to use on this server.

RouteGate installs the required runtime through Agent as the next managed action, then continues through configuration render, validation, apply, and health checking.

For VLESS / Reality, enter an external HTTPS hostname that resolves and accepts a TLS handshake **from this VPN node**. The node's own hostname or IP is not a suitable automatic Reality handshake target. RouteGate checks the name's syntax, but it cannot infer reachability from the Manager. For example, on the VPN node test a candidate with `getent ahostsv4 www.microsoft.com` and `timeout 10 openssl s_client -connect www.microsoft.com:443 -servername www.microsoft.com -brief </dev/null`. Choose a site that works from this node and network.

After saving protocol settings, open the server's **Deployments** workspace. Render a new version, review the validation result (rendering can already mark it validated), apply the validated version, and check the Agent deployment result. Saving settings alone does not change the running VPN service. For VLESS on TCP 8443, allow inbound TCP 8443 in the VPS host firewall and any separate provider firewall; keep SSH access intact. If UFW is active, `sudo ufw allow 8443/tcp` opens that host port; verify the firewall's actual state first. The remote Agent needs outbound HTTPS to Manager, not an inbound management port. Check the selected port from outside the VPS after applying. Other protocols use the port and transport shown in their settings.

This separation is intentional:

```text
Connect server
→ server connected
→ install required VPN runtime
→ deploy protocol configuration
```

A temporary upstream problem downloading Hysteria, mtg, sing-box, or WireGuard packages should therefore fail the runtime step without making Agent onboarding look broken.

## Retry and diagnostics

If bootstrap fails, do not guess which parts were installed.

The remote installer records:

```text
State: /var/lib/routegate-agent-installer/state.env
Log:   /var/log/routegate-agent-installer.log
```

Use the failed stage shown by the installer.

The supported retry path is:

1. Return to **Connect server**.
2. Choose **Generate new token** if RouteGate asks for a new onboarding command.
3. Copy the newly generated command.
4. Run the new command on the same VPS.

If Agent already registered successfully, its persistent identity for the same Manager is preserved.

## Important security notes

- Registration tokens are short-lived and single-use.
- Do not post a full generated command publicly; it contains the registration token.
- Do not share `/etc/routegate/agent.yaml`; after registration it contains the persistent Agent credential.
- Agent initiates HTTPS connections to Manager. It does not expose a general remote-shell management port.
- RouteGate Agent only accepts the product's allow-listed management operations.

## Removing or rebuilding a node

Do not reuse an old bootstrap command when rebuilding a VPS or moving a node to another host. Generate a fresh onboarding command from RouteGate so build identity, bundle source, and registration token are current.
