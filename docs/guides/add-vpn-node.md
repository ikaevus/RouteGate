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

## 6. Install the VPN runtime

In the node workspace open **Services** and choose **Install sing-box** for
VLESS / Reality. Confirm the installation and wait for the Agent job and next
heartbeat to report sing-box installed. An installed but stopped service is
normal on a new node: configuration apply starts it later. Do not start an
unconfigured service or install runtimes manually.

If installation fails, review the reported reason and the node's outbound
connectivity. Correct the problem and use **Install sing-box** again. This is a
runtime retry, not an Agent registration retry.

## 7. Configure Reality and prepare the account

From **Services**, follow **Configure VPN protocol** / **Protocol Settings**
for this node. Select **VLESS / Reality**. In the recommended setup, enter the
external TLS hostname and choose **Configure and make default**. RouteGate
creates the keypair and Short ID and saves the recommended TCP 8443 settings.
For an already configured node, edit the applicable fields and choose **Save
settings**; do not regenerate keys just to change the hostname.

The Reality target is an external site that resolves in DNS and accepts TLS
1.3 **from this VPN node**. It is not the public endpoint clients connect to;
clients connect to the VPN node's public IP and selected VLESS port. Do not use
the node's own hostname, an IP address, or a nonexistent name as the target.
`www.microsoft.com` is an example, not a guaranteed working default.

On the VPN node, test your chosen hostname (replace the example in both places):

```bash
getent ahosts www.microsoft.com
timeout 10 openssl s_client -connect www.microsoft.com:443 -servername www.microsoft.com -tls1_3 -brief </dev/null
```

Manager checks hostname syntax when saving. Agent repeats DNS and TLS 1.3
checks from the node during apply, before replacing the running configuration.
Manager static validation alone does not prove the target is reachable.

Follow **Create VPN account** or open **VPN Accounts** and use **Create VPN
account**. Assign it to this new node. Open the account's **Settings** and
choose **Activate**; verify its status is **Active**. Its **Protocols** workspace
must include the intended VLESS protocol. Create/activate the account **before
rendering**, so its credentials are included in the config snapshot.

## 8. Allow the selected VPN port

Review the actual VLESS port in **Protocol Settings**. For the recommended
configuration it is **TCP 8443**, not TCP 443 or UDP 8443.

Agent does not change host or provider firewall rules. Check both layers and
keep SSH access intact. For example, if UFW is already active:

```bash
sudo ufw status verbose
sudo ufw allow 8443/tcp
```

Run the allow command only after checking that this is your selected port and
UFW is the firewall you use. Do not enable UFW blindly: an incomplete ruleset
can cut off SSH. If your provider has a separate firewall, allow the same
inbound TCP port there as well. Agent itself needs outbound HTTPS to Manager,
not an inbound management port.

## 9. Render, validate, apply

Open this node's **Deployments** workspace:

1. Choose **Render config** to snapshot the saved settings and active accounts.
2. Review the selected version and validation errors/warnings. Rendering can
   already mark it **Validated**; otherwise choose **Validate**.
3. Choose **Apply** for the validated version. A queued task is not success.
4. Wait for the Agent task in **Deployment history** to become **Succeeded**.
   The page refreshes while the task on the first history page is active.
5. Verify that **Current** points to the version just applied.

Saving settings, rendering and Manager validation do not change the running
VPN or issued client parameters. Only a successful Agent apply updates them.
If you change settings or activate an account after rendering, render another
version before applying: old versions are immutable snapshots.

For validation failure, read the reported reason, correct saved settings and
render a new version. For an Agent failure, expand the deployment history row
and review its stages and reason:

- At **validate**, a Reality DNS/TLS error means this attempt was rejected
  before replacing the working config. Test the target from the VPN node,
  correct it and render/validate/apply a new version.
- For connectivity errors, restore the Agent connection and review whether the
  job completed before retrying.
- For apply/restart/healthcheck failures, inspect the reported rollback outcome
  and **Current** version before retrying; do not assume the service is healthy.

