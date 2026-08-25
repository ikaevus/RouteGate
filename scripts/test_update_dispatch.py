#!/usr/bin/env python3

import importlib.util
import io
import os
import stat
import tempfile
import types
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "routegate-update-dispatch.py"
spec = importlib.util.spec_from_file_location("routegate_update_dispatch", MODULE_PATH)
dispatch = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(dispatch)

JOB_ID = "123e4567-e89b-42d3-a456-426614174000"


class DispatchTests(unittest.TestCase):
    def make_candidate(self, root: Path, *, extra: str | None = None) -> Path:
        job_dir = root / JOB_ID
        job_dir.mkdir(parents=True, mode=0o700)
        for name in dispatch.FIXED_ASSETS:
            (job_dir / name).write_text("fixture\n", encoding="utf-8")
            (job_dir / name).chmod(0o600)
        bundle = job_dir / "routegate-v1.2.3-linux-amd64.tar.gz"
        bundle.write_bytes(b"bundle")
        bundle.chmod(0o600)
        if extra:
            (job_dir / extra).write_text("unexpected", encoding="utf-8")
        return job_dir

    def current_user(self):
        return types.SimpleNamespace(pw_uid=os.getuid())

    def test_reconstructs_exact_canonical_candidate(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "update-staging"
            root.mkdir(mode=0o700)
            self.make_candidate(root)
            with mock.patch.object(dispatch, "STAGING_ROOT", root), mock.patch.object(
                dispatch.pwd, "getpwnam", return_value=self.current_user()
            ):
                paths = dispatch.reconstruct_candidate(JOB_ID)
            self.assertEqual(paths[0].name, "release-manifest.json")
            self.assertEqual(paths[3].name, "routegate-v1.2.3-linux-amd64.tar.gz")
            self.assertEqual(len(paths), 5)

    def test_rejects_unexpected_entry(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "update-staging"
            root.mkdir(mode=0o700)
            self.make_candidate(root, extra="evil")
            with mock.patch.object(dispatch, "STAGING_ROOT", root), mock.patch.object(
                dispatch.pwd, "getpwnam", return_value=self.current_user()
            ):
                with self.assertRaises(dispatch.DispatchError):
                    dispatch.reconstruct_candidate(JOB_ID)

    def test_rejects_symlinked_required_asset(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "update-staging"
            root.mkdir(mode=0o700)
            job_dir = self.make_candidate(root)
            target = job_dir / "target"
            target.write_text("x", encoding="utf-8")
            manifest = job_dir / "release-manifest.json"
            manifest.unlink()
            manifest.symlink_to(target)
            with mock.patch.object(dispatch, "STAGING_ROOT", root), mock.patch.object(
                dispatch.pwd, "getpwnam", return_value=self.current_user()
            ):
                with self.assertRaises(dispatch.DispatchError):
                    dispatch.reconstruct_candidate(JOB_ID)

    def test_rejects_multiple_bundles(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "update-staging"
            root.mkdir(mode=0o700)
            job_dir = self.make_candidate(root)
            (job_dir / "routegate-v1.2.4-linux-amd64.tar.gz").write_bytes(b"other")
            with mock.patch.object(dispatch, "STAGING_ROOT", root), mock.patch.object(
                dispatch.pwd, "getpwnam", return_value=self.current_user()
            ):
                with self.assertRaises(dispatch.DispatchError):
                    dispatch.reconstruct_candidate(JOB_ID)

    def test_fixed_apply_invocation_and_path(self):
        paths = tuple(Path("/var/lib/routegate-manager/update-staging") / JOB_ID / name for name in (
            "release-manifest.json",
            "release-manifest.attestation.json",
            "SHA256SUMS",
            "routegate-v1.2.3-linux-amd64.tar.gz",
            "release-bundles.attestation.json",
        ))
        secure = types.SimpleNamespace(st_mode=stat.S_IFREG | 0o755, st_uid=0)
        with mock.patch.object(dispatch.os, "lstat", return_value=secure), mock.patch.object(
            dispatch.subprocess, "run", return_value=types.SimpleNamespace(returncode=0)
        ) as run:
            dispatch.apply_candidate(paths)
        command = run.call_args.args[0]
        self.assertEqual(command[0], "/usr/local/lib/routegate/update/routegate-update-verified.sh")
        self.assertEqual(command[1], "apply")
        self.assertEqual(command[-2:], ["--role", "auto"])
        self.assertNotIn("sudo", command)
        self.assertEqual(run.call_args.kwargs["env"]["PATH"], "/usr/sbin:/usr/bin:/sbin:/bin")

    def test_root_dispatcher_releases_safe_manager_apply_pin(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "update-staging"
            pin_root = root / ".apply-pins"
            pin_root.mkdir(parents=True, mode=0o700)
            pin = pin_root / JOB_ID
            pin.write_text("apply-in-flight\n", encoding="utf-8")
            pin.chmod(0o600)
            with mock.patch.object(dispatch, "STAGING_ROOT", root), mock.patch.object(
                dispatch.pwd, "getpwnam", return_value=self.current_user()
            ):
                dispatch.release_apply_pin(JOB_ID)
            self.assertFalse(pin.exists())

    def test_root_dispatcher_does_not_follow_symlinked_pin_root(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            root = base / "update-staging"
            root.mkdir(mode=0o700)
            outside = base / "outside"
            outside.mkdir(mode=0o700)
            target = outside / JOB_ID
            target.write_text("must-remain\n", encoding="utf-8")
            (root / ".apply-pins").symlink_to(outside, target_is_directory=True)
            with mock.patch.object(dispatch, "STAGING_ROOT", root), mock.patch.object(
                dispatch.pwd, "getpwnam", return_value=self.current_user()
            ):
                dispatch.release_apply_pin(JOB_ID)
            self.assertTrue(target.exists())

    def test_canonical_request_parser(self):
        stdin = types.SimpleNamespace(buffer=io.BytesIO((JOB_ID + "\n").encode("ascii")))
        with mock.patch.object(dispatch.sys, "stdin", stdin):
            self.assertEqual(dispatch.read_request(), JOB_ID)

    def test_rejects_request_variants(self):
        variants = [
            JOB_ID.upper() + "\n",
            " " + JOB_ID + "\n",
            JOB_ID + " \n",
            JOB_ID + "\nextra\n",
            '{"job":"' + JOB_ID + '"}\n',
            "/tmp/anything\n",
            "https://example.invalid/release\n",
            "a" * 65 + "\n",
        ]
        for value in variants:
            with self.subTest(value=value[:20]):
                stdin = types.SimpleNamespace(buffer=io.BytesIO(value.encode("ascii")))
                with mock.patch.object(dispatch.sys, "stdin", stdin):
                    with self.assertRaises(dispatch.DispatchError):
                        dispatch.read_request()

    def test_systemd_contract_is_local_and_manager_remains_unprivileged(self):
        socket_unit = (ROOT / "deploy/systemd/routegate-update-dispatch.socket").read_text(encoding="utf-8")
        service_unit = (ROOT / "deploy/systemd/routegate-update-dispatch@.service").read_text(encoding="utf-8")
        manager_unit = (ROOT / "deploy/systemd/routegate-manager.service").read_text(encoding="utf-8")
        self.assertIn("ListenStream=/run/routegate/update-dispatch.sock", socket_unit)
        self.assertIn("SocketGroup=routegate", socket_unit)
        self.assertIn("SocketMode=0660", socket_unit)
        self.assertNotIn("ListenStream=0.0.0.0", socket_unit)
        self.assertIn("User=root", service_unit)
        self.assertIn("ExecStart=/usr/local/lib/routegate/update/routegate-update-dispatch.py", service_unit)
        self.assertIn("User=routegate", manager_unit)
        self.assertIn("NoNewPrivileges=true", manager_unit)
        self.assertNotIn("sudo", manager_unit.lower())


if __name__ == "__main__":
    unittest.main()
