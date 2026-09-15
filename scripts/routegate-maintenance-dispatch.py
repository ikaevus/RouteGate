#!/usr/bin/env python3

import json
import os
import pwd
import re
import secrets
import shutil
import stat
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

MAX_REQUEST_BYTES = 96
MAX_RESPONSE_BYTES = 4096
PLAN_TTL_SECONDS = 15 * 60
MAX_BACKUP_CANDIDATES = 256
BACKUP_RETENTION_SECONDS = 30 * 24 * 60 * 60
PROMETHEUS_READY_TIMEOUT_SECONDS = 30
ROOT_UID = 0
PLAN_ROOT = Path("/run/routegate/maintenance-plans")
BACKUP_ROOT = Path("/root/routegate-backups")
INSTALL_STATE = Path("/etc/routegate/install-state.env")
PROMETHEUS_CONFIG = Path("/etc/prometheus/routegate.yml")
PROMETHEUS_TOKEN = Path("/etc/prometheus/routegate.token")
PROMETHEUS_STORAGE = Path("/var/lib/prometheus/routegate")
PROMETHEUS_OVERRIDE = Path("/etc/systemd/system/prometheus.service.d/routegate.conf")
TOKEN_RE = re.compile(r"^[0-9a-f]{32}$")
BACKUP_RE = re.compile(
    r"^(?:rg96|update-(?:management|vpn|hybrid))-[0-9a-f]{40}-(\d{8}T\d{6}Z)$"
)
ANALYZE_OPERATIONS = {
    "analyze:platform_rollback_backups": "platform_rollback_backups",
    "analyze:prometheus_tsdb_retention": "prometheus_tsdb_retention",
}
PROMETHEUS_OVERRIDE_WITHOUT_RETENTION = """[Service]
ExecStart=
ExecStart=/usr/bin/prometheus --config.file=/etc/prometheus/routegate.yml --storage.tsdb.path=/var/lib/prometheus/routegate --web.listen-address=127.0.0.1:9090
"""
PROMETHEUS_OVERRIDE_WITH_RETENTION = """[Service]
ExecStart=
ExecStart=/usr/bin/prometheus --config.file=/etc/prometheus/routegate.yml --storage.tsdb.path=/var/lib/prometheus/routegate --storage.tsdb.retention.time=90d --web.listen-address=127.0.0.1:9090
"""


class DispatchError(Exception):
    pass


def reject(message: str) -> None:
    raise DispatchError(message)


def read_request() -> str:
    raw = sys.stdin.buffer.readline(MAX_REQUEST_BYTES + 1)
    if len(raw) > MAX_REQUEST_BYTES:
        reject("request too large")
    if not raw.endswith(b"\n"):
        reject("request must end with newline")
    if b"\x00" in raw or sys.stdin.buffer.read(1) != b"":
        reject("request contains extra data")
    try:
        request = raw[:-1].decode("ascii")
    except UnicodeDecodeError as exc:
        raise DispatchError("request is not ASCII") from exc
    if request in ANALYZE_OPERATIONS:
        return request
    prefix, separator, token = request.partition(":")
    if separator and prefix in {"cleanup", "verify"} and TOKEN_RE.fullmatch(token):
        return request
    reject("unsupported maintenance request")


def secure_directory(path: Path, owner_uid: int, *, allow_missing: bool = False) -> bool:
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        if allow_missing:
            return False
        raise
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        reject(f"unsafe directory: {path}")
    if info.st_uid != owner_uid or info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        reject(f"unsafe directory ownership: {path}")
    return True


def secure_regular(path: Path, owner_uid: int) -> None:
    info = os.lstat(path)
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        reject(f"unsafe file: {path}")
    if info.st_uid != owner_uid or info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        reject(f"unsafe file ownership: {path}")


def directory_bytes(root: Path) -> int:
    total = 0
    for current, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = [name for name in dirs if not (Path(current) / name).is_symlink()]
        for name in files:
            path = Path(current) / name
            try:
                info = os.lstat(path)
            except FileNotFoundError:
                continue
            if stat.S_ISREG(info.st_mode):
                total += info.st_size
    return total


def backup_timestamp(name: str) -> float:
    match = BACKUP_RE.fullmatch(name)
    if not match:
        reject("backup name is not canonical")
    return datetime.strptime(match.group(1), "%Y%m%dT%H%M%SZ").replace(tzinfo=timezone.utc).timestamp()


