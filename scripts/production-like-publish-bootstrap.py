#!/usr/bin/env python3
"""Publish immutable bootstrap files only; never install a bundle or run a service."""
import hashlib
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path('/var/www/routegate/bootstrap')
PUBLIC = 'https://us.routegate.org/bootstrap'
NAMES = [f'routegate-production-like-linux-{arch}.tar.gz' for arch in ('amd64', 'arm64')]


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def regular(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError('input or published file is not a regular file')


def validate(commit, bundles, sums, sums_sha):
    if not re.fullmatch('[0-9a-f]{40}', commit) or not re.fullmatch('[0-9a-f]{64}', sums_sha):
        raise ValueError('invalid commit or checksum identity')
    for path in [*bundles, sums]:
        regular(path)
    if digest(sums) != sums_sha:
        raise ValueError('checksum file differs from the verified Actions output')
    entries = {}
    for line in sums.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64}) [ *]([^/\s]+)', line)
        if not match or match[2] in entries:
            raise ValueError('invalid or duplicate checksum entry')
        entries[match[2]] = match[1]
    if set(entries) != set(NAMES):
        raise ValueError('checksums must describe exactly amd64 and arm64 bundles')
    for arch, name, path in zip(('amd64', 'arm64'), NAMES, bundles):
        if digest(path) != entries[name]:
            raise ValueError(f'{arch} bundle checksum mismatch')
        with tarfile.open(path, 'r:gz') as archive:
            seen = set()
            manifest = None
            for member in archive:
                rel = PurePosixPath(member.name)
                normalized = str(rel)
                if rel.is_absolute() or '..' in rel.parts or normalized in seen:
                    raise ValueError('unsafe or duplicate archive path')
                seen.add(normalized)
                if not (member.isfile() or member.isdir()):
                    raise ValueError('archive contains links or special files')
                if normalized == 'metadata/manifest.env':
                    if member.size > 65536:
                        raise ValueError('manifest is too large')
                    manifest = archive.extractfile(member).read().decode('utf-8')
            if manifest is None:
                raise ValueError('bundle manifest missing')
            fields = {}
            for line in manifest.splitlines():
                key, sep, value = line.partition('=')
                if sep:
                    if key in fields:
                        raise ValueError('duplicate manifest field')
                    fields[key] = value
            required = {'VERSION': 'production-like', 'COMMIT': commit, 'OS': 'linux', 'ARCH': arch}
            if any(fields.get(key) != value for key, value in required.items()):
                raise ValueError('bundle identity differs from the requested commit')
    return entries


def public_probe(commit, sums_sha, entries):
    # Download into private temporary files. Hash bytes, not merely HTTP status
    # (an nginx SPA fallback must not count as a published archive).
    with tempfile.TemporaryDirectory(prefix='routegate-bootstrap-probe-') as temp:
        for name, expected in {'SHA256SUMS': sums_sha, **entries}.items():
            path = Path(temp) / name
            subprocess.run(['curl', '--fail', '--silent', '--show-error', '--proto', '=https',
                            '--connect-timeout', '15', '--max-time', '180',
                            f'{PUBLIC}/{commit}/{name}', '-o', str(path)], check=True)
            if digest(path) != expected:
                raise ValueError('public artifact checksum mismatch')


def publish(commit, bundles, sums, sums_sha, root=ROOT, probe=public_probe):
    entries = validate(commit, bundles, sums, sums_sha)
    if any(parent.is_symlink() for parent in (root, *root.parents)):
        raise ValueError('bootstrap path must not contain symlinks')
    target = root / commit
    if target.exists() or target.is_symlink():
        if target.is_symlink() or not target.is_dir():
            raise ValueError('existing commit directory is unsafe')
        if {p.name for p in target.iterdir()} != {*NAMES, 'SHA256SUMS'}:
            raise ValueError('existing commit directory differs; refusing to replace it')
        for name, expected in {'SHA256SUMS': sums_sha, **entries}.items():
            regular(target / name)
            if digest(target / name) != expected:
                raise ValueError('existing artifacts differ; refusing to overwrite them')
        probe(commit, sums_sha, entries)
        return 'already-published'
    root.mkdir(mode=0o755, parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f'.{commit}.', dir=root))
    created = False
    try:
        staging.chmod(0o755)
        for source, name in zip([*bundles, sums], [*NAMES, 'SHA256SUMS']):
            shutil.copyfile(source, staging / name)
            (staging / name).chmod(0o644)
        # Caller holds the shared production-like flock. Existing commit
        # directories are immutable and other commits are never pruned here.
        staging.rename(target)
        created = True
        probe(commit, sums_sha, entries)
    except BaseException:
        if created:
            shutil.rmtree(target)
        raise
    finally:
        if staging.exists():
            shutil.rmtree(staging)
    return 'published'


def main():
    if os.geteuid() != 0 or len(sys.argv) != 6:
        print('[routegate-bootstrap] ERROR: root and commit/amd64/arm64/checksums/checksums-sha arguments required', file=sys.stderr)
        return 2
    commit, amd64, arm64, sums, sums_sha = sys.argv[1:]
    try:
        result = publish(commit, [Path(amd64), Path(arm64)], Path(sums), sums_sha)
    except (ValueError, OSError, tarfile.TarError, subprocess.SubprocessError) as error:
        print(f'[routegate-bootstrap] ERROR: {error}', file=sys.stderr)
        return 1
    print(f'[routegate-bootstrap] RESULT={result} commit={commit}; public hashes verified; services unchanged')
    return 0


if __name__ == '__main__':
    sys.exit(main())
