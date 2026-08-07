# RG-89C — Clean-host VPN Core installation E2E

## Status

- Prepared for execution on `us.routegate.org`
- Target host: disposable Ubuntu 24.04 LTS, amd64
- Source of truth: GitHub `main` plus the active RG-89C branch
- Finland personal VPN infrastructure is explicitly out of scope

## Purpose

Validate the implemented RouteGate flow on a genuinely clean production-like VPS:

```text
RouteGate Manager
→ registered RouteGate Agent
→ install_sing_box task
→ allow-listed Agent installer
→ official sing-box APT package
→ systemd detection
→ next heartbeat reports installed state
```

This runbook intentionally separates three concerns:

1. **RG-89C:** validate the already implemented sing-box installation path.
2. **Manual deployment debt:** temporarily bring up the current Manager, UI, PostgreSQL, nginx, and Agent without pretending a one-command installer already exists.
3. **Future Clean VPS Installer:** automate the verified procedure in a separate workstream.

## Safety boundary

`us.routegate.org` is a disposable validation host and may be rebuilt repeatedly.

Do not use this procedure on the Finland personal VPN VPS.

Never publish or commit:

- database passwords;
- bootstrap administrator passwords;
- Agent registration tokens;
- Agent tokens;
- TLS private keys;
- real configuration secrets.

## Host acceptance baseline

Before installing RouteGate, verify:

```bash
hostnamectl --static
grep -E '^(PRETTY_NAME|VERSION_ID|ID)=' /etc/os-release
dpkg --print-architecture
systemctl is-system-running
command -v sing-box || true
command -v routegate-manager || true
command -v routegate-agent || true
```

Expected:

- Ubuntu 24.04 LTS;
- amd64;
- systemd running;
- no RouteGate binaries;
- no sing-box binary or package.

## Build validation package in the trusted workspace

Build on the RG-89C branch. Do not install Go or Node.js on the VPS solely to compile RouteGate.

```bash
cd /workspaces/RouteGate

git fetch --prune origin
git switch rg-89c-vpn-core-installation-e2e
git pull --ff-only

test -z "$(git status --porcelain)"

VERSION="rg-89c-$(git rev-parse --short HEAD)"
COMMIT="$(git rev-parse HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
OUT="$(pwd)/out/rg-89c-amd64"

rm -rf "$OUT"
mkdir -p \
  "$OUT/bin" \
  "$OUT/manager" \
  "$OUT/frontend" \
  "$OUT/systemd" \
  "$OUT/nginx" \
  "$OUT/examples"

(
  cd backend
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags "-s -w -X github.com/ikaevus/routegate/backend/internal/buildinfo.Version=${VERSION} -X github.com/ikaevus/routegate/backend/internal/buildinfo.GitCommit=${COMMIT} -X github.com/ikaevus/routegate/backend/internal/buildinfo.BuildDate=${BUILD_DATE}" \
    -o "$OUT/bin/routegate-manager" \
    ./cmd/routegate-manager
)

(
  cd agent
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags "-s -w -X github.com/ikaevus/routegate/agent/internal/buildinfo.Version=${VERSION} -X github.com/ikaevus/routegate/agent/internal/buildinfo.GitCommit=${COMMIT} -X github.com/ikaevus/routegate/agent/internal/buildinfo.BuildDate=${BUILD_DATE}" \
    -o "$OUT/bin/routegate-agent" \
    ./cmd/routegate-agent
)

(
  cd frontend
  npm ci
  npm run i18n:check
  npm run build
)

cp -a backend/migrations "$OUT/manager/migrations"
cp -a frontend/dist/. "$OUT/frontend/"
cp deploy/systemd/routegate-manager.service "$OUT/systemd/"
cp deploy/systemd/routegate-agent.service "$OUT/systemd/"
cp deploy/systemd/manager.env.example "$OUT/examples/"
cp deploy/systemd/agent.yaml.example "$OUT/examples/"
cp deploy/nginx/routegate.conf.example "$OUT/nginx/"

PACKAGE="$(pwd)/out/routegate-rg-89c-amd64.tar.gz"
tar -C "$OUT" -czf "$PACKAGE" .
sha256sum "$PACKAGE" > "${PACKAGE}.sha256"

tar -tzf "$PACKAGE" >/dev/null
ls -lh "$PACKAGE" "${PACKAGE}.sha256"
```

