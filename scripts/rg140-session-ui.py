#!/usr/bin/env python3
"""Publish only index.html and content-addressed UI assets; keep old assets."""
import hashlib
import io
import os
from pathlib import Path, PurePosixPath
import re
import signal
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

CANDIDATE = '6db6e4bcf9f5c547a440d1ea2586a2ebae3665ea'
ROOT = Path('/var/www/routegate')
PROTECTED = ['/etc/routegate', '/etc/sing-box', '/etc/nginx', '/etc/wireguard',
             '/etc/hysteria', '/etc/routegate-mtproto', '/usr/local/bin/routegate-manager',
             '/usr/local/bin/routegate-agent', '/var/www/routegate/bootstrap']
SERVICES = ['routegate-manager', 'routegate-agent', 'nginx', 'sing-box',
            'wg-quick@routegate-wg0', 'hysteria-server', 'routegate-mtproto']

def digest(data):
    return hashlib.sha256(data).hexdigest()

def safe_path(path):
    if any(p.is_symlink() for p in [path, *path.parents]):
        raise RuntimeError('symlink_refused')

def payload(bundle, expected):
    data = bundle.read_bytes()
    if not re.fullmatch('[0-9a-f]{64}', expected) or digest(data) != expected:
        raise RuntimeError('bundle_digest_mismatch')
    files = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
        members = archive.getmembers()
        if len(members) > 100 or sum(m.size for m in members) > 20_000_000:
            raise RuntimeError('bundle_size_refused')
        for member in members:
            path = PurePosixPath(member.name)
            if str(path) != member.name or path.is_absolute() or '..' in path.parts:
                raise RuntimeError('unsafe_bundle_path')
            allowed = member.name == 'index.html' or re.fullmatch(r'assets/[A-Za-z0-9_.-]+', member.name)
            if not member.isfile() or not allowed or member.name in files:
                raise RuntimeError('unexpected_bundle_entry')
            files[member.name] = archive.extractfile(member).read()
    if not files.get('index.html') or len(files) < 2:
        raise RuntimeError('missing_ui_files')
    index = files['index.html'].decode('utf-8')
    refs = re.findall(r'(?:src|href)="(/assets/[^"?#]+)"', index)
    if not refs or any(ref[1:] not in files for ref in refs):
        raise RuntimeError('missing_index_asset')
    return files

def atomic_index(root, content):
    safe_path(root / 'index.html')
    fd, name = tempfile.mkstemp(prefix='.rg140-index-', dir=root)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
            os.fchmod(stream.fileno(), 0o644)
        os.replace(name, root / 'index.html')
    finally:
        path = Path(name)
        if path.exists():
            path.unlink()

def publish(root, files, backup, verify):
    safe_path(root)
    safe_path(backup)
    safe_path(root / 'index.html')
    old = (root / 'index.html').read_bytes()
    backup.mkdir(mode=0o700)
    (backup / 'index.html').write_bytes(old)
    os.chmod(backup / 'index.html', 0o600)
    # Validate every destination before writing anything. Existing hashed assets
    # must match; never replace old assets or touch bootstrap artifacts.
    for name, content in files.items():
        if name == 'index.html':
            continue
        target = root / name
        safe_path(target)
        if target.exists() and (not target.is_file() or target.read_bytes() != content):
            raise RuntimeError('asset_collision_refused')
    (root / 'assets').mkdir(mode=0o755, exist_ok=True)
    for name, content in files.items():
        if name == 'index.html':
            continue
        target = root / name
        if not target.exists():
            with target.open('xb') as stream:
                stream.write(content)
            os.chmod(target, 0o644)
    try:
        atomic_index(root, files['index.html'])
        verify()
    except BaseException:
        atomic_index(root, old)
        print('frontend=previous_index_restored; old_assets=retained', flush=True)
        raise

def snapshot():
    files = {}
    for name in PROTECTED:
        root = Path(name)
        for path in ([root] if root.is_file() else root.rglob('*') if root.is_dir() else []):
            if path.is_symlink():
                files[str(path)] = ('link', os.readlink(path))
            elif path.is_file():
                files[str(path)] = digest(path.read_bytes())
    services = {}
    for name in SERVICES:
        result = subprocess.run(['systemctl', 'show', name, '-p', 'MainPID', '-p', 'ActiveState',
                                 '-p', 'ActiveEnterTimestampMonotonic'], capture_output=True, text=True, check=True)
        services[name] = result.stdout
    return files, services

def readonly_preflight():
    env = os.environ.copy()
    env['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=5s -c lock_timeout=1s'
    env['PGCONNECT_TIMEOUT'] = '3'
    query = "SELECT (SELECT max(version) FROM schema_migrations)='000159_staged_account_transfers' AND NOT EXISTS(SELECT 1 FROM vpn_account_transfers WHERE completed_at IS NULL)"
    result = subprocess.run(['psql', os.environ['ROUTEGATE_DATABASE_URL'], '-X', '-qAt',
                             '-v', 'ON_ERROR_STOP=1', '-c', query], env=env, capture_output=True, text=True)
    if result.returncode or result.stdout.strip() != 't':
        raise RuntimeError('schema159_or_idle_transfer_check_failed')

def main():
    bundle, expected, run_id = sys.argv[1:]
    if not re.fullmatch(r'[0-9]+', run_id):
        raise RuntimeError('invalid_run_id')
    files = payload(Path(bundle), expected)
    readonly_preflight()
    baseline = snapshot()
    backups = Path('/root/routegate-backups')
    safe_path(backups)
    backups.mkdir(mode=0o700, exist_ok=True)
    backup = backups / f'ui-session-{CANDIDATE}-{run_id}'
    def verify():
        with urllib.request.urlopen(f'https://us.routegate.org/?rg140_session_ui={run_id}', timeout=15) as response:
            if response.status != 200 or response.read() != files['index.html']:
                raise RuntimeError('public_ui_verification_failed')
        if snapshot() != baseline:
            raise RuntimeError('protected_files_or_services_changed')
    publish(ROOT, files, backup, verify)
    print(f'frontend_commit={CANDIDATE}; public_index=verified; protected_files_and_services=unchanged', flush=True)
    print('backup=' + str(backup), flush=True)

def interrupted(_signal, _frame):
    raise RuntimeError('interrupted')

if __name__ == '__main__':
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, interrupted)
    try:
        main()
    except Exception:
        print('UI_UPDATE_REFUSED_OR_ROLLED_BACK; private_details_suppressed', flush=True)
        sys.exit(1)
