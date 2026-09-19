"""Run installed-release scenarios with isolated user data and working directories."""

import argparse
import os
from pathlib import Path
import subprocess
import sys
import tempfile


TEST_VERSIONS = ("0.0.0-test.1", "0.0.0-test.2")


def package_test_releases(repository: Path, releases: Path) -> None:
    releases.mkdir(parents=True, exist_ok=True)
    for version in TEST_VERSIONS:
        env = dict(os.environ, VERSION=version, OUTPUT_DIR=str(releases), COMMIT="verify")
        subprocess.run(["sh", "scripts/package.sh"], cwd=repository, env=env, check=True)
    env = dict(os.environ, VERSION=TEST_VERSIONS[0], OUTPUT_DIR=str(releases))
    subprocess.run(["sh", "scripts/package-skill.sh"], cwd=repository, env=env, check=True)


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
            if result.returncode:
                return result.returncode if result.returncode > 0 else 128 - result.returncode
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
