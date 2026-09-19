"""Keep installed help concise and schemas scoped."""
import json
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

binary = str(Path.home() / '.local/bin/devtools')
os.environ['PATH'] = str(Path(binary).parent) + os.pathsep + os.environ['PATH']
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install',
                '--version', '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               check=True, capture_output=True)
def call(*args):
    return subprocess.check_output([binary, *args], text=True)
for args in [(), ('--help',), ('command', '--help'), ('profile', '--help'), ('task', '--help'), ('task', 'claim', '--help')]:
    output = call(*args)
    assert 'Usage:' in output and 'input_schema' not in output
    assert len(output) < 4000
index = call('schema')
specific = call('schema', 'task', 'claim')
full = call('schema', '--all')
assert len(index) < 2000 and len(specific) < len(full) / 5
assert 'input_schema' in json.loads(specific)['data']
assert 'options' not in json.loads(specific)['data']
for output in (index, specific, full, call('schema', 'command')):
    envelope = json.loads(output)
    assert envelope['schema_version'] == 1 and envelope['ok'] is True
    assert envelope['data']['protocol_version'] == 3
for command in json.loads(full)['data']['commands']:
    assert command['output_mode'] in ('json', 'text', 'artifact', 'passthrough')
    assert 'stream_output' not in command
    assert ('output_schema' in command) == (command['output_mode'] == 'json')
assert json.loads(index)['data']['items']
assert json.loads(call('schema', 'command', 'run'))['data'] == json.loads(call('schema', 'run'))['data']
print('Concise help, versioned output modes, group discovery, scoped schema, and explicit full catalog passed')

release_directory = Path(os.environ['DEVTOOLS_TEST_RELEASES'])
archive_path = release_directory / 'devtools-skill_0.0.0-test.1.tar.gz'
checksum_path = Path(str(archive_path) + '.sha256')
checksum, checksum_name = checksum_path.read_text().split()
assert checksum_name == archive_path.name
assert hashlib.sha256(archive_path.read_bytes()).hexdigest() == checksum
with tarfile.open(archive_path, 'r:gz') as archive:
    assert archive.getnames() == ['devtools', 'devtools/SKILL.md']
    skill_member = archive.getmember('devtools/SKILL.md')
    assert skill_member.isfile()
    skill_file = archive.extractfile(skill_member)
    assert skill_file is not None
    skill = skill_file.read()
destination = Path.home() / '.agents/skills/devtools/SKILL.md'
destination.parent.mkdir(parents=True)
destination.write_bytes(skill)
canonical_skill = Path(os.environ['DEVTOOLS_TEST_CANONICAL_SKILL'])
assert destination.read_bytes() == canonical_skill.read_bytes()
print('Independent Agent Skill release artifact installed and matched its canonical source')

completion = Path.home() / 'devtools.fish'
completion.write_text(call('completion', 'fish'))
fish = shutil.which('fish')
if fish:
    for line, expected in [
        ('devtools ', 'var'),
        ('devtools variable set KEY ', '--value'),
        ('devtools task workstream ', 'create'),
        ('devtools command ', 'list'),
        ('devtools profile ', 'export'),
        ('devtools var set KEY --profile ', None),
        ('devtools var set KEY --profile demo ', '--value'),
        ('devtools command run -- ', None),
        ('devtools run -- ', None),
    ]:
        result = subprocess.run(
            [fish, '-c', 'source "$argv[1]"; complete -C "$argv[2]"',
             str(completion), line], text=True, capture_output=True, check=True)
        assert not result.stderr, result.stderr
        output = result.stdout
        assert (expected in output) if expected else not output.strip(), (line, output)
    print('Installed fish completion resolves commands and respects argument boundaries')
else:
    print('Fish is unavailable; generated completion retained for installed-release verification')
