import hashlib
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import yaml

REPO = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('publisher', REPO / 'scripts/production-like-publish-bootstrap.py')
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)
COMMIT = 'a' * 40


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / 'web/bootstrap'
        self.root.mkdir(parents=True)
        self.marker = self.root / ('b' * 40)
        self.marker.mkdir()
        (self.marker / 'untouched').write_text('old artifacts')
        self.bundles = [self.base / name for name in publisher.NAMES]
        for arch, path in zip(('amd64', 'arm64'), self.bundles):
            self.bundle(path, arch)
        self.sums = self.base / 'SHA256SUMS'
        self.checksums()
        self.probes = []

    def bundle(self, path, arch, commit=COMMIT, unsafe=False):
        with tarfile.open(path, 'w:gz') as archive:
            payload = f'VERSION=production-like\nCOMMIT={commit}\nOS=linux\nARCH={arch}\n'.encode()
            member = tarfile.TarInfo('./metadata/manifest.env')
            member.size = len(payload)
            archive.addfile(member, io.BytesIO(payload))
            if unsafe:
                member = tarfile.TarInfo('../outside')
                member.type = tarfile.SYMTYPE
                member.linkname = '/etc/passwd'
                archive.addfile(member)

    def checksums(self):
        self.sums.write_text(''.join(f'{publisher.digest(path)}  {name}\n'
                                     for name, path in zip(publisher.NAMES, self.bundles)))
        self.sums_sha = publisher.digest(self.sums)

    def probe(self, *args):
        self.probes.append(args)

    def run_publish(self, **kwargs):
        return publisher.publish(COMMIT, self.bundles, self.sums, self.sums_sha,
                                 self.root, kwargs.get('probe', self.probe))

    def assert_untouched(self):
        self.assertEqual((self.marker / 'untouched').read_text(), 'old artifacts')
        self.assertFalse((self.root / COMMIT).exists())
        self.assertEqual(list(self.root.iterdir()), [self.marker])

    def test_publish_and_repeat_preserve_other_commits(self):
        self.assertEqual(self.run_publish(), 'published')
        self.assertEqual(self.run_publish(), 'already-published')
        self.assertEqual(len(self.probes), 2)
        target = self.root / COMMIT
        self.assertEqual(target.stat().st_mode & 0o777, 0o755)
        for name in [*publisher.NAMES, 'SHA256SUMS']:
            self.assertEqual((target / name).stat().st_mode & 0o777, 0o644)
        self.assertEqual((self.marker / 'untouched').read_text(), 'old artifacts')

    def test_tampered_bundle_refused_before_publication(self):
        self.bundles[0].write_bytes(b'wrong')
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assert_untouched()

    def test_tampered_checksums_refused(self):
        self.sums.write_text('wrong')
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assert_untouched()

    def test_wrong_commit_or_architecture_refused(self):
        for arch, commit in [('amd64', 'c' * 40), ('arm64', COMMIT)]:
            with self.subTest(arch=arch, commit=commit):
                self.bundle(self.bundles[0], arch, commit)
                self.checksums()
                with self.assertRaises(ValueError):
                    self.run_publish()
                self.assert_untouched()

    def test_unsafe_archive_refused(self):
        self.bundle(self.bundles[0], 'amd64', unsafe=True)
        self.checksums()
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assert_untouched()

    def test_duplicate_checksums_refused(self):
        self.sums.write_text(self.sums.read_text() + self.sums.read_text())
        self.sums_sha = publisher.digest(self.sums)
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assert_untouched()

    def test_probe_failure_removes_only_new_publication(self):
        def fail(*_):
            raise ValueError('SPA fallback')
        with self.assertRaises(ValueError):
            self.run_publish(probe=fail)
        self.assert_untouched()

    def test_existing_directory_never_overwritten(self):
        self.run_publish()
        target = self.root / COMMIT
        (target / 'SHA256SUMS').write_text('keep this')
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assertEqual((target / 'SHA256SUMS').read_text(), 'keep this')

    def test_symlink_root_and_target_refused(self):
        (self.root / COMMIT).symlink_to(self.marker, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assertEqual((self.marker / 'untouched').read_text(), 'old artifacts')
        (self.root / COMMIT).unlink()
        alias = self.base / 'alias'
        alias.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(ValueError):
            publisher.publish(COMMIT, self.bundles, self.sums, self.sums_sha, alias, self.probe)
        self.assert_untouched()

    def test_public_http_success_with_spa_content_is_rejected(self):
        def fake_download(args, **_):
            Path(args[-1]).write_text('<html>RouteGate</html>')
        with patch.object(publisher.subprocess, 'run', side_effect=fake_download):
            with self.assertRaisesRegex(ValueError, 'public artifact checksum mismatch'):
                publisher.public_probe(COMMIT, self.sums_sha, {})

    def test_public_probe_checks_all_three_actual_file_hashes(self):
        files = dict(zip(publisher.NAMES, self.bundles))
        files['SHA256SUMS'] = self.sums
        downloaded = []
        def fake_download(args, **_):
            name = args[-3].rsplit('/', 1)[1]
            downloaded.append(name)
            Path(args[-1]).write_bytes(files[name].read_bytes())
        with patch.object(publisher.subprocess, 'run', side_effect=fake_download):
            publisher.public_probe(COMMIT, self.sums_sha,
                                   {name: publisher.digest(path) for name, path in zip(publisher.NAMES, self.bundles)})
        self.assertEqual(set(downloaded), set(files))


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.steps = yaml.safe_load((REPO / '.github/workflows/production-like-ops.yml').read_text())['jobs']['operate']['steps']
        self.by = {step['name']: step for step in self.steps}

    def resolve(self, body='', operation='', commit='', ref='refs/heads/main', event='issues'):
        with tempfile.NamedTemporaryFile() as output:
            env = dict(os.environ, EVENT_NAME=event, REQUEST_BODY=body,
                       DISPATCH_OPERATION=operation, DISPATCH_COMMIT=commit,
                       GITHUB_REF=ref, GITHUB_OUTPUT=output.name)
            result = subprocess.run(['bash', '-c', self.by['Resolve allow-listed operation']['run']],
                                    env=env, capture_output=True)
            return result.returncode, Path(output.name).read_text()

    def test_request_exactness_and_ref(self):
        body = f'operation=publish-bootstrap commit={COMMIT}'
        self.assertEqual(self.resolve(body), (0, f'operation=publish-bootstrap\ncommit={COMMIT}\n'))
        self.assertEqual(self.resolve(operation='publish-bootstrap', commit=COMMIT, event='workflow_dispatch')[0], 0)
        for bad in [body + '\n', body + ' ', body + ';', body.replace(COMMIT, 'main')]:
            self.assertNotEqual(self.resolve(bad)[0], 0)
        self.assertNotEqual(self.resolve(operation='publish-bootstrap', commit=COMMIT,
                                         event='workflow_dispatch', ref='refs/heads/test')[0], 0)

    def test_build_requires_verified_main_commit_and_both_architectures(self):
        for name in ['Verify update-manager target is on main with a successful CI run',
                     'Checkout pinned update-manager target', 'Verify pinned checkout is in main',
                     'Build pinned Manager update bundle']:
            self.assertIn('publish-bootstrap', self.by[name]['if'])
        self.assertIn('amd64 arm64', self.by['Build pinned Manager update bundle']['env']['ARCHITECTURES'])

    def test_remote_dispatch_copies_only_publication_files_and_cleans_on_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            for name, code in {
                'scp': '#!/bin/bash\nprintf "%s\\n" "$*" >> "$SCP_LOG"\nexit "${COPY_RC:-0}"\n',
                'ssh': '#!/bin/bash\nprintf "%s\\n" "$*" >> "$SSH_LOG"\nexit "${REMOTE_RC:-0}"\n',
            }.items():
                path = base / name
                path.write_text(code)
                path.chmod(0o755)
            for copy_rc, remote_rc in [('0', '0'), ('0', '7'), ('2', '0')]:
                (base / 'out').write_text('')
                (base / 'scp.log').write_text('')
                (base / 'ssh.log').write_text('')
                env = dict(os.environ, PATH=f'{base}:{os.environ["PATH"]}', OPERATION='publish-bootstrap',
                           MANAGER_COMMIT=COMMIT, BOOTSTRAP_CHECKSUMS_SHA='d' * 64,
                           MANAGER_BUNDLE='amd64.tar.gz', BOOTSTRAP_ARM64='arm64.tar.gz',
                           BOOTSTRAP_CHECKSUMS='SHA256SUMS', RUNNER_TEMP=temp, GITHUB_RUN_ID='42',
                           ROUTEGATE_HOST='test-host', ROUTEGATE_USER='test-user', GITHUB_OUTPUT=str(base / 'out'),
                           SCP_LOG=str(base / 'scp.log'), SSH_LOG=str(base / 'ssh.log'),
                           COPY_RC=copy_rc, REMOTE_RC=remote_rc)
                result = subprocess.run(['bash', '-c', self.by['Run allow-listed remote operation']['run']],
                                        env=env, capture_output=True)
                self.assertEqual(result.returncode, 0)
                self.assertIn('rc=' + (copy_rc if copy_rc != '0' else remote_rc), (base / 'out').read_text())
                log = (base / 'scp.log').read_text()
                self.assertNotIn('update-manager.sh', log)
                self.assertEqual(len(log.splitlines()), 4 if copy_rc == '0' else 1)
                ssh = (base / 'ssh.log').read_text()
                self.assertIn('rm -f', ssh)
                if copy_rc == '0':
                    self.assertIn('sudo flock', ssh)
                    self.assertIn('python3', ssh)


if __name__ == '__main__':
    unittest.main()
