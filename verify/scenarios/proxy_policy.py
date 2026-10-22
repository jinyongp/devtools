"""Exercise Host policy through the installed archive without browser dependencies."""
import os
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from proxy_policy_contract import check

binary = Path.home() / ".local/bin/devtools"
subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]], check=True, capture_output=True, timeout=30)
print(check(binary))
