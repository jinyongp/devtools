"""Run each sandbox scenario with its own user data and working directory."""

import argparse
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def main():
    scenarios = {path.stem: path for path in
                 sorted((Path(__file__).parent / "scenarios").glob("*.py"))}
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenarios", nargs="*", help="Scenario names, or all (default)")
    parser.add_argument("--list", action="store_true", help="List available scenarios")
    parser.add_argument("--releases", type=Path, default=Path("/opt/releases"),
                        help="Directory containing packaged test releases")
    args = parser.parse_args()
    if args.list:
        print("\n".join(scenarios))
        return 0
    selected = args.scenarios or ["all"]
    if selected == ["all"]:
        selected = list(scenarios)
    if not selected or any(name not in scenarios for name in selected):
        parser.error("Choose all or named scenarios from --list")
    root = Path(__file__).resolve().parent
    repository = root.parent
    installer = repository / "scripts/install.sh"
    readme = repository / "README.md"
    install_doc = repository / "docs/install.md"
    if not installer.is_file():
        installer = Path("/opt/devtools-install.sh")
        readme = root / "docs/README.md"
        install_doc = root / "docs/install.md"
    for name in selected:
        with tempfile.TemporaryDirectory(prefix=f"devtools-{name}-") as temporary:
            home = Path(temporary).resolve() / "home"
            home.mkdir(mode=0o700)
            env = dict(os.environ, HOME=str(home),
                       XDG_CONFIG_HOME=str(home / ".config"),
                       XDG_DATA_HOME=str(home / ".local/share"),
                       XDG_CACHE_HOME=str(home / ".cache"),
                       GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_NOSYSTEM="1",
                       DEVTOOLS_TEST_INSTALLER=str(installer),
                       DEVTOOLS_TEST_RELEASES=str(args.releases.resolve()),
                       DEVTOOLS_TEST_README=str(readme),
                       DEVTOOLS_TEST_INSTALL_DOC=str(install_doc))
            print(f"Running scenario: {name}", flush=True)
            result = subprocess.run([sys.executable, str(scenarios[name])],
                                    cwd=home, env=env)
            if result.returncode:
                return result.returncode if result.returncode > 0 else 128 - result.returncode
    return 0


if __name__ == "__main__":
    sys.exit(main())
