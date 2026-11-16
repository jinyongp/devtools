"""Exercise a previous Homebrew keg and the first command after brew upgrade."""

import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tempfile
import time


BASELINE = "516ba782d68179cbdd348020cb3cc7d1af319ac6"
BASELINE_SHA256 = "e162b1348b3d3a8d1d816a424e8dfd90e5cc11d1eb31421680e7aaa8c2879455"
TAP = "devtools-fixture/upgrade"
FORMULA = f"{TAP}/devtools-upgrade-fixture"


def run(*args, env=None, cwd=None, timeout=400):
    result = subprocess.run(args, env=env, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    assert result.returncode == 0, (args, result.stdout, result.stderr)
    return result.stdout


def variant(formula, version, commit=None, digest=None):
    formula = formula.replace("class Devtools < Formula\n", "class DevtoolsUpgradeFixture < Formula\n  keg_only :versioned_formula\n", 1)
    assert "class DevtoolsUpgradeFixture < Formula" in formula
    formula, count = re.subn(r'^  version "[^"]+"$', f'  version "{version}"', formula, count=1, flags=re.M)
    assert count == 1
    if commit:
        formula, count = re.subn(r'^  url "[^"]+"$', f'  url "https://github.com/jinyongp/devtools/archive/{commit}.tar.gz"', formula, count=1, flags=re.M)
        assert count == 1
        formula, count = re.subn(r'^  sha256 "[^"]+"$', f'  sha256 "{digest}"', formula, count=1, flags=re.M)
        assert count == 1
    return formula


def exercise(root, spec):
    environment = dict(os.environ, HOMEBREW_NO_AUTO_UPDATE="1", HOMEBREW_NO_INSTALL_CLEANUP="1")
    repository = Path(run("brew", "--repository", env=environment).strip())
    tap = repository / "Library/Taps/devtools-fixture/homebrew-upgrade"
    assert not tap.exists()
    source = root / "homebrew-upgrade"
    (source / "Formula").mkdir(parents=True)
    name = "Formula/devtools-upgrade-fixture.rb"
    (source / name).write_text(variant(spec, "0.46.0", BASELINE, BASELINE_SHA256))
    run("git", "init", "--initial-branch=main", str(source))
    run("git", "-C", str(source), "add", name)
    run("git", "-C", str(source), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-m", "prior formula")
    run("brew", "trust", "--tap", str(source), env=environment)
    run("git", "clone", "--quiet", str(source), str(tap))
    state = root / "isolated"
    binary = None
    try:
        run("brew", "install", "--build-from-source", FORMULA, env=environment)
        binary = str(Path(run("brew", "--prefix", FORMULA, env=environment).strip()) / "bin/devtools")
        project = root / "sample"
        project.mkdir()
        (project / "devtools.toml").write_text(
            'profile = "sample"\n[ports.web]\n[proxies.web]\nport = "web"\nhost = "sample.localhost"\n'
            '[commands.web]\ninject = false\nserve = ["web"]\nexec = ' +
            json.dumps([sys.executable, "-c", "print('fixture-ran-once')"]) + "\n"
        )

        def cli(*args):
            return run(binary, "--format", "json", "--isolated-root", str(state), *args, cwd=project, timeout=45)

        assert json.loads(cli("version"))["data"]["version"] == "0.46.0"
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        cli("proxy", "configure", "--http-address", f"127.0.0.1:{port}", "--disable-https")
        prior = json.loads(cli("proxy", "start"))["data"]["item"]
        (tap / name).write_text(variant(spec, "0.47.0"))
        run("brew", "upgrade", FORMULA, env=environment)
        assert json.loads(cli("version"))["data"]["version"] == "0.47.0"
        assert json.loads(cli("proxy", "status"))["data"]["item"]["generation"] == prior["generation"]
        assert cli("run", "web").splitlines() == ["fixture-ran-once"]
        current = json.loads(cli("proxy", "status"))["data"]["item"]
        assert current["running"] and current["port"] == port and current["generation"] != prior["generation"]
        receipt = state / "data/.installation-upgrade.json"
        for _ in range(120):
            if receipt.exists() and json.loads(receipt.read_text())["ready"]:
                break
            time.sleep(0.25)
        else:
            raise AssertionError("the independent upgrade did not finish")
        assert cli("run", "web").splitlines() == ["fixture-ran-once"]
        assert json.loads(cli("proxy", "status"))["data"]["item"]["generation"] == current["generation"]
        cli("proxy", "stop", "--all")
        print("Homebrew upgrade and first project command passed")
    finally:
        if binary:
            subprocess.run([binary, "--isolated-root", str(state), "proxy", "stop", "--all"], capture_output=True, timeout=15)
        subprocess.run(["brew", "uninstall", "--force", FORMULA], env=environment, capture_output=True, timeout=60)
        subprocess.run(["brew", "untap", "--force", TAP], env=environment, capture_output=True, timeout=60)
        subprocess.run(["brew", "untrust", "--tap", str(source)], env=environment, capture_output=True, timeout=60)


if __name__ == "__main__":
    assert os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
    spec = Path(sys.argv[1]).read_text()
    with tempfile.TemporaryDirectory(prefix="devtools-brew-upgrade-") as fixture:
        exercise(Path(fixture), spec)
