"""Exercise dynamic completion using only an installed release binary."""
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import uuid

binary = str(Path.home() / '.local/bin/devtools')
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install',
                '--version', '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               check=True, capture_output=True)
os.environ['PATH'] = str(Path(binary).parent) + os.pathsep + os.environ['PATH']

def call(*args, input=None):
    return subprocess.check_output([binary, *args], input=input, text=True)

Path('devtools.toml').write_text("profile = 'app'\n[commands.web]\nexec = ['echo', 'CANARY-VALUE']\n")
call('env', 'create', 'local')
call('env', 'create', 'staging', '--profile', 'other')
call('var', 'set', '_LEVEL', '--value', 'CANARY-VALUE')
call('var', 'set', 'LOCAL_ONLY', '--env', 'local', '--value', 'CANARY-VALUE')
call('sec', 'set', 'TOKEN', '--stdin', input='CANARY-SECRET')
task_id = json.loads(call('task', 'add', '--title', 'CANARY-TITLE',
                          '--request-id', str(uuid.uuid4())))['data']['item']['id']
workstream_id = json.loads(call('task', 'workstream', 'create', '--title', 'CANARY-TITLE',
                                '--request-id', str(uuid.uuid4())))['data']['item']['id']

cases = [
    (['var', 'list', '--profile', 'ot'], 'other'),
    (['var', 'list', '--env', ''], 'local'),
    (['var', 'list', '--profile', 'other', '--env', ''], 'staging'),
    (['var', 'list', '--profile=other', '--env=s'], '--env=staging'),
    (['var', 'get', '_'], '_LEVEL'),
    (['variable', 'get', '--env', 'local', 'LOCAL'], 'LOCAL_ONLY'),
    (['sec', 'unset', 'TO'], 'TOKEN'),
    (['run', 'we'], 'web'),
    (['task', 'show', task_id[:8]], task_id),
    (['task', 'add', '--workstream', ''], workstream_id),
    (['var', 'set', 'KEY', '--value', ''], None),
    (['run', '--', ''], None),
]
for shell in ('bash', 'zsh', 'fish'):
    if not shutil.which(shell):
        continue
    script = Path.home() / ('completion.' + shell)
    script.write_text(call('completion', shell))
    for args, expected in cases:
        words = ['devtools', *args]
        quoted = ' '.join(shlex.quote(word) for word in words)
        source = 'source ' + shlex.quote(str(script)) + '\n'
        if shell == 'bash':
            harness = (source + 'COMP_WORDS=(' + quoted + ')\n'
                       'COMP_CWORD=$((${#COMP_WORDS[@]}-1))\n'
                       '_devtools\nprintf "%s\\n" "${COMPREPLY[@]}"')
        elif shell == 'zsh':
            harness = ('compdef() { :; }\ncompadd() { print -rl -- $dynamic; }\n'
                       '_describe() { print -rl -- $candidates; }\n' + source +
                       'words=(' + quoted + ')\nCURRENT=${#words}\n_devtools')
        else:
            line = shlex.join(words[:-1]) + ' ' + words[-1]
            harness = source + 'complete -C ' + shlex.quote(line)
        command = [shell, '--no-config' if shell == 'fish' else '-f', '-c', harness]
        result = subprocess.run(command, text=True, capture_output=True)
        output = result.stdout
        assert not result.stderr and 'CANARY' not in output, (shell, words, result.stderr, output)
        assert (expected in output) if expected else not output.strip(), (shell, words, output)
    print(shell + ' installed dynamic completion passed')
    if shell == 'fish':
        result = subprocess.run(
            [shutil.which(shell), '--no-config', '-c',
             'source "$argv[1]"; set -gx PATH /nonexistent; complete -C "devtools var "',
             str(script)], text=True, capture_output=True, check=True)
        assert not result.stderr and 'get' in result.stdout, (result.stdout, result.stderr)

call('var', 'unset', 'LOCAL_ONLY', '--env', 'local')
call('env', 'remove', 'local')
assert call('__complete', input='var\0list\0--env\0\0') == ''
print('Removed names disappear on the next completion query')
