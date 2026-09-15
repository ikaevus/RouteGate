#!/usr/bin/env python3

import importlib.util
import os
import tempfile
import types
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "routegate_maintenance_dispatch", ROOT / "scripts" / "routegate-maintenance-dispatch.py"
)
DISPATCH = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(DISPATCH)


class MaintenanceDispatchTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        root = Path(self.temporary.name)
        self.backups = root / "backups"
        self.plans = root / "plans"
        self.backups.mkdir(mode=0o700)
        self.plans.mkdir(mode=0o700)
        self.patches = [
            mock.patch.object(DISPATCH, "ROOT_UID", os.getuid()),
            mock.patch.object(DISPATCH, "BACKUP_ROOT", self.backups),
            mock.patch.object(DISPATCH, "PLAN_ROOT", self.plans),
        ]
        for patcher in self.patches:
            patcher.start()

    def tearDown(self):
        for patcher in reversed(self.patches):
            patcher.stop()
        self.temporary.cleanup()

    def backup(self, timestamp, contents=b"backup"):
        name = f"rg96-{'a' * 40}-{timestamp}"
        path = self.backups / name
        path.mkdir(mode=0o700)
        (path / "payload").write_bytes(contents)
        return name

    def test_backup_cleanup_preserves_newest_even_when_every_backup_is_old(self):
        older = self.backup("20250101T000000Z", b"older")
        newest = self.backup("20250102T000000Z", b"newest")
        now = DISPATCH.datetime(2026, 9, 15, tzinfo=DISPATCH.timezone.utc).timestamp()
        with mock.patch.object(DISPATCH.time, "time", return_value=now):
            analyzed = DISPATCH.analyze("platform_rollback_backups")
            self.assertEqual(analyzed["candidateCount"], 1)
            plan = DISPATCH.read_plan(analyzed["token"])
            self.assertEqual(plan["candidateNames"], [older])
            result = DISPATCH.cleanup_backups(plan)
            self.assertEqual(result["deletedCount"], 1)
            self.assertFalse((self.backups / older).exists())
            self.assertTrue((self.backups / newest).is_dir())

    def test_canonical_backup_symlink_fails_closed(self):
        target = Path(self.temporary.name) / "outside"
        target.mkdir()
        name = f"rg96-{'b' * 40}-20250101T000000Z"
        (self.backups / name).symlink_to(target, target_is_directory=True)
        with self.assertRaises(DISPATCH.DispatchError):
            DISPATCH.safe_backups(DISPATCH.time.time())

    def configure_prometheus(self, override_text):
        root = Path(self.temporary.name)
        install_state = root / "install-state.env"
        config = root / "routegate.yml"
        token = root / "routegate.token"
        storage = root / "prometheus-storage"
        override_dir = root / "prometheus.service.d"
        override = override_dir / "routegate.conf"
        install_state.write_text("PROMETHEUS_MANAGED=1\n", encoding="utf-8")
        config.write_text("global: {}\n", encoding="utf-8")
        token.write_text("secret\n", encoding="utf-8")
        storage.mkdir(mode=0o700)
        override_dir.mkdir(mode=0o700)
        override.write_text(override_text, encoding="utf-8")
        return mock.patch.multiple(
            DISPATCH,
            INSTALL_STATE=install_state,
            PROMETHEUS_CONFIG=config,
            PROMETHEUS_TOKEN=token,
            PROMETHEUS_STORAGE=storage,
            PROMETHEUS_OVERRIDE=override,
        ), override

    def test_prometheus_uses_supported_retention_flag_and_verifies_readiness(self):
        paths, override = self.configure_prometheus(DISPATCH.PROMETHEUS_OVERRIDE_WITHOUT_RETENTION)
        with paths, mock.patch.object(
            DISPATCH.pwd, "getpwnam", return_value=types.SimpleNamespace(pw_uid=os.getuid())
        ), mock.patch.object(DISPATCH.subprocess, "run") as run, mock.patch.object(
            DISPATCH, "prometheus_ready", return_value=True
        ):
            plan = {"candidateNames": ["retention_90d"]}
            result = DISPATCH.cleanup_prometheus(plan)
        self.assertEqual(result["deletedCount"], 1)
        self.assertEqual(override.read_text(encoding="utf-8"), DISPATCH.PROMETHEUS_OVERRIDE_WITH_RETENTION)
        self.assertIn("--storage.tsdb.retention.time=90d", override.read_text(encoding="utf-8"))
        self.assertEqual(run.call_args_list[0].args[0], ["systemctl", "daemon-reload"])
        self.assertEqual(run.call_args_list[1].args[0], ["systemctl", "restart", "prometheus"])

    def test_prometheus_restores_override_when_readiness_fails(self):
        original = DISPATCH.PROMETHEUS_OVERRIDE_WITHOUT_RETENTION
        paths, override = self.configure_prometheus(original)
        with paths, mock.patch.object(
            DISPATCH.pwd, "getpwnam", return_value=types.SimpleNamespace(pw_uid=os.getuid())
        ), mock.patch.object(DISPATCH.subprocess, "run"), mock.patch.object(
            DISPATCH, "prometheus_ready", return_value=False
        ), mock.patch.object(DISPATCH, "PROMETHEUS_READY_TIMEOUT_SECONDS", 0):
            with self.assertRaises(DISPATCH.DispatchError):
                DISPATCH.cleanup_prometheus({"candidateNames": ["retention_90d"]})
        self.assertEqual(override.read_text(encoding="utf-8"), original)

    def test_prometheus_verify_requires_retention_even_for_empty_plan(self):
        paths, _ = self.configure_prometheus(DISPATCH.PROMETHEUS_OVERRIDE_WITHOUT_RETENTION)
        with paths, mock.patch.object(
            DISPATCH.pwd, "getpwnam", return_value=types.SimpleNamespace(pw_uid=os.getuid())
        ), mock.patch.object(DISPATCH, "prometheus_ready", return_value=True):
            result = DISPATCH.verify({"kind": "prometheus_tsdb_retention", "candidateNames": []})
        self.assertEqual(result["remainingCount"], 1)

    def test_systemd_contract_exposes_only_local_typed_socket(self):
        socket_unit = (ROOT / "deploy/systemd/routegate-maintenance-dispatch.socket").read_text(encoding="utf-8")
        service_unit = (ROOT / "deploy/systemd/routegate-maintenance-dispatch@.service").read_text(encoding="utf-8")
        self.assertIn("ListenStream=/run/routegate/maintenance-dispatch.sock", socket_unit)
        self.assertIn("SocketGroup=routegate", socket_unit)
        self.assertIn("DirectoryMode=0755", socket_unit)
        self.assertIn("User=root", service_unit)
        self.assertIn("ProtectSystem=strict", service_unit)
        self.assertIn("IPAddressDeny=any", service_unit)
        self.assertIn("IPAddressAllow=localhost", service_unit)


if __name__ == "__main__":
    unittest.main()
