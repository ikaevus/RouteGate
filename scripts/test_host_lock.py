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

class RecoveryLockTests(unittest.TestCase):
    def test_default_lock_identity_matches_updater(self):
        source = (ROOT / 'routegate-update-transaction.sh').read_text()
        assignment = re.search(r'^LOCK_FILE=.*$', source, re.M).group(0)
        result = subprocess.run(['bash', '-c',
            'unset RG_UPDATE_LOCK_FILE; source "$1/routegate-recovery"; '
            'recovery_update=$UPDATE_LOCK_FILE; ' + assignment +
            '; [[ "$recovery_update" == "$LOCK_FILE" ]]', 'fixture', str(ROOT)],
            capture_output=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_update_recovery_and_legacy_contention(self):
        source = (ROOT / 'routegate-update-transaction.sh').read_text()
        acquire = re.search(r'^acquire_lock\(\) \{\n.*?^\}', source, re.M | re.S).group(0)
        for holder_kind, contender_kind in (
            ('update', 'recovery'), ('recovery', 'update'),
            ('legacy', 'recovery'), ('recovery', 'legacy'),
            ('recovery', 'recovery'),
        ):
            with self.subTest(holder=holder_kind, contender=contender_kind), tempfile.TemporaryDirectory() as tmp:
                directory = pathlib.Path(tmp)
                locks = [directory / name for name in ('update.lock', 'recovery.lock')]
                for lock in locks:
                    lock.write_bytes(b'fixture sentinel\n')
                inodes = [lock.stat().st_ino for lock in locks]

                def command(kind, hold, label):
                    action = 'printf "ready\\n"; read -r finish' if hold else 'printf admitted'
                    prefix = 'set -eu\ncd "$1"\n'
                    if kind == 'recovery':
                        # Execute the real main and lock setup. Replace only the
                        # privileged operation and root check, never flock.
                        script = prefix + '''RG_UPDATE_LOCK_FILE="$PWD/update.lock"
source "$2/routegate-recovery"
require_root() { :; }
LOCK_FILE="$PWD/recovery.lock"
LOG_FILE="$PWD/$3.log"
restart_services() { ''' + action + '; }\nmain restart-services'
                    elif kind == 'update':
                        script = prefix + 'LOCK_FILE="$PWD/update.lock"\ndie() { exit 1; }\n' + acquire + '\nacquire_lock\n' + action
                    else:
                        # Original recovery lock identity, with a real flock.
                        script = prefix + 'exec 9>>recovery.lock\nflock -n 9 || exit 1\n' + action
                    return ['bash', '-c', script, 'fixture', tmp, str(ROOT), label]

                holder = subprocess.Popen(command(holder_kind, True, 'holder'),
                                          stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                try:
                    with selectors.DefaultSelector() as selector:
                        selector.register(holder.stdout, selectors.EVENT_READ)
                        self.assertTrue(selector.select(5), 'holder did not acquire lock')
                    self.assertEqual(holder.stdout.readline(), b'ready\n')
                    contender = subprocess.run(command(contender_kind, False, 'contender'),
                                               capture_output=True, timeout=5)
                    self.assertEqual(contender.returncode, 1, contender.stderr)
                    self.assertEqual(contender.stdout, b'')
                    self.assertFalse((directory / 'contender.log').exists())
                    for lock, inode in zip(locks, inodes):
                        self.assertEqual(lock.read_bytes(), b'fixture sentinel\n')
                        self.assertEqual(lock.stat().st_ino, inode)
                finally:
                    holder.kill()
                    holder.wait(timeout=5)
                    holder.stdin.close()
                    holder.stdout.close()
                    holder.stderr.close()
                successor = subprocess.run(command(contender_kind, False, 'successor'),
                                           capture_output=True, timeout=5)
                self.assertEqual(successor.returncode, 0, successor.stderr)
                self.assertEqual(successor.stdout, b'admitted')
