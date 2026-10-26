"""Exercise actual outer error handlers without invoking host entrypoints."""
import pathlib
import re
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent


class OuterRollbackStopTests(unittest.TestCase):
    def test_shutdown_precedes_every_restore(self):
        for filename, handler, services in (
            ('deploy-production-like-bundle.sh', 'rollback', 'agent manager'),
            ('production-like-update-manager.sh', 'on_error', 'manager'),
        ):
            text = (ROOT / filename).read_text()
            match = re.search(r'^' + handler + r'\(\) \{\n.*?^\}', text, re.M | re.S)
            self.assertIsNotNone(match, filename)
            for db_mutated in (0, 1):
                for stop_ok in (0, 1):
                    with self.subTest(file=filename, db=db_mutated, stop_ok=stop_ok):
                        with tempfile.TemporaryDirectory() as tmp:
                            trace = pathlib.Path(tmp) / 'trace'
                            script = r'''
set -u
trace=$1
stop_ok=$2
DB_MAY_BE_MUTATED=$3
MUTATED=1
BACKUP_DIR=fixture-backup
DB_URL=fixture-url
STAGE=fixture-stage
RG_UPDATE_AGENT_SERVICE=agent
RG_UPDATE_MANAGER_SERVICE=manager
log() { :; }
cleanup() { printf 'cleanup\n' >> "$trace"; }
systemctl() { printf '%s\n' "$*" >> "$trace"; [[ "$stop_ok" == 1 ]]; }
rollback_database_to_backup() { printf 'database\n' >> "$trace"; exit 77; }
rollback_database() { printf 'database\n' >> "$trace"; exit 77; }
restore_maintenance_dispatch() { printf 'maintenance\n' >> "$trace"; }
rg_update_restore_backup() { printf 'files\n' >> "$trace"; exit 78; }
rg_update_restore_management_backup() { printf 'files\n' >> "$trace"; exit 78; }
''' + match.group(0) + '\nfalse\n' + handler + '\n'
                            result = subprocess.run(
                                ['bash', '-c', script, 'fixture', str(trace), str(stop_ok), str(db_mutated)],
                                capture_output=True, text=True, timeout=5,
                            )
                            lines = trace.read_text().splitlines()
                            self.assertEqual(lines[0], 'stop ' + services)
                            if not stop_ok:
                                self.assertEqual(result.returncode, 1, result.stderr)
                                self.assertEqual(lines, ['stop ' + services] +
                                                 (['cleanup'] if handler == 'rollback' else []))
                            else:
                                self.assertEqual(result.returncode, 77 if db_mutated else 78, result.stderr)
                                self.assertEqual(lines[1:], ['database'] if db_mutated else
                                                 (['maintenance', 'files'] if handler == 'rollback' else ['files']))


if __name__ == '__main__':
    unittest.main()
