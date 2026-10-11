#!/usr/bin/env python3
"""Offline installer contract tests. No real network, credentials or installation."""
import hashlib
import io
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().with_name("install-mcp.sh")


class InstallerTests(unittest.TestCase):
    def run_fixture(self, failure=""):
        with tempfile.TemporaryDirectory(prefix="mcp-installer-test-") as root:
            root = Path(root)
            fixture, commands, install = [root / x for x in ("fixture", "commands", "install")]
            for path in (fixture, commands, install):
                path.mkdir()
            osname = platform.system().lower()
            arch = "arm64" if platform.machine() in ("arm64", "aarch64") else "amd64"
            name = f"foliospace-mcp_1.03.1_{osname}_{arch}"
            payload = b'#!/bin/sh\nread input\nprintf \'%s\\n\' \'{"result":{"serverInfo":{"name":"foliospace-library","version":"1.03.1"}}}\'\n'
            if failure == "smoke":
                payload = b"#!/bin/sh\nexit 1\n"
            archive = fixture / (name + ".tar.gz")
            with tarfile.open(archive, "w:gz") as tar:
                entry = tarfile.TarInfo(name + "/foliospace-mcp")
                entry.size, entry.mode = len(payload), 0o755
                tar.addfile(entry, io.BytesIO(payload))
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            if failure == "checksum":
                digest = "0" * 64
            checksums = f"{digest}  {archive.name}\n"
            if failure == "entry":
                checksums = f"{digest}  another.tar.gz\n"
            if failure == "duplicate":
                checksums *= 2
            if failure != "missing":
                (fixture / "checksums.txt").write_text(checksums)
            # curl substitute accepts the actual command shape; no Internet needed.
            curl = commands / "curl"
            curl.write_text('#!/bin/sh\nwhile [ "$#" -gt 0 ]; do\n case "$1" in https://*) url="$1";; -o) shift; out="$1";; esac\n shift\ndone\ncp "$FIXTURE/${url##*/}" "$out"\n')
            curl.chmod(0o755)
            current = install / "foliospace-mcp"
            current.write_bytes(b"previous binary")
            env = dict(os.environ, PATH=str(commands) + os.pathsep + os.environ["PATH"],
                       FIXTURE=str(fixture), FOLIOSPACE_MCP_INSTALL_DIR=str(install),
                       FOLIOSPACE_MCP_RELEASE_BASE_URL="https://fixture.invalid",
                       FOLIOSPACE_MCP_VERSION="1.03.1")
            result = subprocess.run(["sh", str(SCRIPT)], env=env, capture_output=True)
            if failure:
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(current.read_bytes(), b"previous binary")
                self.assertFalse((install / ".foliospace-mcp.previous").exists())
            else:
                self.assertEqual(result.returncode, 0, result.stderr.decode())
                self.assertEqual(current.read_bytes(), payload)
                self.assertEqual((install / ".foliospace-mcp.previous").read_bytes(), b"previous binary")
            self.assertEqual(list(install.glob(".foliospace-mcp.candidate.*")), [])
            self.assertEqual(list(install.glob(".foliospace-mcp.backup.*")), [])

    def test_success(self):
        self.run_fixture()

    def test_fail_closed(self):
        for failure in ("missing", "entry", "duplicate", "checksum", "smoke"):
            with self.subTest(failure=failure):
                self.run_fixture(failure)


if __name__ == "__main__":
    unittest.main()
