"""Installed recovery, bounded inputs, inventory, cursor and backup integration."""
import json
import os
from pathlib import Path
import subprocess
import sys

sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from cli_reliability_fixture import check_cli_reliability,unix_terminal

subprocess.run(['sh',os.environ['DEVTOOLS_TEST_INSTALLER'],'install','--version','0.0.0-test.1',
                '--source',os.environ['DEVTOOLS_TEST_RELEASES']],check=True,capture_output=True,timeout=30)
result=check_cli_reliability(Path.home()/'.local/bin/devtools',Path.home()/'reliability',terminal=unix_terminal)
assert result['diagnostic_schema']['validated'] > 0 and result['diagnostic_schema']['protocol_version'] == 17
print(json.dumps(result))
