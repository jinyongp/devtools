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
    args = parser.parse_args()
    if args.list:
        print("\n".join(scenarios))
        return 0
    selected = args.scenarios or ["all"]
    if selected == ["all"]:
        selected = list(scenarios)
    if not selected or any(name not in scenarios for name in selected):
        parser.error("Choose all or named scenarios from --list")
    for name in selected:
        with tempfile.TemporaryDirectory(prefix=f"devtools-{name}-") as temporary:
            home = Path(temporary) / "home"
            home.mkdir(mode=0o700)
            env = dict(os.environ, HOME=str(home),
                       XDG_CONFIG_HOME=str(home / ".config"),
                       XDG_DATA_HOME=str(home / ".local/share"),
                       XDG_CACHE_HOME=str(home / ".cache"))
            print(f"Running scenario: {name}", flush=True)
            result = subprocess.run([sys.executable, str(scenarios[name])],
                                    cwd=home, env=env)
            if result.returncode:
                return result.returncode if result.returncode > 0 else 128 - result.returncode
    return 0


if __name__ == "__main__":
    sys.exit(main())