Transfer the archive and checksum to the clean VPS through the operator's existing SSH/SFTP path. Do not commit the generated package.

## Install current platform dependencies on the clean VPS

The commands in this section are the temporary manual deployment path, not the final RouteGate user experience.

Install only the platform dependencies required for this validation:

```bash
sudo apt-get update
sudo apt-get install -y \
  ca-certificates \
  curl \
  nginx \
  openssl \
  postgresql \
  postgresql-client \
  tar
```

For public HTTPS on `us.routegate.org`, install Certbot when DNS already points to the VPS:

```bash
sudo apt-get install -y certbot python3-certbot-nginx
```

## Install the validation package

Assume the transferred archive is in the operator's home directory.

```bash
PACKAGE="$HOME/routegate-rg-89c-amd64.tar.gz"
CHECKSUM="${PACKAGE}.sha256"
STAGING="/tmp/routegate-rg-89c"

sha256sum -c "$CHECKSUM"
rm -rf "$STAGING"
mkdir -p "$STAGING"
tar -C "$STAGING" -xzf "$PACKAGE"

sudo useradd \
  --system \
  --home-dir /var/lib/routegate-manager \
  --create-home \
  --shell /usr/sbin/nologin \
  routegate 2>/dev/null || true

sudo install -d -m 0755 /etc/routegate
sudo install -d -m 0755 -o routegate -g routegate /opt/routegate-manager
sudo install -d -m 0755 /var/www/routegate
sudo install -d -m 0755 /var/lib/routegate-agent/configs
sudo install -d -m 0755 /var/lib/routegate-agent/backups

sudo install -m 0755 "$STAGING/bin/routegate-manager" /usr/local/bin/routegate-manager
sudo install -m 0755 "$STAGING/bin/routegate-agent" /usr/local/bin/routegate-agent

sudo rm -rf /opt/routegate-manager/migrations
sudo cp -a "$STAGING/manager/migrations" /opt/routegate-manager/migrations
sudo chown -R routegate:routegate /opt/routegate-manager

sudo rm -rf /var/www/routegate/*
sudo cp -a "$STAGING/frontend/." /var/www/routegate/
sudo chown -R root:root /var/www/routegate

sudo install -m 0644 "$STAGING/systemd/routegate-manager.service" /etc/systemd/system/routegate-manager.service
sudo install -m 0644 "$STAGING/systemd/routegate-agent.service" /etc/systemd/system/routegate-agent.service
```

## Bootstrap PostgreSQL

Generate a URL-safe database password and create a dedicated local database role.

```bash
DB_PASSWORD="$(openssl rand -hex 32)"

sudo -u postgres psql \
  --set=ON_ERROR_STOP=1 \
  --set=db_password="$DB_PASSWORD" <<'SQL'
CREATE ROLE routegate LOGIN PASSWORD :'db_password';
CREATE DATABASE routegate OWNER routegate;
REVOKE ALL ON DATABASE routegate FROM PUBLIC;
SQL
```

Store the generated password securely before closing the shell. Do not paste it into GitHub or the validation issue.

## Configure and start Manager

Generate a temporary strong administrator password. This is an interim bootstrap mechanism until the accepted one-time setup flow is implemented.

```bash
ADMIN_PASSWORD="$(openssl rand -hex 24)"

sudo install -m 0600 -o root -g root /dev/null /etc/routegate/manager.env

sudo tee /etc/routegate/manager.env >/dev/null <<EOF
ROUTEGATE_ENV=production
ROUTEGATE_HTTP_ADDR=127.0.0.1:8080
ROUTEGATE_DATABASE_URL=postgres://routegate:${DB_PASSWORD}@127.0.0.1:5432/routegate?sslmode=disable
ROUTEGATE_LOG_LEVEL=info
ROUTEGATE_AUTH_SESSION_TTL_HOURS=24
ROUTEGATE_BOOTSTRAP_ADMIN_EMAIL=admin@routegate.local
ROUTEGATE_BOOTSTRAP_ADMIN_USERNAME=admin
ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD=${ADMIN_PASSWORD}
ROUTEGATE_BOOTSTRAP_ADMIN_DISPLAY_NAME=RouteGate Administrator
EOF

sudo chmod 0600 /etc/routegate/manager.env
sudo systemctl daemon-reload
sudo systemctl enable --now postgresql
sudo systemctl enable --now routegate-manager

sudo systemctl status routegate-manager --no-pager
curl -fsS http://127.0.0.1:8080/api/admin/health
```