def safe_backups(now: float) -> tuple[list[str], int]:
    if not secure_directory(BACKUP_ROOT, ROOT_UID, allow_missing=True):
        return [], 0
    entries = []
    for entry in BACKUP_ROOT.iterdir():
        if not BACKUP_RE.fullmatch(entry.name):
            continue
        info = os.lstat(entry)
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
            reject("canonical backup entry is unsafe")
        if info.st_uid != ROOT_UID or info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
            reject("canonical backup ownership is unsafe")
        entries.append((backup_timestamp(entry.name), entry.name, entry))
    entries.sort()
    if not entries:
        return [], 0
    newest_name = entries[-1][1]
    cutoff = now - BACKUP_RETENTION_SECONDS
    candidates = [(name, path) for timestamp, name, path in entries if timestamp < cutoff and name != newest_name]
    if len(candidates) > MAX_BACKUP_CANDIDATES:
        reject("too many rollback backup candidates")
    return [name for name, _ in candidates], sum(directory_bytes(path) for _, path in candidates)


def state_value(key: str) -> str:
    secure_regular(INSTALL_STATE, ROOT_UID)
    for line in INSTALL_STATE.read_text(encoding="utf-8").splitlines():
        if line.startswith(key + "="):
            return line.split("=", 1)[1]
    return ""


def validate_managed_prometheus() -> str:
    if state_value("PROMETHEUS_MANAGED") != "1":
        reject("Prometheus is not RouteGate-managed")
    secure_regular(PROMETHEUS_CONFIG, ROOT_UID)
    secure_regular(PROMETHEUS_TOKEN, ROOT_UID)
    secure_regular(PROMETHEUS_OVERRIDE, ROOT_UID)
    secure_directory(PROMETHEUS_STORAGE, pwd.getpwnam("prometheus").pw_uid)
    current = PROMETHEUS_OVERRIDE.read_text(encoding="utf-8")
    if current not in {PROMETHEUS_OVERRIDE_WITHOUT_RETENTION, PROMETHEUS_OVERRIDE_WITH_RETENTION}:
        reject("Prometheus override is not canonical")
    return current


def ensure_plan_root() -> None:
    PLAN_ROOT.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(PLAN_ROOT, 0o700)
    secure_directory(PLAN_ROOT, ROOT_UID)


def purge_expired_plans(now: float) -> None:
    for path in PLAN_ROOT.iterdir():
        if not re.fullmatch(r"[0-9a-f]{32}\.json", path.name):
            continue
        secure_regular(path, ROOT_UID)
        if os.lstat(path).st_mtime < now - PLAN_TTL_SECONDS:
            path.unlink()


def write_plan(kind: str, candidates: list[str], estimated_bytes: int) -> dict:
    ensure_plan_root()
    purge_expired_plans(time.time())
    token = secrets.token_hex(16)
    now = int(time.time())
    plan = {
        "schemaVersion": 1,
        "kind": kind,
        "createdAt": now,
        "expiresAt": now + PLAN_TTL_SECONDS,
        "candidateNames": candidates,
        "estimatedBytes": estimated_bytes,
    }
    path = PLAN_ROOT / f"{token}.json"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as output:
        json.dump(plan, output, separators=(",", ":"), sort_keys=True)
        output.write("\n")
    return {"token": token, "candidateCount": len(candidates), "estimatedBytes": estimated_bytes}


def read_plan(token: str) -> dict:
    if not TOKEN_RE.fullmatch(token):
        reject("invalid maintenance token")
    secure_directory(PLAN_ROOT, ROOT_UID)
    path = PLAN_ROOT / f"{token}.json"
    secure_regular(path, ROOT_UID)
    raw = path.read_bytes()
    if len(raw) > 64 * 1024:
        reject("maintenance plan is too large")
    plan = json.loads(raw)
    if (
        plan.get("schemaVersion") != 1
        or plan.get("kind") not in set(ANALYZE_OPERATIONS.values())
        or not isinstance(plan.get("candidateNames"), list)
        or any(not isinstance(name, str) for name in plan["candidateNames"])
        or int(plan.get("expiresAt", 0)) <= int(time.time())
    ):
        reject("maintenance plan is invalid or expired")
    return plan


def analyze(kind: str) -> dict:
    if kind == "platform_rollback_backups":
        names, estimated_bytes = safe_backups(time.time())
        return write_plan(kind, names, estimated_bytes)
    current = validate_managed_prometheus()
    candidates = [] if current == PROMETHEUS_OVERRIDE_WITH_RETENTION else ["retention_90d"]
    return write_plan(kind, candidates, 0)