Do not repeat an old snapshot to apply corrected settings. Existing client
links keep the last successfully applied parameters. A new node/account has
no usable access link until a successful apply includes its credentials.

## 10. Get the link and test the client

Open **VPN Accounts → the account assigned to this node → Access**. Choose
**+ Add device**, name the device and select the client (for example Hiddify or
V2RayN) and platform. Create it, then use **Copy link** or its QR code to import
the subscription into that device's VPN client. Copy it before leaving the
section: a full device link is shown only when issued. If an existing device's
link is hidden and you need a new copy, use **Rotate link** in its focused
details and confirm replacement. Import the newly issued link: the old one
stops working. Treat links as credentials.

Verify that the client uses this node and the applied Reality settings, then
connect and load a test page. If the node is connected but a link is withheld,
check account activation/assignment and whether the **Current** config was
rendered after that account became active. Render and apply a fresh version if
it was not included.

### External port check and its limits

There is currently no automatic public-port check in RouteGate. From a
**different computer/network**, after successful apply, test the actual public
IP and selected TCP port. Examples using documentation-only IP `203.0.113.10`:

```bash
nc -vz -w 5 203.0.113.10 8443
```

Windows PowerShell:

```powershell
Test-NetConnection -ComputerName 203.0.113.10 -Port 8443
```

Replace the IP and port with this node's values. A check from the node itself
or a loopback CI fixture does not test the public firewall. A TCP success only
shows that something accepts connections from that source network; it does not
prove Reality authentication, account credentials, Hiddify/iPhone compatibility,
UDP reachability, or regional access to YouTube/Instagram. A timeout can result
from the host/provider firewall, a wrong IP/port, no listener, or the network
path; it does not identify which one. Client testing remains a separate step.

A possible future UI action is an explicit, bounded TCP probe from Manager to
the node's saved public IP and applied port. It would need to reject private,
loopback and special destinations, limit rate/time/concurrency, and report its
source and timestamp. Its result would describe **Manager → node** only; it
could not guarantee **client → node** reachability or diagnose UDP with a TCP
probe. This is a proposal, not an implemented API or firewall action.

## Bootstrap retry and diagnostics

If the generated command fails, wait for it to exit and use its reported stage.
For installer-stage failures, inspect the root-owned diagnostics locally:

```bash
sudo cat /var/lib/routegate-agent-installer/state.env
sudo less /var/log/routegate-agent-installer.log
```

These files are created after the installer starts. A failure downloading or
verifying the installer itself can occur before they exist; use the command's
terminal message in that case.

Before registration succeeds:

1. Return to **Connection → Connect server**.
2. Choose **Generate new token**, which invalidates the previous unused token.
3. Copy and run the new generated command on the **same target VPS**.

Never run two bootstrap commands concurrently. The command verifies the
matching artifacts again. If registration already succeeded but a later
installer stage failed, follow the terminal recovery instruction and use a
fresh command; the installer preserves its persistent identity for the same
Manager. If Agent is already online and installation completed, continue with
**Services** instead of registering it again. Diagnose a stale heartbeat through
**Connection** and the local Agent service/logs.

## Security and rebuilding

- Registration tokens are short-lived and single-use. The generated command
  contains the token; do not publish it or the full terminal/session contents.
- Do not share `/etc/routegate/agent.yaml`; it contains the persistent Agent
  credential after registration. Device subscription links are credentials too.
- Agent initiates HTTPS connections to Manager and executes only allow-listed
  management operations; it exposes no general inbound remote-shell port.
- When rebuilding or moving to another VPS, generate a fresh command. Do not
  reuse old build metadata, artifact locations or a consumed token.

The isolated CI workflow is documented in
[remote-node-onboarding-e2e.md](../operations/remote-node-onboarding-e2e.md).
It verifies bootstrap, runtime installation, real VLESS/Reality traffic and
NXDOMAIN rejection. It does not replace the public-port and real-client checks
above.