The Manager unit uses `/opt/routegate-manager` as its working directory because the current migration loader reads the relative `migrations` directory during startup.

After confirming the first SuperAdmin exists and login succeeds, remove the temporary bootstrap password from `/etc/routegate/manager.env` and restart Manager.

## Configure nginx and HTTPS

Install the supplied nginx template and replace the example hostname.

```bash
sudo install -m 0644 "$STAGING/nginx/routegate.conf.example" /etc/nginx/sites-available/routegate
sudo sed -i 's/routegate\.example\.com/us.routegate.org/g' /etc/nginx/sites-available/routegate
sudo ln -sfn /etc/nginx/sites-available/routegate /etc/nginx/sites-enabled/routegate
sudo rm -f /etc/nginx/sites-enabled/default

sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx
```

Verify HTTP before requesting a certificate:

```bash
curl -I http://us.routegate.org/
curl -fsS http://us.routegate.org/api/admin/health
```

Then request HTTPS through the normal Certbot nginx integration:

```bash
sudo certbot --nginx -d us.routegate.org
```

Verify:

```bash
curl -I https://us.routegate.org/
curl -fsS https://us.routegate.org/api/admin/health
```

## Create the RouteGate server and registration token

Through the Admin UI:

1. Sign in with the temporary bootstrap administrator credentials.
2. Create the `us.routegate.org` server record.
3. Open Server Details.
4. Generate a one-time Agent registration token.
5. Keep the token only long enough to create `/etc/routegate/agent.yaml`.

## Configure and start Agent

Before starting Agent, verify the clean VPN Core state:

```bash
command -v sing-box && exit 1 || true
dpkg-query -W sing-box 2>/dev/null && exit 1 || true
systemctl status sing-box --no-pager 2>/dev/null && exit 1 || true
```

Create the Agent YAML using the one-time registration token from the UI:

```yaml
manager_url: "https://us.routegate.org"
registration_token: "REPLACE_WITH_ONE_TIME_REGISTRATION_TOKEN"
heartbeat_interval_seconds: 10

config_staging_dir: "/var/lib/routegate-agent/configs"
active_config_path: "/etc/sing-box/config.json"
config_backup_dir: "/var/lib/routegate-agent/backups"

sing_box_path: "/usr/bin/sing-box"
sing_box_service_name: "sing-box"
service_control_enabled: true

traffic_collection_enabled: false
traffic_collection_interval_seconds: 60
traffic_usage_file_path: "/var/lib/routegate-agent/traffic-usage.json"
```

Install it with strict permissions, then start Agent:

```bash
sudo install -m 0600 -o root -g root /path/to/prepared-agent.yaml /etc/routegate/agent.yaml
sudo systemctl daemon-reload
sudo systemctl enable --now routegate-agent
sudo systemctl status routegate-agent --no-pager
sudo journalctl -u routegate-agent -n 100 --no-pager
```

After successful registration, Agent rewrites the YAML with its persistent credentials and removes the one-time registration token.

## Pre-installation acceptance checks

Confirm in the UI and through host evidence:

- Agent is connected;
- Agent heartbeat reports `vpnCoreInstallationOperations: ["install_sing_box"]`;
- VPN Core state is `not_installed`;
- the primary action is **Install sing-box**;
- service-control actions are not shown before installation.

Host evidence:

```bash
command -v sing-box || true
dpkg-query -W sing-box 2>/dev/null || true
systemctl list-unit-files sing-box.service --no-pager 2>/dev/null || true
```

