"""Keep installed help concise and schemas scoped."""
import json
import os
from pathlib import Path
import subprocess

binary = str(Path.home() / '.local/bin/devtools')
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install',
                '--version', '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               check=True, capture_output=True)
def call(*args):
    return subprocess.check_output([binary, *args], text=True)
for args in [(), ('--help',), ('task', '--help'), ('task', 'claim', '--help')]:
    output = call(*args)
    assert 'Usage:' in output and 'input_schema' not in output
    assert len(output) < 4000
index = call('schema')
specific = call('schema', 'task', 'claim')
full = call('schema', '--all')
assert len(index) < 2000 and len(specific) < len(full) / 5
assert 'input_schema' in json.loads(specific)['data']
assert 'options' not in json.loads(specific)['data']
print('Concise help, group discovery, scoped schema, and explicit full catalog passed')
skill = call('skill')
assert skill.startswith('---\nname: devtools\n')
destination = Path.home() / 'agent-skills/devtools/SKILL.md'
destination.parent.mkdir(parents=True)
destination.write_text(skill)
assert destination.read_text() == skill
print('Bundled skill exported from installed binary')
