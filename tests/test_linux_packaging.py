import importlib.util
import os
from pathlib import Path
import struct
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("linux_package", ROOT / "scripts/package-linux.py")
pack = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pack)


class PackageTests(unittest.TestCase):
    def test_rejects_wrong_arch_dynamic_binary_and_mislabeled_version(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "polysync"
            header = bytearray(64)
            header[:6] = b"\x7fELF\x02\x01"
            struct.pack_into("<H", header, 18, 62)
            binary.write_bytes(header)
            info = Path(str(binary) + ".build-info")
            info.write_text(f"0.4.0 {pack.digest(binary)}\n")
            pack.check_binary(binary, "amd64", "0.4.0")
            with self.assertRaises(ValueError):
                pack.check_binary(binary, "arm64", "0.4.0")
            with self.assertRaises(ValueError):
                pack.check_binary(binary, "amd64", "0.4.1")
            binary.write_bytes(header + b"changed")
            with self.assertRaises(ValueError):
                pack.check_binary(binary, "amd64", "0.4.0")
            struct.pack_into("<Q", header, 32, 64)
            struct.pack_into("<HH", header, 54, 4, 1)
            binary.write_bytes(header + struct.pack("<I", 3))
            info.write_text(f"0.4.0 {pack.digest(binary)}\n")
            with self.assertRaisesRegex(ValueError, "CGO_ENABLED"):
                pack.check_binary(binary, "amd64", "0.4.0")

    def test_windows_checkout_launcher_is_packaged_with_unix_newlines(self):
        with tempfile.TemporaryDirectory() as temporary:
            source, output = Path(temporary) / "source", Path(temporary) / "output"
            source.write_bytes(b"#!/bin/sh\r\nexit 0\r\n")
            pack.copy(source, output, True, text=True)
            self.assertEqual(output.read_bytes(), b"#!/bin/sh\nexit 0\n")
            self.assertTrue(os.access(output, os.X_OK))

    def test_launcher_waits_for_readiness_and_does_not_open_on_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            log = directory / "calls"
            for name, content in {
                "systemctl": 'echo "systemctl $*" >> "$TEST_LOG"; exit "${SERVICE_EXIT:-0}"',
                "curl": 'echo curl >> "$TEST_LOG"; exit "${CURL_EXIT:-0}"',
                "xdg-open": 'echo "open $*" >> "$TEST_LOG"',
                "sleep": "exit 0",
            }.items():
                path = directory / name
                path.write_text("#!/bin/sh\n" + content + "\n")
                path.chmod(0o755)
            env = dict(os.environ, PATH=str(directory), TEST_LOG=str(log))
            launcher = ROOT / "packaging/linux/polysync-open"
            for _ in range(2):
                subprocess.run(["/bin/sh", str(launcher)], env=env, check=True)
            self.assertEqual(log.read_text().splitlines(), ["systemctl --user start polysync.service", "curl", "open http://127.0.0.1:45124"] * 2)
            log.write_text("")
            failed = subprocess.run(["/bin/sh", str(launcher)], env=dict(env, SERVICE_EXIT="1"), capture_output=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertNotIn("open", log.read_text())
            log.write_text("")
            failed = subprocess.run(["/bin/sh", str(launcher)], env=dict(env, CURL_EXIT="1"), capture_output=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertNotIn("open", log.read_text())
            self.assertIn("journalctl", failed.stderr.decode())


if __name__ == "__main__":
    unittest.main()
