"""Installation and update scenario for the shared Linux sandbox."""

import hashlib
import json
import os
from pathlib import Path
import select
import shutil
import signal
import ssl
import subprocess
import tempfile
import threading
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer


assert os.getuid() != 0, "Use an ordinary installation user"
assert shutil.which("go") is None, "Runtime image should contain the installed app"
assert not Path("/src/go.mod").exists()
home = Path.home()
env = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])
env.update(GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_NOSYSTEM="1")
installer = ["sh", "/opt/devtools-install.sh"]
project = home / "project"
project.mkdir()


def execute(args, *, cwd=project, input=None, expected=0):
    result = subprocess.run(args, cwd=cwd, env=env, input=input, text=True,
                            capture_output=True, timeout=20)
    assert result.returncode == expected, (args, result.returncode, result.stderr)
    return result


def api(*args, input=None, expected=0, cwd=project):
    result = execute(["devtools", *args], input=input, expected=expected, cwd=cwd)
    response = json.loads(result.stdout if expected == 0 else result.stderr)
    assert response["ok"] == (expected == 0)
    assert "fixture-secret" not in result.stdout + result.stderr
    return response


def install(action, version, source="/opt/releases", expected=0):
    result = execute(installer + [action, "--version", version, "--source", source],
                     expected=expected)
    response = json.loads(result.stdout if expected == 0 else result.stderr)
    assert response["ok"] == (expected == 0)


install("install", "0.0.0-test.1")
assert shutil.which("devtools", path=env["PATH"]) == str(home / ".local/bin/devtools")
assert api("version")["data"]["version"] == "0.0.0-test.1"
install("install", "0.0.0-test.1", expected=1)
api("init", "--profile", "fixture")
api("env", "create", "local")
api("env", "create", "staging")
api("var", "set", "LEVEL", "--value", "common")
api("var", "set", "LEVEL", "--env", "local", "--value", "local")
api("var", "set", "LEVEL", "--env", "staging", "--value", "staging")
api("sec", "set", "TOKEN", "--stdin", input="fixture-secret")
api("var", "get", "TOKEN", expected=3)
api("sec", "list", "--env", "local")

# Existing project commands also run on their own.
(project / "app.py").write_text(
    "import json,os,sys\n"
    "print(json.dumps(dict(level=os.getenv('LEVEL'), "
    "secret_present=os.getenv('TOKEN')=='fixture-secret', "
    "args=sys.argv[1:],cwd=os.getcwd())))\n"
)
(project / "justfile").write_text("check:\n    @python3 app.py\n")
(project / "devtools.toml").write_text(
    'profile="fixture"\n'
    '[commands.check]\nexec=["just","check"]\ninject=true\nenv="local"\n'
    '[commands.app]\nexec=["python3","app.py"]\ninject=true\nenv="local"\n'
)
plain = json.loads(execute(["just", "check"]).stdout)
assert plain["level"] is None and not plain["secret_present"]


def verify_project(cwd=project):
    named = json.loads(execute(["devtools", "run", "check"], cwd=cwd).stdout)
    assert named["level"] == "local" and named["secret_present"]
    assert named["cwd"] == str(cwd)
    extra = json.loads(execute(["devtools", "run", "app", "--env", "staging",
                               "--", "extra space"], cwd=cwd).stdout)
    assert extra["level"] == "staging" and extra["args"] == ["extra space"]
    direct = json.loads(execute(["devtools", "run", "--", "python3", "app.py"],
                               cwd=cwd).stdout)
    assert direct["level"] == "common" and direct["secret_present"]


