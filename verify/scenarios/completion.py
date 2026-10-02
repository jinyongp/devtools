"""Exercise dynamic completion using only an installed release binary."""
import json
import os
from pathlib import Path
import select
import shlex
import shutil
import subprocess
import time
import uuid

binary = str(Path.home() / '.local/bin/devtools')
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install',
                '--version', '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               check=True, capture_output=True)
os.environ['PATH'] = str(Path(binary).parent) + os.pathsep + os.environ['PATH']

def call(*args, input=None):
    return subprocess.check_output([binary, *args], input=input, text=True)

literal_commands = {
    'docs:dev': 'docs:',
    '@docs/dev': '@docs/',
    '@@docs/dev': '@@docs/',
    'foo:@docs/dev': 'foo:@docs/',
    'space name': 'space',
    'money$dev': 'money',
    "quote'cmd": 'quote',
    'equals=dev': 'equals',
    '文書:開発': '文書:',
    'meta$(touch COMMAND_NAME_EXECUTED)': 'meta',
}
Path('devtools.toml').write_text(
    "profile = 'app'\n[commands.web]\nexec = ['echo', 'CANARY-VALUE']\n" +
    ''.join(f'[commands.{json.dumps(name, ensure_ascii=False)}]\n'
            "exec = ['printf', 'LITERAL_COMMAND_RAN']\n"
            for name in literal_commands))
for name in literal_commands:
    assert call('run', name) == 'LITERAL_COMMAND_RAN', name
    assert json.loads(call('command', 'inspect', name))['data']['item']['name'] == name
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
    (['run', 'docs:'], 'docs:dev'),
    (['process', 'start', '@docs/'], '@docs/dev'),
    (['project', 'restart', 'docs:'], 'docs:dev'),
    (['project', 'logs', 'docs:'], 'docs:dev'),
    (['task', 'show', task_id[:8]], task_id),
    (['task', 'add', '--workstream', ''], workstream_id),
    (['var', 'set', 'KEY', '--value', ''], None),
    (['run', '--', ''], None),
    (['run', 'web', ''], None),
    (['run', 'web', '--env', ''], None),
    (['command', 'run', 'web', '--profile', ''], None),
    (['cmd', 'run', 'web', '--help', ''], None),
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
    if shell == 'bash':
        split_cases = [
            ('devtools run docs:d', ['devtools', 'run', 'docs', ':', 'd'], 'dev'),
            ('devtools run docs:', ['devtools', 'run', 'docs', ':'], 'dev'),
            ('devtools run equals=d', ['devtools', 'run', 'equals', '=', 'd'], 'dev'),
            ('devtools run @docs/d', ['devtools', 'run', '@', 'docs/d'], '@docs/dev'),
            ('devtools run foo:@docs/d', ['devtools', 'run', 'foo', ':@', 'docs/d'], '@docs/dev'),
            ('devtools var list --profile=ot', ['devtools', 'var', 'list', '--profile', '=', 'ot'], 'other'),
            ('devtools project up docs:dev @docs/', ['devtools', 'project', 'up', 'docs', ':', 'dev', '@docs/'], '@docs/dev'),
        ]
        for line, words, expected in split_cases:
            harness = (source + 'COMP_LINE=' + shlex.quote(line) + '\n'
                       'COMP_WORDS=(' + ' '.join(shlex.quote(word) for word in words) + ')\n'
                       'COMP_CWORD=$((${#COMP_WORDS[@]}-1))\n'
                       '_devtools\nprintf "%s\\n" "${COMPREPLY[@]}"')
            result = subprocess.run(['bash', '--noprofile', '--norc', '-c', harness],
                                    text=True, capture_output=True, check=True)
            assert not result.stderr and result.stdout.strip() == expected, (line, result.stdout, result.stderr)
        for name, prefix in literal_commands.items():
            harness = (source + 'COMP_WORDS=(devtools run ' + shlex.quote(prefix) + ')\n'
                       'COMP_CWORD=2\n_devtools\nprintf "%s\\n" "${COMPREPLY[@]}"')
            candidate = subprocess.check_output(['bash', '--noprofile', '--norc', '-c', harness], text=True).strip()
            completed_name = subprocess.check_output(
                ['bash', '--noprofile', '--norc', '-c', 'printf "%s" ' + candidate], text=True)
            assert completed_name == name, (name, candidate)
            # Execute the completion exactly as inserted; shell syntax in names stays literal.
            output = subprocess.check_output(['bash', '--noprofile', '--norc', '-c',
                                              shlex.quote(binary) + ' run ' + candidate], text=True)
            assert output == 'LITERAL_COMMAND_RAN', (name, candidate, output)
        assert not Path('COMMAND_NAME_EXECUTED').exists()
        print('bash word breaks and literal shell quoting passed')

        # Readline supplies raw shell quotes and word breaks, unlike synthetic COMP_WORDS.
        print('Starting bash interactive Readline verification', flush=True)
        terminal_env = dict(os.environ, TERM='dumb')
        shell_binary = shutil.which('bash')
        shell_args = [shell_binary, '--noprofile', '--norc', '-i']
        # Readline needs terminal file descriptors; job control is tested by tty.py.
        # Avoid Python work after fork, which can deadlock on macOS.
        master, slave = os.openpty()
        try:
            process = subprocess.Popen(shell_args, stdin=slave, stdout=slave, stderr=slave,
                                       env=terminal_env, start_new_session=True)
        except BaseException:
            os.close(master)
            raise
        finally:
            os.close(slave)
        print('Interactive Bash started', flush=True)

        def read_until(marker):
            output = b''
            deadline = time.monotonic() + 5
            while marker not in output and time.monotonic() < deadline:
                if select.select([master], [], [], .1)[0]:
                    output += os.read(master, 8192)
            assert marker in output, (marker, output)
            return output

        try:
            setup = ("PS1='REVIEW_PROMPT> '; source " + shlex.quote(str(script)) +
                     "; devtools() { printf '\\nREVIEW_RESULT:%s:%s\\n' \"$#\" \"$2\"; }; "
                     "printf '\\nREVIEW_READY\\n'\n")
            os.write(master, setup.encode())
            read_until(b'\r\nREVIEW_READY\r\n')
            print('Interactive Bash completion ready', flush=True)
            for prefix, expected in [
                ('@docs/d', '@docs/dev'), ('@@docs/d', '@@docs/dev'),
                ('foo:@docs/d', 'foo:@docs/dev'),
                ("'space", 'space name'), ('"space', 'space name'),
                ('space\\ na', 'space name'),
                ("'quote", "quote'cmd"), ('"quote', "quote'cmd"),
                ('"money', 'money$dev'),
                ('"meta', 'meta$(touch COMMAND_NAME_EXECUTED)'),
            ]:
                os.write(master, ('devtools run ' + prefix + '\t\n').encode())
                read_until(('\r\nREVIEW_RESULT:2:' + expected + '\r\n').encode())
            assert not Path('COMMAND_NAME_EXECUTED').exists()
            os.write(master, b'exit\n')
            assert process.wait(timeout=3) == 0, process.returncode
        finally:
            os.close(master)
            if process.poll() is None:
                process.kill()
                process.wait(timeout=3)
        print('bash interactive Readline quoting and special word breaks passed')
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
