"""Native package contracts; run with python -B -m unittest discover -s scripts -p test_package.py."""
import hashlib
import importlib.util
import json
import io
import tarfile
import os
from pathlib import Path
import stat
import struct
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import warnings
import zipfile

_spec = importlib.util.spec_from_file_location("devtools_package", Path(__file__).with_name("package.py"))
package = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(package)


class ArchiveContracts(unittest.TestCase):
    def test_zip_rejects_traversal_duplicates_directories_and_links(self):
        with tempfile.TemporaryDirectory() as scratch:
            path = Path(scratch) / "bad.zip"
            for names, mode in [(["../devtools.exe"], stat.S_IFREG), (["devtools.exe"] * 2, stat.S_IFREG),
                                (["devtools.exe/"], stat.S_IFDIR), (["devtools.exe"], stat.S_IFLNK),
                                (["devtools.exe", "other"], stat.S_IFREG)]:
                with self.subTest(names=names, mode=mode), warnings.catch_warnings():
                    warnings.simplefilter("ignore", UserWarning)
                    with zipfile.ZipFile(path, "w") as archive:
                        for name in names:
                            entry = zipfile.ZipInfo(name)
                            entry.create_system = 3
                            entry.external_attr = (mode | 0o755) << 16
                            archive.writestr(entry, b"fixture")
                    with self.assertRaises(ValueError):
                        package.read_archive(path, "windows")

    def test_wrong_pe_architecture_fails_before_metadata_probe(self):
        body = bytearray(128)
        body[:2] = b"MZ"
        struct.pack_into("<I", body, 60, 64)
        body[64:68] = b"PE\0\0"
        struct.pack_into("<H", body, 68, 0xAA64)
        struct.pack_into("<H", body, 86, 2)
        struct.pack_into("<H", body, 88, 0x20B)
        with tempfile.TemporaryDirectory() as scratch:
            binary = Path(scratch) / "wrong.exe"
            binary.write_bytes(body)
            with patch.object(package, "command", side_effect=AssertionError("must not run")):
                with self.assertRaisesRegex(ValueError, "architecture"):
                    package.verify_binary(binary, "windows", "amd64", "v", "c")

    def test_invalid_version_is_rejected_before_build_or_output(self):
        with patch.object(package, "command", side_effect=AssertionError("must not run")):
            for version in [None, "", ".", "..", "../escape", "v;command", "v\n"]:
                with self.subTest(version=version), self.assertRaises(ValueError):
                    package.package(version, {})

    def test_build_failure_publishes_no_partial_archive(self):
        with tempfile.TemporaryDirectory() as scratch:
            output = Path(scratch) / "output"
            with patch.object(package, "command", return_value="linux\namd64"), patch.object(package.subprocess, "run", side_effect=subprocess.CalledProcessError(23, "go")):
                with self.assertRaises(subprocess.CalledProcessError):
                    package.package("0.0.0-test", dict(OUTPUT_DIR=str(output)))
            self.assertEqual(list(output.iterdir()), [])

    def test_skill_roundtrip_includes_nested_resources(self):
        with tempfile.TemporaryDirectory() as scratch:
            source = Path(scratch) / "canonical"
            (source / "references" / "nested").mkdir(parents=True)
            (source / "SKILL.md").write_bytes(b"Standalone entrypoint\n")
            (source / "references" / "nested" / "guide.md").write_bytes(b"Independent nested instructions\n")
            output = dict(os.environ, OUTPUT_DIR=str(Path(scratch) / "output"))
            artifact = package.package_skill("0.0.0-test", output, source=source / "SKILL.md")
            package.verify_skill(artifact, artifact.with_name(artifact.name + ".sha256"), source=source / "SKILL.md")
            with tarfile.open(artifact) as archive:
                self.assertEqual({member.name.rstrip("/") for member in archive}, {
                    "devtools", "devtools/SKILL.md", "devtools/references",
                    "devtools/references/nested", "devtools/references/nested/guide.md",
                })
                self.assertEqual(archive.extractfile("devtools/references/nested/guide.md").read(), b"Independent nested instructions\n")

    def test_skill_rejects_tampered_or_unsafe_members_with_matching_checksum(self):
        with tempfile.TemporaryDirectory() as scratch:
            source = Path(scratch) / "canonical"
            (source / "references").mkdir(parents=True)
            (source / "SKILL.md").write_bytes(b"Standalone entrypoint\n")
            (source / "references" / "guide.md").write_bytes(b"Canonical guide\n")
            path = Path(scratch) / "skill.tar.gz"
            checksum = path.with_name(path.name + ".sha256")
            for kind in ("tampered", "symlink", "hardlink", "duplicate", "missing", "extra", "traversal"):
                with self.subTest(kind=kind):
                    with tarfile.open(path, "w:gz") as archive:
                        for name in ("devtools", "devtools/references"):
                            directory = tarfile.TarInfo(name)
                            directory.type = tarfile.DIRTYPE
                            archive.addfile(directory)
                        for name, body in (("devtools/SKILL.md", b"Standalone entrypoint\n"),
                                           ("devtools/references/guide.md", b"Canonical guide\n")):
                            if kind == "missing" and name.endswith("guide.md"):
                                continue
                            member = tarfile.TarInfo(name)
                            if name.endswith("guide.md") and kind in ("symlink", "hardlink"):
                                member.type = tarfile.SYMTYPE if kind == "symlink" else tarfile.LNKTYPE
                                member.linkname = "outside"
                                archive.addfile(member)
                                continue
                            if name.endswith("guide.md") and kind == "tampered":
                                body = b"Different guide\n"
                            member.size = len(body)
                            archive.addfile(member, io.BytesIO(body))
                            if kind == "duplicate" and name.endswith("guide.md"):
                                archive.addfile(member, io.BytesIO(body))
                        if kind in ("extra", "traversal"):
                            archive.addfile(tarfile.TarInfo("devtools/unexpected" if kind == "extra" else "../outside"))
                    checksum.write_text(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.name + "\n", encoding="ascii")
                    with self.assertRaises(ValueError):
                        package.verify_skill(path, checksum, source=source / "SKILL.md")

    def test_skill_archive_writer_failure_publishes_no_partial_output(self):
        with tempfile.TemporaryDirectory() as scratch:
            output = Path(scratch) / "output"
            with self.assertRaises(subprocess.CalledProcessError):
                package.package_skill("0.0.0-test", dict(os.environ, OUTPUT_DIR=str(output)), source=Path(scratch) / "missing.md")
            self.assertEqual(list(output.iterdir()), [])

    def test_actual_native_archive_checksum_version_and_standalone_execution(self):
        with tempfile.TemporaryDirectory(prefix="devtools package 한글 ") as scratch:
            system, arch = package.command("go", "env", "GOHOSTOS", "GOHOSTARCH").splitlines()
            env = dict(os.environ, OUTPUT_DIR=scratch, TARGET_OS=system, TARGET_ARCH=arch, COMMIT="package-native-test")
            artifact = package.package("0.0.0-package-test", env)
            digest, name = artifact.with_name(artifact.name + ".sha256").read_text().split()
            self.assertEqual(name, artifact.name)
            self.assertEqual(digest, hashlib.sha256(artifact.read_bytes()).hexdigest())
            binary = Path(scratch) / ("standalone.exe" if system == "windows" else "standalone")
            binary.write_bytes(package.read_archive(artifact, system))
            binary.chmod(0o755)
            result = subprocess.check_output([str(binary), "version"], env=dict(os.environ, PATH="", DEVTOOLS_FORMAT="json"), text=True, encoding="utf-8")
            self.assertEqual(json.loads(result)["data"]["version"], "0.0.0-package-test")
            self.assertEqual(json.loads(result)["data"]["commit"], "package-native-test")
            with self.assertRaisesRegex(ValueError, "metadata"):
                package.verify_binary(binary, system, arch, "0.0.0-wrong", "package-native-test")


if __name__ == "__main__":
    unittest.main()