## Execute installation from RouteGate

1. Click **Install sing-box**.
2. Confirm the installation warning.
3. Verify the UI shows queued/in-progress state.
4. Verify conflicting controls are disabled.
5. Observe Agent logs without exposing credentials:

```bash
sudo journalctl -u routegate-agent -f
```

Expected task result:

- kind: `vpn_core_install`;
- operation: `install_sing_box`;
- status: `succeeded`;
- platform: Ubuntu 24.04 / amd64;
- binary path: `/usr/bin/sing-box`;
- service name: `sing-box.service`;
- structured stage results are present.

## Post-installation host evidence

```bash
command -v sing-box
/usr/bin/sing-box version
dpkg-query -W -f='${binary:Package}\t${Version}\t${Status}\n' sing-box
systemctl cat sing-box
systemctl is-enabled sing-box 2>/dev/null || true
systemctl is-active sing-box 2>/dev/null || true
sudo journalctl -u routegate-agent -n 200 --no-pager
```

The package service may be loaded but not yet running until a valid VPN configuration is deployed. RG-89C requires successful package and systemd detection; a later Config Deploy validation covers the running VPN service.

Confirm the next Agent heartbeat changes VPN Core from `not_installed` to an installed state and displays version, binary path, service name, and service state.

## Idempotent second request

The normal UI hides the installation action after successful detection. To exercise the idempotent backend path, submit the same authenticated installation endpoint again through an authenticated browser session or an equivalent controlled API request.

Before the second request, capture:

```bash
/usr/bin/sing-box version
sha256sum /usr/bin/sing-box
apt-cache policy sing-box
```

After the second request, capture the same evidence and verify:

- the job succeeds through the already-installed path;
- no upgrade is performed;
- the binary hash and package version are unchanged;
- repository and signing-key files are not overwritten;
- the service unit remains loaded.

## Duplicate active-operation check

A safe way to verify duplicate rejection is:

1. Stop Agent temporarily so the first queued operation remains pending.
2. Submit one installation request.
3. Immediately submit a second installation request.
4. Verify the second request returns `409 operation_in_progress`.
5. Start Agent and allow the original job to finish.

Do not leave Agent stopped after the check.

## Reboot persistence

```bash
sudo systemctl reboot
```

After SSH returns:

```bash
systemctl is-active routegate-manager
systemctl is-active routegate-agent
systemctl is-active nginx
systemctl is-active postgresql
command -v sing-box
/usr/bin/sing-box version
sudo journalctl -u routegate-agent -b --no-pager
```

Confirm in the UI:

- Agent reconnects;
- the installed VPN Core state persists;
- the installation capability remains advertised;
- no stale active installation job blocks future operations.

## Failure paths

Failure-path checks must be safe and reversible.

- Unsupported platform and architecture are primarily covered by automated Agent tests.
- A repository source or signing-key conflict may be tested only on a disposable clean cycle before successful installation; RouteGate must reject the conflict and must not replace the conflicting file.
- Download and command timeout behavior is covered by unit tests; a live network-fault test is optional.
- If Agent is killed after claiming a job, record whether the job remains indefinitely `in_progress`. The current repository does not yet implement stale-job recovery; any observed permanent block should become a separate recovery issue.

## Evidence checklist

Record only non-secret evidence:

- clean Ubuntu version and architecture;
- absence of sing-box before installation;
- Agent capability advertisement;
- screenshot of `not_installed` and guided action;
- screenshot of queued/in-progress state;
- sanitized structured success result;
- sing-box package version and binary path;
- systemd unit detection;
- idempotent second-request evidence;
- duplicate active-operation rejection;
- reboot and reconnect evidence;
- any manual deployment step that belongs in the future installer backlog.

## Acceptance decision

RG-89C passes when:

- clean `not_installed → install → installed` succeeds;
- installation is initiated through Manager and executed by Agent;
- no arbitrary shell command or package selection is exposed;
- structured job results are returned;
- repeat execution is idempotent;
- duplicate active operations are rejected;
- Agent and installed-state detection persist after reboot;
- observed gaps are recorded without being confused with the future Clean VPS Installer scope.
