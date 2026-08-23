import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path

from scripts import release_manifest

VERSION = "v0.2.0"
COMMIT = "a" * 40
BUILD_DATE = "2026-08-23T12:00:00Z"
MIGRATION = "000134_distinct_tcp_listener_ports"


class ReleaseManifestTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.dist = self.root / "dist"
        self.migrations = self.root / "migrations"
        self.dist.mkdir()
        self.migrations.mkdir()
        (self.migrations / f"{MIGRATION}.up.sql").write_text("SELECT 1;\n", encoding="utf-8")

    def tearDown(self):
        self.temp.cleanup()

    def make_bundle(
        self,
        arch="amd64",
        *,
        metadata_version=VERSION,
        unsafe=False,
        include_update_core=True,
    ):
        stage = self.root / f"stage-{arch}"
        for directory in ("bin", "frontend", "metadata", "manager/migrations", "tools"):
            (stage / directory).mkdir(parents=True, exist_ok=True)
        (stage / "bin/routegate-manager").write_text("manager", encoding="utf-8")
        (stage / "bin/routegate-agent").write_text("agent", encoding="utf-8")
        (stage / "frontend/index.html").write_text("html", encoding="utf-8")
        (stage / f"manager/migrations/{MIGRATION}.up.sql").write_text(
            "SELECT 1;\n", encoding="utf-8"
        )
        if include_update_core:
            (stage / "tools/routegate-update-core.sh").write_text(
                "#!/usr/bin/env bash\n", encoding="utf-8"
            )
        (stage / "metadata/manifest.env").write_text(
            "\n".join(
                [
                    "FORMAT_VERSION=1",
                    f"VERSION={metadata_version}",
                    f"COMMIT={COMMIT}",
                    f"BUILD_DATE={BUILD_DATE}",
                    "OS=linux",
                    f"ARCH={arch}",
                    "",
                ]
            ),
            encoding="utf-8",
        )
        bundle = self.dist / f"routegate-{VERSION}-linux-{arch}.tar.gz"
        with tarfile.open(bundle, "w:gz") as archive:
            archive.add(stage, arcname=".")
            if unsafe:
                entry = tarfile.TarInfo("../escape")
                entry.size = 1
                archive.addfile(entry, io.BytesIO(b"x"))
        return bundle

    def write_checksums(self):
        lines = []
        for bundle in sorted(self.dist.glob("routegate-*.tar.gz")):
            lines.append(f"{release_manifest.sha256_file(bundle)}  {bundle.name}\n")
        (self.dist / "SHA256SUMS").write_text("".join(lines), encoding="utf-8")

    def build(self):
        manifest = release_manifest.build_manifest(
            self.dist, VERSION, COMMIT, BUILD_DATE, self.migrations
        )
        path = self.dist / "release-manifest.json"
        release_manifest.write_manifest(path, manifest)
        return path

    def test_build_and_verify(self):
        self.make_bundle("amd64")
        self.make_bundle("arm64")
        self.write_checksums()
        path = self.build()

        release_manifest.verify_manifest(path, self.dist)

        payload = json.loads(path.read_text(encoding="utf-8"))
        self.assertEqual(payload["database"]["expectedMigration"], MIGRATION)
        self.assertEqual(
            [item["arch"] for item in payload["artifacts"]], ["amd64", "arm64"]
        )

    def test_rejects_tampered_bundle(self):
        bundle = self.make_bundle()
        self.write_checksums()
        path = self.build()
        bundle.write_bytes(bundle.read_bytes() + b"tamper")

        with self.assertRaisesRegex(release_manifest.ManifestError, "SHA-256 mismatch"):
            release_manifest.verify_manifest(path, self.dist)

    def test_rejects_bundle_metadata_mismatch(self):
        self.make_bundle(metadata_version="v9.9.9")
        self.write_checksums()
        path = self.build()

        with self.assertRaisesRegex(release_manifest.ManifestError, "metadata mismatch"):
            release_manifest.verify_manifest(path, self.dist)

    def test_rejects_unsafe_bundle_path(self):
        self.make_bundle(unsafe=True)
        self.write_checksums()
        path = self.build()

        with self.assertRaisesRegex(release_manifest.ManifestError, "unsafe bundle path"):
            release_manifest.verify_manifest(path, self.dist)

    def test_rejects_bundle_without_update_core(self):
        self.make_bundle(include_update_core=False)
        self.write_checksums()
        path = self.build()

        with self.assertRaisesRegex(
            release_manifest.ManifestError, "tools/routegate-update-core.sh"
        ):
            release_manifest.verify_manifest(path, self.dist)


if __name__ == "__main__":
    unittest.main()
