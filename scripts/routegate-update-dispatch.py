#!/usr/bin/env python3

import os
import pwd
import re
import stat
import subprocess
import sys
from pathlib import Path

STAGING_ROOT = Path("/var/lib/routegate-manager/update-staging")
VERIFIED_UPDATER = Path("/usr/local/lib/routegate/update/routegate-update-verified.sh")
MAX_REQUEST_BYTES = 64
UUID4_RE = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
BUNDLE_RE = re.compile(r"^routegate-[A-Za-z0-9][A-Za-z0-9._+-]*-linux-(?:amd64|arm64)\.tar\.gz$")
FIXED_ASSETS = (
    "release-manifest.json",
    "release-manifest.attestation.json",
    "SHA256SUMS",
    "release-bundles.attestation.json",
)


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
    if b"\x00" in raw:
        reject("request contains NUL")
    if sys.stdin.buffer.read(1) != b"":
        reject("request contains extra data")
    try:
        text = raw[:-1].decode("ascii")
    except UnicodeDecodeError as exc:
        raise DispatchError("request is not ASCII") from exc
    if not UUID4_RE.fullmatch(text):
        reject("request is not a canonical UUIDv4")
    return text


def require_directory(path: Path, owner_uid: int) -> None:
    try:
        info = os.lstat(path)
    except FileNotFoundError as exc:
        raise DispatchError(f"missing directory: {path}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        reject(f"unsafe directory: {path}")
    if info.st_uid != owner_uid:
        reject(f"unexpected directory owner: {path}")
    if info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        reject(f"writable directory: {path}")


def require_regular(path: Path, owner_uid: int) -> None:
    try:
        info = os.lstat(path)
    except FileNotFoundError as exc:
        raise DispatchError(f"missing staged asset: {path.name}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        reject(f"unsafe staged asset: {path.name}")
    if info.st_uid != owner_uid:
        reject(f"unexpected staged asset owner: {path.name}")
    if info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        reject(f"writable staged asset: {path.name}")


def reconstruct_candidate(job_id: str) -> tuple[Path, Path, Path, Path, Path]:
    routegate_uid = pwd.getpwnam("routegate").pw_uid
    require_directory(STAGING_ROOT, routegate_uid)

    job_dir = STAGING_ROOT / job_id
    require_directory(job_dir, routegate_uid)
    if job_dir.parent != STAGING_ROOT:
        reject("staging path escaped fixed root")

    try:
        entries = list(job_dir.iterdir())
    except OSError as exc:
        raise DispatchError("unable to enumerate staged candidate") from exc

    names = {entry.name for entry in entries}
    bundles = sorted(name for name in names if BUNDLE_RE.fullmatch(name))
    expected = set(FIXED_ASSETS)
    if len(bundles) != 1:
        reject("staged candidate must contain exactly one release bundle")
    expected.add(bundles[0])
    if names != expected or len(entries) != len(expected):
        reject("staged candidate contains missing or unexpected entries")

    manifest = job_dir / "release-manifest.json"
    manifest_attestation = job_dir / "release-manifest.attestation.json"
    checksums = job_dir / "SHA256SUMS"
    bundle_attestation = job_dir / "release-bundles.attestation.json"
    bundle = job_dir / bundles[0]
    for path in (manifest, manifest_attestation, checksums, bundle_attestation, bundle):
        require_regular(path, routegate_uid)

    return manifest, manifest_attestation, checksums, bundle, bundle_attestation


def apply_candidate(paths: tuple[Path, Path, Path, Path, Path]) -> None:
    updater_info = os.lstat(VERIFIED_UPDATER)
    if stat.S_ISLNK(updater_info.st_mode) or not stat.S_ISREG(updater_info.st_mode):
        reject("verified updater is unsafe")
    if updater_info.st_uid != 0 or updater_info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        reject("verified updater trust state is unsafe")

    manifest, manifest_attestation, checksums, bundle, bundle_attestation = paths
    command = [
        str(VERIFIED_UPDATER),
        "apply",
        "--manifest", str(manifest),
        "--manifest-attestation", str(manifest_attestation),
        "--checksums", str(checksums),
        "--bundle", str(bundle),
        "--bundle-attestation", str(bundle_attestation),
        "--role", "auto",
    ]
    completed = subprocess.run(
        command,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
        close_fds=True,
        env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin"},
    )
    if completed.returncode != 0:
        reject("verified apply failed")


def main() -> int:
    try:
        if os.geteuid() != 0:
            reject("dispatcher must run as root")
        job_id = read_request()
        paths = reconstruct_candidate(job_id)
        apply_candidate(paths)
    except (DispatchError, KeyError, OSError, subprocess.SubprocessError):
        sys.stdout.write("ERR\n")
        sys.stdout.flush()
        return 1

    sys.stdout.write("OK\n")
    sys.stdout.flush()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
