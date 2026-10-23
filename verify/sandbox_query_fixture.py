"""Native read-only sandbox and old-writer interoperability, using disposable data."""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import platform
import selectors
import subprocess
import sys
import tempfile


def sandbox_command(data, scratch, command, system=None, privileged=False):
    system = system or sys.platform
    data, scratch = Path(data).resolve(), Path(scratch).resolve()
    if data == scratch or data in scratch.parents or scratch in data.parents:
        raise ValueError("data and writable scratch must be disjoint")
    if system == "darwin":
        policy = '(version 1) (allow default) (deny file-write* (subpath ' + json.dumps(str(data)) + '))'
        return ["/usr/bin/sandbox-exec", "-p", policy, *map(str, command)]
    if system == "linux":
        prefix = ["sudo", "-n"] if privileged else []
        # Root only prepares mounts. Drop to the invoking host identity before
        # the probe; a root-created user namespace maps the wrong file owner.
        capabilities = ["--cap-add", "CAP_SETUID", "--cap-add", "CAP_SETGID"] if privileged else []
        identity = ["/usr/bin/setpriv", "--reuid", str(os.getuid()), "--regid", str(os.getgid()),
                    "--clear-groups", "--"] if privileged else []
        return [*prefix, "bwrap", "--ro-bind", "/", "/", "--bind", str(scratch), str(scratch),
                "--dev", "/dev", "--proc", "/proc", "--die-with-parent", *capabilities, "--chdir", str(scratch),
                "--", *identity, *map(str, command)]
    raise RuntimeError("native sandbox queries require Linux or macOS")


def fixture_environment(scratch):
    scratch = str(Path(scratch).resolve())
    # Do not pass CI identity, shell credentials, or a real user's vault session.
    env = {key: os.environ[key] for key in ("PATH", "LANG", "LC_ALL", "SYSTEMROOT") if key in os.environ}
    env.update(HOME=scratch, TMPDIR=scratch, XDG_CONFIG_HOME=scratch + "/config",
               XDG_CACHE_HOME=scratch + "/cache", XDG_RUNTIME_DIR=scratch + "/runtime")
    return env


class Sandbox:
    def __init__(self, data, scratch):
        self.data, self.scratch = Path(data).resolve(), Path(scratch).resolve()
        self.environment = fixture_environment(scratch)
        self.privileged = False
        preflight = self.run(["/usr/bin/true"], check=False)
        if preflight.returncode and sys.platform == "linux":
            self.privileged = True
            preflight = self.run(["/usr/bin/true"], check=False)
        if preflight.returncode:
            raise RuntimeError("native sandbox unavailable: " + preflight.stderr.strip())

    def command(self, command):
        return sandbox_command(self.data, self.scratch, command, privileged=self.privileged)

    def run(self, command, check=True, timeout=15):
        return subprocess.run(self.command(command), env=self.environment, cwd=self.scratch,
                              text=True, capture_output=True, timeout=timeout, check=check)


def inventory(directory):
    result = {}
    for path in sorted(Path(directory).rglob("*")):
        info = path.lstat()
        result[str(path.relative_to(directory))] = {
            "mode": info.st_mode, "inode": info.st_ino, "mtime_ns": info.st_mtime_ns,
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None,
        }
    return result


def line(process, timeout=10):
    with selectors.DefaultSelector() as selector:
        selector.register(process.stdout, selectors.EVENT_READ)
        if not selector.select(timeout):
            raise TimeoutError("probe readiness deadline exceeded")
    value = process.stdout.readline()
    if not value:
        raise RuntimeError("probe exited before readiness: " + process.stderr.read())
    return json.loads(value)


@contextmanager
def held(command, env=None, cwd=None):
    process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True, env=env, cwd=cwd)
    try:
        assert line(process) == {"event": "acquired"}
        yield process
        _, diagnostic = process.communicate("x", timeout=10)
        if process.returncode:
            raise RuntimeError(diagnostic)
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate(timeout=10)


def native_probe(binary, data, scratch):
    data, scratch = Path(data).resolve(), Path(scratch).resolve()
    data.mkdir(mode=0o700, exist_ok=True)
    scratch.mkdir(mode=0o700, exist_ok=True)
    for name in ("lock", "write", "rename", "unlink"):
        path = data / name
        path.write_bytes(b"fixture")
        path.chmod(0o600)
    before = inventory(data)
    sandbox = Sandbox(data, scratch)
    denied = [json.loads(value) for value in sandbox.run([binary, "deny-writes", data]).stdout.splitlines()]
    assert len(denied) == 5 and all(item["denied"] for item in denied), denied
    lock = data / "lock"
    with held([str(binary), "hold-write", str(lock)]):
        assert json.loads(sandbox.run([binary, "try-read", lock]).stdout) == {"event": "busy"}
        assert json.loads(sandbox.run([binary, "cancel-read", lock]).stdout) == {"event": "deadline_exceeded"}
    with held(sandbox.command([binary, "hold-read", lock]), sandbox.environment, scratch):
        writer = subprocess.run([binary, "try-write", lock], text=True, capture_output=True, check=True, timeout=10)
        assert json.loads(writer.stdout) == {"event": "busy"}
        assert json.loads(sandbox.run([binary, "try-read", lock]).stdout) == {"event": "acquired"}
    writer = subprocess.run([binary, "try-write", lock], text=True, capture_output=True, check=True, timeout=10)
    assert json.loads(writer.stdout) == {"event": "acquired"}
    assert inventory(data) == before, "read-only probe changed fixture"
    return {"os": sys.platform, "kernel": platform.release(), "arch": platform.machine(),
            "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            "checks": denied, "old_writer_blocks_reader": True, "reader_blocks_old_writer": True,
            "concurrent_readers": True, "cancellation": True, "close_releases_lock": True,
            "inventory_unchanged": True}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--probe", required=True, type=Path)
    parser.add_argument("--artifacts", type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="devtools-sandbox-query-") as temporary:
        root = Path(temporary).resolve()
        result = native_probe(args.probe.resolve(), root / "data", root / "scratch")
    if args.artifacts:
        args.artifacts.mkdir(parents=True, exist_ok=True)
        (args.artifacts / "native-lock.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
