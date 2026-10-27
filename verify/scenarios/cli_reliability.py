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
assert result['reference_schema'] == {'validated':7,'format_assertions':[False,True]}
assert {'reference_schema_formats_and_exact_edit_provenance','doctor_deadline_probe_reasons_and_recovery_effects',
        'actual_terminal_edit_reference_location','actual_terminal_doctor_reason_recovery'} <= set(result['checks'])
print(json.dumps(result))