def cleanup_backups(plan: dict) -> dict:
    current_candidates, _ = safe_backups(time.time())
    allowed = set(current_candidates)
    requested = plan["candidateNames"]
    if any(not BACKUP_RE.fullmatch(name) or name not in allowed for name in requested):
        reject("rollback backup candidate is no longer safe")
    deleted = 0
    reclaimed = 0
    for name in requested:
        path = BACKUP_ROOT / name
        info = os.lstat(path)
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode) or info.st_uid != ROOT_UID:
            reject("rollback backup candidate changed")
        reclaimed += directory_bytes(path)
        shutil.rmtree(path)
        deleted += 1
    return {"deletedCount": deleted, "reclaimedBytes": reclaimed}


def prometheus_ready() -> bool:
    try:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open("http://127.0.0.1:9090/-/ready", timeout=5) as response:
            return response.status == 200
    except OSError:
        return False


def wait_prometheus_ready() -> bool:
    deadline = time.monotonic() + PROMETHEUS_READY_TIMEOUT_SECONDS
    while True:
        if prometheus_ready():
            return True
        if time.monotonic() >= deadline:
            return False
        time.sleep(1)


def replace_prometheus_override(contents: str) -> None:
    temporary = PROMETHEUS_OVERRIDE.with_name(
        f".routegate.conf.maintenance.{secrets.token_hex(8)}"
    )
    descriptor = os.open(
        temporary,
        os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0),
        0o644,
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            os.fchmod(output.fileno(), 0o644)
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, PROMETHEUS_OVERRIDE)
    finally:
        try:
            temporary.unlink()
        except FileNotFoundError:
            pass


def cleanup_prometheus(plan: dict) -> dict:
    current = validate_managed_prometheus()
    if plan["candidateNames"] == []:
        return {"deletedCount": 0, "reclaimedBytes": 0}
    if plan["candidateNames"] != ["retention_90d"] or current != PROMETHEUS_OVERRIDE_WITHOUT_RETENTION:
        reject("Prometheus retention candidate changed")
    previous = current
    try:
        replace_prometheus_override(PROMETHEUS_OVERRIDE_WITH_RETENTION)
        subprocess.run(["systemctl", "daemon-reload"], check=True, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin"})
        subprocess.run(["systemctl", "restart", "prometheus"], check=True, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin"})
        if not wait_prometheus_ready():
            reject("Prometheus did not become ready")
    except (OSError, subprocess.SubprocessError, DispatchError):
        replace_prometheus_override(previous)
        subprocess.run(["systemctl", "daemon-reload"], check=False, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin"})
        subprocess.run(["systemctl", "restart", "prometheus"], check=False, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin"})
        raise
    return {"deletedCount": 1, "reclaimedBytes": 0}


def verify(plan: dict) -> dict:
    if plan["kind"] == "platform_rollback_backups":
        remaining = 0
        for name in plan["candidateNames"]:
            try:
                os.lstat(BACKUP_ROOT / name)
                remaining += 1
            except FileNotFoundError:
                pass
        return {"remainingCount": remaining}
    current = validate_managed_prometheus()
    remaining = 0 if current == PROMETHEUS_OVERRIDE_WITH_RETENTION and prometheus_ready() else 1
    return {"remainingCount": remaining}


def handle(request: str) -> dict:
    if request in ANALYZE_OPERATIONS:
        return analyze(ANALYZE_OPERATIONS[request])
    operation, token = request.split(":", 1)
    plan = read_plan(token)
    if operation == "cleanup":
        if plan["kind"] == "platform_rollback_backups":
            return cleanup_backups(plan)
        return cleanup_prometheus(plan)
    return verify(plan)


def write_response(payload: dict) -> None:
    encoded = (json.dumps({"ok": True, **payload}, separators=(",", ":"), sort_keys=True) + "\n").encode("ascii")
    if len(encoded) > MAX_RESPONSE_BYTES:
        reject("response is too large")
    sys.stdout.buffer.write(encoded)
    sys.stdout.buffer.flush()


def main() -> int:
    try:
        if os.geteuid() != 0:
            reject("dispatcher must run as root")
        write_response(handle(read_request()))
        return 0
    except (DispatchError, json.JSONDecodeError, OSError, ValueError, subprocess.SubprocessError):
        sys.stdout.write('{"ok":false}\n')
        sys.stdout.flush()
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