verify_project()
for args in (["init", "--quiet"], ["add", "."],
             ["-c", "user.name=Test", "-c", "user.email=test@example.com",
              "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "test: fixture"]):
    execute(["git", *args])
worktree = home / "worktree"
execute(["git", "worktree", "add", "--quiet", "--detach", str(worktree)])
verify_project(worktree)

profile_dir = home / ".local/share/devtools/profiles"
assert profile_dir.stat().st_mode & 0o777 == 0o700
for file in profile_dir.iterdir():
    assert file.stat().st_mode & 0o777 == 0o600
before = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
          for p in [*profile_dir.iterdir(), project / "devtools.toml"]}
binary = home / ".local/bin/devtools"
original_binary = hashlib.sha256(binary.read_bytes()).hexdigest()

# A failed update preserves both the installed program and user data.
with tempfile.TemporaryDirectory() as temporary:
    source = Path(temporary)
    for file in Path("/opt/releases").glob("*test.2*"):
        shutil.copy(file, source / file.name)
    for archive in source.glob("*.tar.gz"):
        with archive.open("ab") as stream:
            stream.write(b"corrupt")
    install("update", "0.0.0-test.2", str(source), expected=1)
assert hashlib.sha256(binary.read_bytes()).hexdigest() == original_binary

install("update", "0.0.0-test.2")
assert api("version")["data"]["version"] == "0.0.0-test.2"

# Exercise HTTPS delivery on container loopback with a test-only trust root.
with tempfile.TemporaryDirectory() as temporary:
    cert = Path(temporary) / "certificate.pem"
    key = Path(temporary) / "key.pem"
    releases = Path(temporary) / "releases"
    latest = releases / "latest/download"
    latest.mkdir(parents=True)
    for version in ("0.0.0-test.1", "0.0.0-test.2"):
        target = releases / "download" / f"v{version}"
        target.mkdir(parents=True)
        for artifact in Path("/opt/releases").glob(f"*{version}*"):
            shutil.copy(artifact, target / artifact.name)
    execute(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
             "-keyout", str(key), "-out", str(cert), "-days", "1",
             "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost"])

    class Handler(SimpleHTTPRequestHandler):
        def __init__(self, *args, **kwargs):
            super().__init__(*args, directory=str(releases), **kwargs)

        def log_message(self, *args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert, key)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    env["CURL_CA_BUNDLE"] = str(cert)
    env["DEVTOOLS_RELEASE_URL"] = f"https://localhost:{server.server_port}"
    try:
        install("update", "0.0.0-test.2", env["DEVTOOLS_RELEASE_URL"] + "/download/v0.0.0-test.2")
        download_bin = home / "download-bin"

        def released(action, *args, expected=0):
            result = execute(installer + [action, "--bin-dir", str(download_bin), *args],
                             expected=expected)
            return json.loads(result.stdout if expected == 0 else result.stderr)

        # Latest discovery resolves once, then uses the version-specific URL.
        (latest / "version.txt").write_text("0.0.0-test.1\n")
        assert released("install")["data"]["version"] == "0.0.0-test.1"
        (latest / "version.txt").write_text("0.0.0-test.2\n")
        assert released("update")["data"]["version"] == "0.0.0-test.2"
        downloaded = download_bin / "devtools"
        preserved = downloaded.read_bytes()
        (latest / "version.txt").write_text("../../invalid\n")
        assert not released("update", expected=1)["ok"]
        assert downloaded.read_bytes() == preserved
        (latest / "version.txt").unlink()
        assert not released("update", expected=1)["ok"]
        assert downloaded.read_bytes() == preserved
        assert released("update", "--version", "0.0.0-test.1")["data"]["version"] == "0.0.0-test.1"
        assert not released("update", "--source", "/opt/releases", expected=1)["ok"]
    finally:
        del env["CURL_CA_BUNDLE"]
        del env["DEVTOOLS_RELEASE_URL"]
        server.shutdown()
        server.server_close()
        worker.join()
assert hashlib.sha256(binary.read_bytes()).hexdigest() != original_binary
assert before == {p: hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in before}
verify_project()
verify_project(worktree)
install("update", "0.0.0-test.2")
assert api("version")["data"]["version"] == "0.0.0-test.2"

# Confirm the installed entrypoint forwards signals and child exit status.
child = subprocess.Popen(
    ["devtools", "run", "--", "/bin/sh", "-c",
     "trap 'exit 42' TERM; printf 'ready\\n'; while :; do sleep 1; done"],
    cwd=project, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
)
try:
    assert select.select([child.stdout], [], [], 10)[0], "Child startup timed out"
    assert child.stdout.readline().strip() == "ready"
    child.send_signal(signal.SIGTERM)
    child.communicate(timeout=10)
    assert child.returncode == 42
finally:
    if child.poll() is None:
        child.kill()
        child.wait()

assert not list((home / ".local/bin").glob(".devtools-install*"))
print("PASS: install, PATH, profile values, project commands, worktrees, permissions, "
      "failed-update preservation, update, repeat update, HTTPS delivery, latest and "
      "pinned releases, discovery failure preservation, and signals")
