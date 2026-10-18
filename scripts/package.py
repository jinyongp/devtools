#!/usr/bin/env python3
"""Build and verify standalone CLI archives using only Python's standard library."""
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile

TARGETS = {(system, arch) for system in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")}
TOKEN = re.compile(r"[a-zA-Z0-9._-]+\Z")


def token(value, label):
    if not value or value in (".", "..") or not TOKEN.fullmatch(value):
        raise ValueError(f"Invalid {label}")
    return value


def command(*args, env=None):
    return subprocess.check_output(args, env=env, text=True, encoding="utf-8").strip()


def verify_binary(path, system, arch, version, commit):
    body = Path(path).read_bytes()
    if system == "windows":
        if len(body) < 64 or body[:2] != b"MZ":
            raise ValueError("Package has no PE image")
        offset = struct.unpack_from("<I", body, 60)[0]
        if offset > len(body) - 26 or body[offset:offset+4] != b"PE\0\0":
            raise ValueError("Invalid PE image")
        machine = struct.unpack_from("<H", body, offset+4)[0]
        flags = struct.unpack_from("<H", body, offset+22)[0]
        magic = struct.unpack_from("<H", body, offset+24)[0]
        if machine != {"amd64": 0x8664, "arm64": 0xAA64}[arch] or magic != 0x20B or not flags & 2 or flags & 0x2000:
            raise ValueError("PE architecture or executable type mismatch")
    elif system == "linux":
        if len(body) < 20 or body[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", body, 18)[0] != {"amd64": 62, "arm64": 183}[arch]:
            raise ValueError("ELF architecture mismatch")
    elif len(body) < 8 or struct.unpack_from("<II", body) != (0xFEEDFACF, {"amd64": 0x1000007, "arm64": 0x100000C}[arch]):
        raise ValueError("Mach-O architecture mismatch")
    info = json.loads(command("go", "version", "-m", "-json", str(path)))
    settings = {item["Key"]: item["Value"] for item in info.get("Settings", [])}
    identity = f"devtools-build-v1:{version}:{commit}:{system}:{arch}:end".encode("ascii")
    if (info.get("Path") != "github.com/jinyongp/devtools/cmd/devtools" or settings.get("GOOS") != system
            or settings.get("GOARCH") != arch or settings.get("CGO_ENABLED") != "0"
            or body.count(identity) != 1):
        raise ValueError("Package Go build/version/commit metadata mismatch")
    host = command("go", "env", "GOHOSTOS", "GOHOSTARCH").splitlines()
    if host == [system, arch]:
        result = json.loads(command(str(Path(path).resolve()), "version"))
        if not result.get("ok") or result.get("data", {}).get("version") != version or result["data"].get("commit") != commit:
            raise ValueError("Package runtime version/commit mismatch")


def archive_binary(path, binary, system):
    if system == "windows":
        member = zipfile.ZipInfo("devtools.exe", (1980, 1, 1, 0, 0, 0))
        member.create_system = 3
        member.external_attr = (stat.S_IFREG | 0o755) << 16
        with zipfile.ZipFile(path, "x", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
            archive.writestr(member, Path(binary).read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    else:
        with open(path, "xb") as raw:
            with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                    member = tarfile.TarInfo("devtools")
                    member.size = Path(binary).stat().st_size
                    member.mode = 0o755
                    archive.addfile(member, io.BytesIO(Path(binary).read_bytes()))


def read_archive(path, system):
    if system == "windows":
        with zipfile.ZipFile(path) as archive:
            entries = archive.infolist()
            if len(entries) != 1:
                raise ValueError("ZIP must contain exactly one devtools.exe")
            entry = entries[0]
            mode = entry.external_attr >> 16
            if (entry.filename != "devtools.exe" or entry.is_dir() or entry.flag_bits & 1
                    or stat.S_IFMT(mode) != stat.S_IFREG or not 0 < entry.file_size <= 256 << 20):
                raise ValueError("Invalid ZIP entry")
            return archive.read(entry)
    with tarfile.open(path, "r:gz") as archive:
        entries = archive.getmembers()
        if len(entries) != 1 or entries[0].name != "devtools" or not entries[0].isreg() or not 0 < entries[0].size <= 256 << 20:
            raise ValueError("Invalid tar entry")
        return archive.extractfile(entries[0]).read()


def package(version=None, env=None):
    env = dict(os.environ if env is None else env)
    version = token(version if version is not None else env.get("VERSION"), "VERSION")
    commit = token(env.get("COMMIT", "unknown"), "COMMIT")
    defaults = command("go", "env", "GOOS", "GOARCH", env=env).splitlines()
    system, arch = env.get("TARGET_OS", defaults[0]), env.get("TARGET_ARCH", defaults[1])
    if (system, arch) not in TARGETS:
        raise ValueError("Unsupported target")
    output = Path(env.get("OUTPUT_DIR", "dist"))
    output.mkdir(parents=True, exist_ok=True)
    name = f"devtools_{version}_{system}_{arch}." + ("zip" if system == "windows" else "tar.gz")
    with tempfile.TemporaryDirectory(prefix=".devtools-package-", dir=output) as scratch:
        scratch = Path(scratch)
        binary = scratch / ("devtools.exe" if system == "windows" else "devtools")
        build_env = dict(env, CGO_ENABLED="0", GOOS=system, GOARCH=arch)
        subprocess.run(["go", "build", "-trimpath", "-ldflags", f"-s -w -X main.version={version} -X main.commit={commit} -X main.packageIdentity=devtools-build-v1:{version}:{commit}:{system}:{arch}:end", "-o", str(binary), "./cmd/devtools"], env=build_env, check=True)
        verify_binary(binary, system, arch, version, commit)
        artifact = scratch / name
        archive_binary(artifact, binary, system)
        if read_archive(artifact, system) != binary.read_bytes():
            raise ValueError("Archive binary mismatch")
        checksum = scratch / (name + ".sha256")
        checksum.write_bytes((hashlib.sha256(artifact.read_bytes()).hexdigest() + "  " + name + "\n").encode("ascii"))
        os.replace(artifact, output / name)
        os.replace(checksum, output / checksum.name)
    return output / name


def configure_output():
    # Windows redirected streams can default to an ANSI codec even when paths
    # contain Unicode. Developer command output is UTF-8 on every host.
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")


def main():
    configure_output()
    try:
        if len(sys.argv) != 1:
            raise ValueError("Usage: package.py (VERSION is required in the environment)")
        print(package())
    except ValueError as error:
        print(error, file=sys.stderr)
        return 2
    except (OSError, subprocess.SubprocessError, zipfile.BadZipFile, tarfile.TarError) as error:
        print(error, file=sys.stderr)
        return 1
    return 0


def skill_resources(source="skills/devtools/SKILL.md"):
    entrypoint = Path(source)
    if entrypoint.is_symlink() or not entrypoint.is_file():
        raise ValueError("Canonical skill entrypoint must be a regular file")
    root = entrypoint.parent
    resources = {"devtools": root}
    for path in sorted(root.rglob("*")):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError("Canonical skill resource must be a regular file or directory")
        resources["devtools/" + path.relative_to(root).as_posix()] = path
    return resources


def verify_skill(archive, checksum, source="skills/devtools/SKILL.md"):
    archive, checksum = Path(archive), Path(checksum)
    if archive.is_symlink() or checksum.is_symlink() or not archive.is_file() or not checksum.is_file():
        raise ValueError("Skill artifact must be a regular file")
    fields = checksum.read_text(encoding="ascii").split()
    if len(fields) != 2 or fields[1] != archive.name or not re.fullmatch(r"[0-9a-fA-F]{64}", fields[0]) or hashlib.sha256(archive.read_bytes()).hexdigest() != fields[0].lower():
        raise ValueError("Skill checksum mismatch")
    resources = skill_resources(source)
    with tarfile.open(archive, "r:gz") as package_file:
        members = package_file.getmembers()
        names = [member.name.rstrip("/") for member in members]
        if len(names) != len(resources) or set(names) != resources.keys():
            raise ValueError("Invalid skill archive entries")
        for name, member in zip(names, members):
            path = resources[name]
            if path.is_dir():
                if not member.isdir() or member.size != 0:
                    raise ValueError("Invalid skill archive entries")
            elif not member.isreg():
                raise ValueError("Invalid skill archive entries")
            elif member.size != path.stat().st_size or package_file.extractfile(member).read() != path.read_bytes():
                raise ValueError("Skill artifact differs from canonical source")


def package_skill(version, env=None, source="skills/devtools/SKILL.md"):
    env = dict(os.environ if env is None else env)
    version = token(version, "VERSION")
    output = Path(env.get("OUTPUT_DIR", "dist"))
    output.mkdir(parents=True, exist_ok=True)
    name = f"devtools-skill_{version}.tar.gz"
    with tempfile.TemporaryDirectory(prefix=".devtools-skill-", dir=output) as scratch:
        artifact = Path(scratch) / name
        subprocess.run(["go", "run", "./scripts/package-skill", "--source", str(source), "--output", str(artifact)], env=env, check=True)
        checksum = artifact.with_name(name + ".sha256")
        checksum.write_bytes((hashlib.sha256(artifact.read_bytes()).hexdigest() + "  " + name + "\n").encode("ascii"))
        verify_skill(artifact, checksum, source=source)
        os.replace(artifact, output / name)
        os.replace(checksum, output / checksum.name)
    return output / name


if __name__ == "__main__":
    sys.exit(main())
