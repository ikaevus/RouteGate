import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('publisher', Path(__file__).with_name('rg140-session-ui.py'))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)
INDEX = b'<script src="/assets/index-new.js"></script>'
FILES = {'index.html': INDEX, 'assets/index-new.js': b'new-js'}

class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'ui'
        self.root.mkdir()
        (self.root / 'index.html').write_bytes(b'old-index')
        (self.root / 'assets').mkdir()
        (self.root / 'assets/old.js').write_bytes(b'old-js')
        (self.root / 'bootstrap').mkdir()
        (self.root / 'bootstrap/keep').write_bytes(b'bootstrap')
        self.backup = Path(self.temp.name) / 'backup'

    def unchanged(self):
        self.assertEqual((self.root / 'assets/old.js').read_bytes(), b'old-js')
        self.assertEqual((self.root / 'bootstrap/keep').read_bytes(), b'bootstrap')

    def test_success_keeps_old_assets_and_backup(self):
        publisher.publish(self.root, FILES, self.backup, lambda: self.assertEqual((self.root / 'index.html').read_bytes(), INDEX))
        self.assertEqual((self.backup / 'index.html').read_bytes(), b'old-index')
        self.unchanged()

    def test_failed_verification_restores_index(self):
        def fail(): raise RuntimeError('verification_failed')
        with self.assertRaises(RuntimeError): publisher.publish(self.root, FILES, self.backup, fail)
        self.assertEqual((self.root / 'index.html').read_bytes(), b'old-index')
        self.unchanged()

    def test_interrupt_restores_index(self):
        def fail(): raise KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt): publisher.publish(self.root, FILES, self.backup, fail)
        self.assertEqual((self.root / 'index.html').read_bytes(), b'old-index')
        self.unchanged()

    def test_existing_asset_collision_is_not_overwritten(self):
        (self.root / 'assets/index-new.js').write_bytes(b'other')
        with self.assertRaises(RuntimeError): publisher.publish(self.root, FILES, self.backup, lambda: None)
        self.assertEqual((self.root / 'assets/index-new.js').read_bytes(), b'other')
        self.assertEqual((self.root / 'index.html').read_bytes(), b'old-index')

    def test_symlink_is_refused(self):
        (self.root / 'assets/index-new.js').symlink_to(self.root / 'assets/old.js')
        with self.assertRaises(RuntimeError): publisher.publish(self.root, FILES, self.backup, lambda: None)
        self.unchanged()

    def test_existing_identical_asset_is_reusable(self):
        (self.root / 'assets/index-new.js').write_bytes(b'new-js')
        publisher.publish(self.root, FILES, self.backup, lambda: None)
        self.unchanged()

    def archive(self, entries):
        bundle = Path(self.temp.name) / 'bundle.tar.gz'
        with tarfile.open(bundle, 'w:gz') as archive:
            for name, data, kind in entries:
                info = tarfile.TarInfo(name)
                info.type = kind
                info.size = len(data)
                archive.addfile(info, io.BytesIO(data))
        return bundle, hashlib.sha256(bundle.read_bytes()).hexdigest()

    def test_valid_payload(self):
        bundle, sha = self.archive([(k, v, tarfile.REGTYPE) for k, v in FILES.items()])
        self.assertEqual(publisher.payload(bundle, sha), FILES)

    def test_digest_mismatch(self):
        bundle, _ = self.archive([(k, v, tarfile.REGTYPE) for k, v in FILES.items()])
        with self.assertRaises(RuntimeError): publisher.payload(bundle, '0' * 64)

    def test_unsafe_archive_entries(self):
        for name, kind in [('../escape', tarfile.REGTYPE), ('/index.html', tarfile.REGTYPE),
                           ('bootstrap/overwrite', tarfile.REGTYPE), ('assets/link.js', tarfile.SYMTYPE),
                           ('assets/sub/file.js', tarfile.REGTYPE), ('assets', tarfile.DIRTYPE)]:
            with self.subTest(name=name):
                bundle, sha = self.archive([(name, b'bad', kind)])
                with self.assertRaises(RuntimeError): publisher.payload(bundle, sha)

    def test_missing_referenced_asset(self):
        bundle, sha = self.archive([('index.html', INDEX, tarfile.REGTYPE), ('assets/wrong.js', b'x', tarfile.REGTYPE)])
        with self.assertRaises(RuntimeError): publisher.payload(bundle, sha)

    def test_duplicate_archive_entry(self):
        bundle, sha = self.archive([('index.html', INDEX, tarfile.REGTYPE)] * 2)
        with self.assertRaises(RuntimeError): publisher.payload(bundle, sha)

if __name__ == '__main__': unittest.main()
