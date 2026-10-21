"""Installed source feature against an owned cooperative local receiver."""
import os
from pathlib import Path
import subprocess
import sys

repository = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(repository / "verify"))
from value_handoff_fixture import run

home = Path.home()
subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]],
               cwd=home, capture_output=True, check=True, timeout=30)
receiver = home / "value-handoff-runner"
subprocess.run(["go", "build", "-o", str(receiver), "./examples/value-handoff-runner"], cwd=repository, check=True, timeout=90)
run(home / ".local/bin/devtools", receiver, home / "handoff-fixture")
