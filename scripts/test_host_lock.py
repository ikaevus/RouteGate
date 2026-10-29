"""Real flock contention in disposable directories; no host updates."""
import pathlib
import re
import selectors
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent

class HostLockTests(unittest.TestCase):
    def test_contention_preserves_inode_and_release_after_process_death(self):
        source = (ROOT / 'routegate-update-transaction.sh').read_text()
        match = re.search(r'^acquire_lock\(\) \{\n.*?^\}', source, re.M | re.S)
        self.assertIsNotNone(match)
        script = 'set -eu\nLOCK_FILE=$1\ndie() { exit 73; }\n' + match.group(0)
        with tempfile.TemporaryDirectory() as tmp:
            lock = pathlib.Path(tmp) / 'transaction.lock'
            lock.write_bytes(b'fixture sentinel\n')
            inode = lock.stat().st_ino
            holder = subprocess.Popen(['bash', '-c', script + '\nacquire_lock\nprintf "ready\\n"\nread -r finish',
                                       'fixture', str(lock)], stdin=subprocess.PIPE, stdout=subprocess.PIPE)
            try:
                with selectors.DefaultSelector() as selector:
                    selector.register(holder.stdout, selectors.EVENT_READ)
                    self.assertTrue(selector.select(5), 'holder did not acquire lock')
                self.assertEqual(holder.stdout.readline(), b'ready\n')
                contender = subprocess.run(['bash', '-c', script + '\nacquire_lock\nprintf admitted',
                                            'fixture', str(lock)], capture_output=True, timeout=5)
                self.assertEqual(contender.returncode, 73, contender.stderr)
                self.assertEqual(contender.stdout, b'')
                self.assertEqual(lock.read_bytes(), b'fixture sentinel\n')
                self.assertEqual(lock.stat().st_ino, inode)
            finally:
                holder.kill()
                holder.wait(timeout=5)
                holder.stdin.close()
                holder.stdout.close()
            successor = subprocess.run(['bash', '-c', script + '\nacquire_lock\nprintf admitted',
                                        'fixture', str(lock)], capture_output=True, timeout=5)
            self.assertEqual(successor.returncode, 0, successor.stderr)
            self.assertEqual(successor.stdout, b'admitted')
            self.assertEqual(lock.read_bytes(), b'fixture sentinel\n')
            self.assertEqual(lock.stat().st_ino, inode)
