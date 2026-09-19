"""Run installed-release scenarios with isolated user data and working directories."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import uuid


TEST_VERSIONS = ("0.0.0-test.1", "0.0.0-test.2")


def host_target(repository: Path):
    result = subprocess.run(
        ["go", "env", "GOHOSTOS", "GOHOSTARCH"],
        cwd=repository,
        text=True,
        capture_output=True,
        check=True,
    )
    values = result.stdout.splitlines()
    if len(values) != 2:
        raise RuntimeError("Cannot resolve host Go target")
    return values[0], values[1]


def package_test_releases(repository: Path, releases: Path) -> None:
    releases.mkdir(parents=True, exist_ok=True)
    target_os, target_arch = host_target(repository)
    for version in TEST_VERSIONS:
        env = dict(
            os.environ,
            VERSION=version,
            OUTPUT_DIR=str(releases),
            COMMIT="verify",
            TARGET_OS=target_os,
            TARGET_ARCH=target_arch,
        )
        subprocess.run(["sh", "scripts/package.sh"], cwd=repository, env=env, check=True)
    env = dict(os.environ, VERSION=TEST_VERSIONS[0], OUTPUT_DIR=str(releases))
    subprocess.run(["sh", "scripts/package-skill.sh"], cwd=repository, env=env, check=True)


def cleanup_managed_processes(home: Path, env) -> bool:
    binary = home / ".local/bin/devtools"
    if not binary.is_file():
        return True

    def run(*args):
        try:
            return subprocess.run(
                [str(binary), *args],
                cwd=home,
                env=env,
                text=True,
                capture_output=True,
                timeout=10,
            )
        except (OSError, subprocess.TimeoutExpired):
            return None

    clean = True
    run("proxy", "stop", "--request-id", str(uuid.uuid4()))
    proxy_status = run("proxy", "status")
    if proxy_status is None or proxy_status.returncode != 0:
        clean = False
    else:
        try:
            if json.loads(proxy_status.stdout)["data"]["item"]["running"]:
                clean = False
        except (KeyError, TypeError, json.JSONDecodeError):
            clean = False

    profiles = set()
    listed = run("profile", "list")
    if listed is not None and listed.returncode == 0:
        try:
            profiles.update(
                item["profile"]
                for item in json.loads(listed.stdout)["data"]["items"]
                if isinstance(item.get("profile"), str)
            )
        except (KeyError, TypeError, json.JSONDecodeError):
            pass

    for config in home.rglob("devtools.toml"):
        inspected = run("project", "inspect", "--dir", str(config.parent))
        if inspected is None or inspected.returncode != 0:
            continue
        try:
            profile = json.loads(inspected.stdout)["data"]["item"]["profile"]
        except (KeyError, TypeError, json.JSONDecodeError):
            continue
        if isinstance(profile, str):
            profiles.add(profile)

    for profile in sorted(profiles):
        run("dashboard", "stop", "--profile", profile)
        dashboard_status = run("dashboard", "status", "--profile", profile)
        if dashboard_status is None or dashboard_status.returncode != 0:
            clean = False
        else:
            try:
                if json.loads(dashboard_status.stdout)["data"]["item"]["running"]:
                    clean = False
            except (KeyError, TypeError, json.JSONDecodeError):
                clean = False

        processes = run("process", "list", "--profile", profile)
        if processes is None or processes.returncode != 0:
            clean = False
            continue
        try:
            items = json.loads(processes.stdout)["data"]["items"]
        except (KeyError, TypeError, json.JSONDecodeError):
            clean = False
            continue
        for item in items:
            if item.get("ended_at") is not None:
                continue
            run(
                "process",
                "stop",
                item["id"],
                "--request-id",
                str(uuid.uuid4()),
            )

        remaining = run("process", "list", "--profile", profile)
        if remaining is None or remaining.returncode != 0:
            clean = False
            continue
        try:
            if any(
                item.get("ended_at") is None
                for item in json.loads(remaining.stdout)["data"]["items"]
            ):
                clean = False
        except (KeyError, TypeError, json.JSONDecodeError):
            clean = False
    return clean


def run_scenarios(selected, scenarios, repository: Path, releases: Path) -> int:
    installer = repository / "scripts/install.sh"
    readme = repository / "README.md"
    install_doc = repository / "docs/install.md"
    canonical_skill = repository / "skills/devtools/SKILL.md"

    for name in selected:
        with tempfile.TemporaryDirectory(prefix=f"devtools-{name}-") as temporary:
            home = Path(temporary).resolve() / "home"
            home.mkdir(mode=0o700)
            env = dict(
                os.environ,
                HOME=str(home),
                XDG_CONFIG_HOME=str(home / ".config"),
                XDG_DATA_HOME=str(home / ".local/share"),
                XDG_CACHE_HOME=str(home / ".cache"),
                GIT_CONFIG_GLOBAL="/dev/null",
                GIT_CONFIG_NOSYSTEM="1",
                DEVTOOLS_TEST_INSTALLER=str(installer),
                DEVTOOLS_TEST_RELEASES=str(releases),
                DEVTOOLS_TEST_README=str(readme),
                DEVTOOLS_TEST_INSTALL_DOC=str(install_doc),
                DEVTOOLS_TEST_CANONICAL_SKILL=str(canonical_skill),
            )
            print(f"Running scenario: {name}", flush=True)
            result = subprocess.run(
                [sys.executable, str(scenarios[name])],
                cwd=home,
                env=env,
            )
            cleaned = cleanup_managed_processes(home, env)
            if result.returncode:
                return result.returncode if result.returncode > 0 else 128 - result.returncode
            if not cleaned:
                print(f"Scenario left managed processes running: {name}", file=sys.stderr)
                return 1
    return 0


def main():
    root = Path(__file__).resolve().parent
    repository = root.parent
    scenarios = {
        path.stem: path for path in sorted((root / "scenarios").glob("*.py"))
    }

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenarios", nargs="*", help="Scenario names, or all (default)")
    parser.add_argument("--list", action="store_true", help="List available scenarios")
    parser.add_argument(
        "--releases",
        type=Path,
        help="Use an existing directory of packaged test releases instead of building fixtures",
    )
    args = parser.parse_args()

    if args.list:
        print("\n".join(scenarios))
        return 0

    selected = args.scenarios or ["all"]
    if selected == ["all"]:
        selected = list(scenarios)
    if not selected or any(name not in scenarios for name in selected):
        parser.error("Choose all or named scenarios from --list")

    if args.releases is not None:
        releases = args.releases.resolve()
        if not releases.is_dir():
            parser.error("--releases must name an existing directory")
        return run_scenarios(selected, scenarios, repository, releases)

    with tempfile.TemporaryDirectory(prefix="devtools-verify-releases-") as temporary:
        releases = Path(temporary).resolve() / "releases"
        package_test_releases(repository, releases)
        return run_scenarios(selected, scenarios, repository, releases)


if __name__ == "__main__":
    sys.exit(main())
