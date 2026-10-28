"""Root-only isolated first-publication and failure tests."""
import concurrent.futures
import os
import pathlib
import tempfile
import unittest
from unittest import mock
from scripts import rg140_pin_writer as writer

PAYLOAD = (b'FORMAT_VERSION=1\nMODE=external\n' +
           b'ENV_SHA256=' + b'a'*64 + b'\nUNIT_SHA256=' + b'b'*64 +
           b'\nBINARY_SHA256=' + b'c'*64 + b'\n')


class PinWriterTests(unittest.TestCase):
    def setUp(self):
        self.assertEqual(os.geteuid(), 0, 'run via sudo')
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        self.fd = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY)
        self.addCleanup(os.close, self.fd)
        self.directory = self.root / writer.DIRECTORY

    def test_publication_and_refusal_to_overwrite(self):
        writer.publish_first_pin(self.fd, PAYLOAD)
        pin = self.directory / 'restore.pin'
        self.assertEqual(pin.read_bytes(), PAYLOAD)
        self.assertEqual(pin.stat().st_mode & 0o777, 0o600)
        self.assertEqual(pin.stat().st_nlink, 1)
        self.assertEqual(pin.stat().st_uid, 0)
        self.assertEqual(list(self.directory.iterdir()), [pin])
        with self.assertRaises(FileExistsError):
            writer.publish_first_pin(self.fd, PAYLOAD.replace(b'a'*64, b'd'*64))
        self.assertEqual(pin.read_bytes(), PAYLOAD)

    def test_invalid_payload_does_not_create_latch(self):
        for payload in (PAYLOAD[:-1], PAYLOAD+b'\0', PAYLOAD+b'\n', b''):
            with self.subTest(payload=payload[:10]), self.assertRaises(ValueError):
                writer.publish_first_pin(self.fd, payload)
            self.assertFalse(self.directory.exists())

    def test_existing_partial_directory_is_not_adopted(self):
        self.directory.mkdir()
        with self.assertRaises(FileExistsError):
            writer.publish_first_pin(self.fd, PAYLOAD)
        self.assertEqual(list(self.directory.iterdir()), [])

    def test_symlink_is_not_followed(self):
        target = self.root / 'outside'
        target.mkdir()
        self.directory.symlink_to(target)
        with self.assertRaises(FileExistsError):
            writer.publish_first_pin(self.fd, PAYLOAD)
        self.assertEqual(list(target.iterdir()), [])

    def test_unsafe_parent(self):
        self.root.chmod(0o777)
        with self.assertRaises(PermissionError):
            writer.publish_first_pin(self.fd, PAYLOAD)
        self.assertFalse(self.directory.exists())

    def test_failure_at_each_durability_boundary_retains_latch(self):
        real_fsync = os.fsync
        for fail_at in (1, 2, 3):
            with self.subTest(fail_at=fail_at), tempfile.TemporaryDirectory() as tmp:
                fd = os.open(tmp, os.O_RDONLY | os.O_DIRECTORY)
                calls = 0
                def fail(f):
                    nonlocal calls
                    calls += 1
                    if calls == fail_at:
                        raise OSError('injected fsync failure')
                    real_fsync(f)
                try:
                    with mock.patch.object(writer.os, 'fsync', side_effect=fail):
                        with self.assertRaises(OSError):
                            writer.publish_first_pin(fd, PAYLOAD)
                    directory = pathlib.Path(tmp) / writer.DIRECTORY
                    self.assertTrue(directory.is_dir())
                    pin = directory / 'restore.pin'
                    if fail_at == 3:
                        self.assertEqual(pin.read_bytes(), PAYLOAD)
                    else:
                        self.assertFalse(pin.exists())
                    with self.assertRaises(FileExistsError):
                        writer.publish_first_pin(fd, PAYLOAD)
                finally:
                    os.close(fd)

    def test_concurrent_first_writers_have_one_winner(self):
        def publish(payload):
            try:
                writer.publish_first_pin(self.fd, payload)
                return payload
            except FileExistsError:
                return None
        other = PAYLOAD.replace(b'a'*64, b'd'*64)
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(publish, (PAYLOAD, other)))
        winners = [r for r in results if r is not None]
        self.assertEqual(len(winners), 1)
        self.assertEqual((self.directory / 'restore.pin').read_bytes(), winners[0])

    def test_failed_rename_never_publishes(self):
        with mock.patch.object(writer.os, 'rename', side_effect=OSError('rename failure')):
            with self.assertRaises(OSError):
                writer.publish_first_pin(self.fd, PAYLOAD)
        self.assertTrue(self.directory.is_dir())
        self.assertFalse((self.directory / 'restore.pin').exists())
        self.assertEqual((self.directory / 'pending.pin').read_bytes(), PAYLOAD)
