"""Root-only disposable filesystem tests; no host service/database operations."""
import os
import pathlib
import re
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent

FIXTURE = r'''
set -Eeuo pipefail
source "$1/routegate-update-core.sh"
RG_UPDATE_ROOT=$2/host
BACKUP_DIR=$2/backup
trace=$2/trace
mkdir -p "$RG_UPDATE_ROOT"/{etc/routegate,etc/systemd/system,usr/local/bin,var/lib/routegate-credential-mode} "$BACKUP_DIR"
printf 'ROUTEGATE_DATABASE_MIGRATION_MODE=external\n' > "$RG_UPDATE_ROOT/etc/routegate/manager.env"
printf 'unit' > "$RG_UPDATE_ROOT/etc/systemd/system/routegate-manager.service"
printf 'binary' > "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager"
cp "$RG_UPDATE_ROOT/etc/routegate/manager.env" "$BACKUP_DIR/manager.env"
cp "$RG_UPDATE_ROOT/etc/systemd/system/routegate-manager.service" "$BACKUP_DIR/routegate-manager.service"
cp "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager" "$BACKUP_DIR/routegate-manager"
pin=$RG_UPDATE_ROOT/var/lib/routegate-credential-mode/restore.pin
{
  printf 'FORMAT_VERSION=1\nMODE=external\n'
  for entry in ENV:etc/routegate/manager.env UNIT:etc/systemd/system/routegate-manager.service BINARY:usr/local/bin/routegate-manager; do
    digest=$(sha256sum "$RG_UPDATE_ROOT/${entry#*:}")
    printf '%s_SHA256=%s\n' "${entry%%:*}" "${digest%% *}"
  done
} > "$pin"
chmod 0600 "$pin"
systemctl() {
  if [[ $1 == show ]]; then
    [[ $3 != --value ]] && return 1
    if [[ $2 == --property=FragmentPath ]]; then
      if [[ $SCENARIO == fragment ]]; then printf '/run/systemd/system/routegate-manager.service\n';
      else printf '/etc/systemd/system/routegate-manager.service\n'; fi
    elif [[ $SCENARIO == dropin ]]; then printf '/etc/systemd/system/routegate-manager.service.d/override.conf\n'; fi
    [[ $SCENARIO != systemctl_failure ]]
  else printf '%s\n' "$*" >> "$trace"; fi
}
SCENARIO=$3
case "$SCENARIO" in
  conversion_pending) mkdir "${pin%/*}/conversion.pending" ;;
  pending_file) touch "${pin%/*}/conversion.pending" ;;
  pending_link) ln -s "$2/absent" "${pin%/*}/conversion.pending" ;;
  pending_dropin) mkdir -p "$RG_UPDATE_ROOT/etc/systemd/system/routegate-manager.service.d" ;;
  global_dropin) mkdir -p "$RG_UPDATE_ROOT/run/systemd/system/service.d" ;;
  prefix_dropin) mkdir -p "$RG_UPDATE_ROOT/usr/lib/systemd/system/routegate-.service.d" ;;
  nul) printf '\0' >> "$pin" ;;
  no_newline) truncate -s -1 "$pin" ;;
  extra_newline) printf '\n' >> "$pin" ;;
  missing_env) rm "$RG_UPDATE_ROOT/etc/routegate/manager.env" ;;
  unmarked_env) printf 'owner' > "$RG_UPDATE_ROOT/etc/routegate/manager.env" ;;
  current_binary) printf 'other' > "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager" ;;
  backup_binary) printf 'old' > "$BACKUP_DIR/routegate-manager" ;;
  backup_unit) printf 'old' > "$BACKUP_DIR/routegate-manager.service" ;;
  backup_env) printf 'old' > "$BACKUP_DIR/manager.env" ;;
  missing_pin) rm "$pin" ;;
  malformed) printf 'FORMAT_VERSION=1\nMODE=external\n' > "$pin" ;;
  unknown) printf 'EXTRA=yes\n' >> "$pin" ;;
  duplicate) cat "$pin" > "$2/copy"; cat "$2/copy" >> "$pin" ;;
  injection) printf '$(touch %s/executed)\n' "$2" > "$pin" ;;
  mode) chmod 0644 "$pin" ;;
  owner) chown 65534 "$pin" ;;
  parent_mode) chmod 0777 "${pin%/*}" ;;
  ancestor_mode) chmod 0777 "$RG_UPDATE_ROOT/var" ;;
  parent_link) mv "${pin%/*}" "$2/record"; ln -s "$2/record" "${pin%/*}" ;;
  pin_link) mv "$pin" "$2/record"; ln -s "$2/record" "$pin" ;;
  pin_hardlink) ln "$pin" "$2/link" ;;
  backup_link) rm "$BACKUP_DIR/routegate-manager"; ln -s "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager" "$BACKUP_DIR/routegate-manager" ;;
  backup_hardlink) rm "$BACKUP_DIR/routegate-manager"; ln "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager" "$BACKUP_DIR/routegate-manager" ;;
esac
'''


class RestoreModeRecordTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise RuntimeError('run this filesystem fixture as root (sudo python3 -m unittest scripts/test_restore_mode_record.py)')

    def test_record_is_strict_and_never_evaluated(self):
        cases = ('conversion_pending', 'pending_file', 'pending_link', 'equal', 'missing_env', 'unmarked_env', 'current_binary', 'backup_binary',
                 'backup_unit', 'backup_env', 'missing_pin', 'malformed', 'unknown', 'duplicate',
                 'injection', 'mode', 'owner', 'parent_mode', 'ancestor_mode', 'parent_link',
                 'pin_link', 'pin_hardlink', 'backup_link', 'backup_hardlink', 'dropin',
                 'nul', 'no_newline', 'extra_newline', 'fragment', 'systemctl_failure', 'pending_dropin', 'global_dropin', 'prefix_dropin')
        for case in cases:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as tmp:
                result = subprocess.run(['bash', '-c', FIXTURE + '\nrg_update_check_manager_restore_environment "$BACKUP_DIR"',
                                         'fixture', str(ROOT), tmp, case], capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0 if case == 'equal' else 1, result.stderr)
                if case != 'equal':
                    self.assertIn('credential-mode restore pin refused', result.stderr)
                self.assertFalse((pathlib.Path(tmp) / 'executed').exists())
                self.assertFalse((pathlib.Path(tmp) / 'trace').exists())

    def test_outer_handlers_refuse_before_mutation(self):
        for filename, handler, stop in (
            ('deploy-production-like-bundle.sh', 'rollback', 'stop agent manager'),
            ('production-like-update-manager.sh', 'on_error', 'stop manager'),
        ):
            match = re.search(r'^' + handler + r'\(\) \{\n.*?^\}', (ROOT / filename).read_text(), re.M | re.S)
            self.assertIsNotNone(match)
            for case in ('conversion_pending', 'equal', 'missing_env', 'unmarked_env', 'backup_binary', 'missing_pin', 'dropin'):
                for db_mutated in (0, 1):
                    with self.subTest(handler=handler, case=case, db=db_mutated), tempfile.TemporaryDirectory() as tmp:
                        script = FIXTURE + r'''
DB_MAY_BE_MUTATED=$4
MUTATED=1
DB_URL=fixture
STAGE=fixture
RG_UPDATE_AGENT_SERVICE=agent
RG_UPDATE_MANAGER_SERVICE=manager
log() { printf '%s\n' "$*"; }
cleanup() { :; }
rollback_database_to_backup() { printf 'database\n' >> "$trace"; exit 77; }
rollback_database() { printf 'database\n' >> "$trace"; exit 77; }
restore_maintenance_dispatch() { printf 'maintenance\n' >> "$trace"; }
rg_update_restore_backup() { printf 'files\n' >> "$trace"; exit 78; }
rg_update_restore_management_backup() { printf 'files\n' >> "$trace"; exit 78; }
''' + match.group(0) + '\nset +e\nfalse\n' + handler
                        result = subprocess.run(['bash', '-c', script, 'fixture', str(ROOT), tmp, case, str(db_mutated)],
                                                capture_output=True, text=True, timeout=5)
                        trace = (pathlib.Path(tmp) / 'trace').read_text().splitlines()
                        self.assertEqual(trace[0], stop)
                        if case == 'equal':
                            self.assertEqual(result.returncode, 77 if db_mutated else 78, result.stderr)
                            self.assertEqual(trace[1:], ['database'] if db_mutated else
                                             (['maintenance', 'files'] if handler == 'rollback' else ['files']))
                        else:
                            self.assertEqual(result.returncode, 1, result.stderr)
                            self.assertEqual(trace, [stop])
                            self.assertIn('credential-mode restore pin refused', result.stderr)
                        self.assertTrue((pathlib.Path(tmp) / 'backup/manager.env').exists())

    def test_file_helpers_refuse_before_database_and_files(self):
        for helper in ('rg_update_restore_backup', 'rg_update_restore_management_backup'):
            for case in ('missing_env', 'conversion_pending'):
                with self.subTest(helper=helper, case=case), tempfile.TemporaryDirectory() as tmp:
                    script = FIXTURE + r'''
source "$1/routegate-update-role.sh"
# Backup metadata validation is outside this record test. File existence and
# the actual stop/preflight/restore ordering remain exercised.
rg_update_require_role_backup() { :; }
for name in routegate-agent routegate-agent.service manager-migrations.tar.gz frontend.tar.gz routegate.pgdump; do
  printf 'fixture' > "$BACKUP_DIR/$name"
done
pg_restore() { printf 'database\n' >> "$trace"; return 0; }
install() { printf 'files\n' >> "$trace"; return 1; }
RG_UPDATE_AGENT_SERVICE=agent
RG_UPDATE_MANAGER_SERVICE=manager
"$4" "$BACKUP_DIR" fixture 1
'''
                    result = subprocess.run(['bash', '-c', script, 'fixture', str(ROOT), tmp, case, helper],
                                            capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn('credential-mode restore pin refused', result.stderr)
                    self.assertEqual((pathlib.Path(tmp) / 'trace').read_text().splitlines(),
                                     ['stop agent manager'] if helper == 'rg_update_restore_backup' else ['stop manager'])
