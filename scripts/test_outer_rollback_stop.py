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

    def test_manager_database_outcome_reaches_real_helper(self):
        source = (ROOT / 'production-like-update-manager.sh').read_text()
        functions = []
        for name in ('rollback_database', 'on_error'):
            match = re.search(r'^' + name + r'\(\) \{\n.*?^\}', source, re.M | re.S)
            self.assertIsNotNone(match)
            functions.append(match.group(0))
        for db_mutated, restore_rc in ((1, 75), (1, 0), (0, 0)):
            with self.subTest(db_mutated=db_mutated, restore_rc=restore_rc):
                with tempfile.TemporaryDirectory() as tmp:
                    script = r'''
set -u
source "$1/routegate-update-core.sh"
source "$1/routegate-update-role.sh"
RG_UPDATE_ROOT=$2/host
BACKUP_DIR=$2/backup
trace=$2/trace
DB_MAY_BE_MUTATED=$3
fixture_restore_rc=$4
DB_URL=fixture
CURRENT_SCHEMA=000133_old
STAGE=fixture
MUTATED=1
mkdir -p "$BACKUP_DIR" "$2/archive/migrations" "$2/frontend"
printf 'old migration' >"$2/archive/migrations/000133_old.up.sql"
printf 'old frontend' >"$2/frontend/index.html"
tar -czf "$BACKUP_DIR/manager-migrations.tar.gz" -C "$2/archive" migrations
tar -czf "$BACKUP_DIR/frontend.tar.gz" -C "$2/frontend" .
printf 'ROLE=management\n' >"$BACKUP_DIR/role.meta"
for file in routegate-manager routegate-manager.service manager.env routegate.pgdump; do
  printf 'old' >"$BACKUP_DIR/$file"
done
mkdir -p "$RG_UPDATE_ROOT/usr/local/bin" "$RG_UPDATE_ROOT/etc/systemd/system" "$RG_UPDATE_ROOT/etc/routegate"
printf 'new' >"$RG_UPDATE_ROOT/usr/local/bin/routegate-manager"
# Host boundaries are stubbed; real handler, DB rollback and file helper run.
systemctl() { printf 'systemctl %s\n' "$*" >>"$trace"; }
pg_restore() { printf 'pg_restore\n' >>"$trace"; return "$fixture_restore_rc"; }
psql() { if [[ "$*" == *'count(*)'* ]]; then printf '0\n'; else printf '%s\n' "$CURRENT_SCHEMA"; fi; }
log() { printf '%s\n' "$*"; }
manager_healthy() { printf 'health\n' >>"$trace"; }
rg_update_secure_manager_migrations() { return 0; }
''' + '\n'.join(functions) + '\ntrap on_error ERR\nfalse\n'
                    result = subprocess.run(['bash', '-c', script, 'fixture', str(ROOT), tmp,
                                             str(db_mutated), str(restore_rc)],
                                            capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertEqual((pathlib.Path(tmp) / 'host/usr/local/bin/routegate-manager').read_text(), 'old')
                    self.assertTrue((pathlib.Path(tmp) / 'backup/routegate.pgdump').is_file())
                    trace = (pathlib.Path(tmp) / 'trace').read_text()
                    failed = db_mutated and restore_rc
                    if failed:
                        self.assertNotIn('systemctl start', trace)
                        self.assertNotIn('systemctl daemon-reload', trace)
                        self.assertNotIn('health', trace)
                        self.assertIn('ROLLBACK INCOMPLETE', result.stdout)
                        self.assertNotIn('ROLLBACK COMPLETE', result.stdout)
                    else:
                        self.assertIn('systemctl start', trace)
                        self.assertIn('health', trace)
                        self.assertIn('ROLLBACK COMPLETE', result.stdout)
                    self.assertEqual(trace.count('pg_restore'), db_mutated)


if __name__ == '__main__':
    unittest.main()


class FileRollbackStartupTests(unittest.TestCase):
    def test_real_file_failures_withhold_startup(self):
        for role in ('core', 'management'):
            for damage in ('none', 'archive', 'environment'):
                with self.subTest(role=role, damage=damage), tempfile.TemporaryDirectory() as tmp:
                    script = r'''source "$1/routegate-update-core.sh"
source "$1/routegate-update-role.sh"
RG_UPDATE_ROOT=$2/host
backup=$2/backup
trace=$2/trace
mkdir -p "$backup" "$2/archive/migrations" "$2/frontend"
printf 'migration' > "$2/archive/migrations/000133_old.up.sql"
printf 'frontend' > "$2/frontend/index.html"
tar -czf "$backup/manager-migrations.tar.gz" -C "$2/archive" migrations
tar -czf "$backup/frontend.tar.gz" -C "$2/frontend" .
printf 'ROLE=management\n' > "$backup/role.meta"
for file in routegate-manager routegate-agent routegate-manager.service routegate-agent.service manager.env; do
  printf old > "$backup/$file"
done
mkdir -p "$RG_UPDATE_ROOT/usr/local/bin" "$RG_UPDATE_ROOT/etc/systemd/system" "$RG_UPDATE_ROOT/etc/routegate"
case "$4" in
  archive) printf corrupt > "$backup/frontend.tar.gz" ;;
  environment) rm "$backup/manager.env"; mkdir "$backup/manager.env" ;;
esac
systemctl() { printf '%s\n' "$*" >> "$trace"; }
rg_update_secure_manager_migrations() { return 0; }
rg_update_wait_manager() { systemctl start routegate-manager; }
rg_update_wait_agent() { systemctl start routegate-agent; }
if [[ "$3" == core ]]; then
  rg_update_restore_backup "$backup" fixture 0
else
  rg_update_restore_management_backup "$backup" fixture 0
fi
'''
                    result = subprocess.run(['bash', '-c', script, 'fixture', str(ROOT), tmp, role, damage],
                                            capture_output=True, text=True, timeout=5)
                    trace = (pathlib.Path(tmp) / 'trace').read_text()
                    self.assertEqual(result.returncode, 0 if damage == 'none' else 1, result.stderr)
                    self.assertTrue((pathlib.Path(tmp) / 'backup/routegate-manager').is_file())
                    if damage == 'none':
                        self.assertIn('start routegate-manager', trace)
                    else:
                        self.assertNotIn('start ', trace)
                        self.assertNotIn('daemon-reload', trace)
                        self.assertIn('file restore incomplete', result.stdout)
