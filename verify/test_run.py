import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
import subprocess
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import run
import wsl_vault


class WSLFixtureOwnershipTest(unittest.TestCase):
    def attempt(self, *, preexisting=False, unregister_fails=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            scratch = root / 'import'
            scratch.mkdir()
            binary = root / 'binary'
            binary.write_bytes(b'synthetic-candidate')
            evidence = root / 'evidence.json'
            name = 'devtools-vault-test-fixed-owned-id'
            calls = []
            registered = preexisting

            def process(argv, **kwargs):
                nonlocal registered
                words = argv[1:]
                calls.append(words)
                code, output = 0, b''
                if words[0] == '--version':
                    output = b'WSL fixture'
                elif words[0] == '--list':
                    output = ('User-Ubuntu\n' + (name + '\n' if registered else '')).encode()
                elif words[0] == '--import':
                    registered = True
                    code = 1  # Registration partially succeeded before failure.
                elif words[0] == '--unregister':
                    code = int(unregister_fails)
                    registered = bool(code)
                return subprocess.CompletedProcess(argv, code, stdout=output, stderr=b'')

            digest = SimpleNamespace(update=lambda data: None, hexdigest=lambda:
                                     '2a790896740b14d637dbdc583cce1ba081ac53b9e9cdb46dc09a2f73abbd9934')
            with patch.object(wsl_vault, 'os', SimpleNamespace(name='nt', environ={'DEVTOOLS_DISPOSABLE_HOST': '1'})), \
                    patch.object(wsl_vault.uuid, 'uuid4', return_value=SimpleNamespace(hex='fixed-owned-id')), \
                    patch.object(wsl_vault.tempfile, 'mkdtemp', return_value=str(scratch)), \
                    patch.object(wsl_vault.hashlib, 'sha256', return_value=digest), \
                    patch.object(wsl_vault.urllib.request, 'urlopen', return_value=io.BytesIO(b'fixture-image')), \
                    patch.object(wsl_vault.subprocess, 'run', side_effect=process), \
                    patch('sys.argv', ['wsl_vault.py', '--binary', str(binary), '--commit', 'fixture', '--evidence', str(evidence)]):
                with self.assertRaises(RuntimeError):
                    wsl_vault.main()
            return calls, json.loads(evidence.read_text()), scratch.exists()

    def test_existing_distribution_is_never_imported_terminated_or_removed(self):
        calls, report, retained = self.attempt(preexisting=True)
        self.assertTrue(all(words[0] in ('--version', '--list') for words in calls))
        self.assertEqual(report['result'], 'fail')
        self.assertFalse(retained)

    def test_partial_import_failure_unregisters_only_its_owned_distribution(self):
        calls, report, retained = self.attempt()
        targets = [words for words in calls if words[0] in ('--terminate', '--unregister')]
        self.assertEqual(targets, [['--terminate', 'devtools-vault-test-fixed-owned-id'],
                                   ['--unregister', 'devtools-vault-test-fixed-owned-id']])
        self.assertEqual(report['result'], 'fail')
        self.assertTrue(report['owned_distribution_removed'])
        self.assertFalse(retained)

    def test_failed_unregister_retains_recovery_root_and_failed_evidence(self):
        _, report, retained = self.attempt(unregister_fails=True)
        self.assertEqual(report['result'], 'fail')
        self.assertNotIn('owned_distribution_removed', report)
        self.assertTrue(retained)


class ScenarioTimeoutTest(unittest.TestCase):
    def test_scenario_does_not_inherit_callers_git_repository_or_index(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            caller = root / "caller"
            caller.mkdir()
            env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
            env.update(GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_NOSYSTEM="1",
                       GIT_AUTHOR_NAME="Test", GIT_AUTHOR_EMAIL="test@example.test",
                       GIT_COMMITTER_NAME="Test", GIT_COMMITTER_EMAIL="test@example.test")
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=caller, env=env, text=True).strip()
            git("init", "--quiet")
            (caller / "content").write_text("caller")
            git("add", ".")
            git("commit", "--quiet", "-m", "caller")
            before = git("rev-parse", "HEAD")
            index = (caller / ".git/index").read_bytes()
            (caller / "private.txt").write_text("must remain untracked")
            scenario = root / "git_fixture.py"
            scenario.write_text(
                "import subprocess\nfrom pathlib import Path\n"
                "Path('fixture').write_text('fixture')\n"
                "for args in [('init', '--quiet'), ('add', '.'), ('commit', '--quiet', '-m', 'fixture')]:\n"
                "    subprocess.run(['git', *args], check=True)\n"
            )
            inherited = dict(env, GIT_DIR=str(caller / ".git"), GIT_WORK_TREE=str(caller),
                             GIT_INDEX_FILE=str(caller / ".git/index"))
            with patch.dict(os.environ, inherited, clear=True), \
                    patch.object(run, "cleanup_managed_processes", return_value=True):
                self.assertEqual(run.run_scenarios(["git_fixture"], {"git_fixture": scenario}, root, root), 0)
            self.assertEqual(git("rev-parse", "HEAD"), before)
            self.assertEqual((caller / ".git/index").read_bytes(), index)
            self.assertEqual(git("ls-files", "private.txt"), "")

    def test_stalled_scenario_exits_and_cleans_up_without_running_next(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stalled = root / "stalled.py"
            pid_file = root / "pid"
            stalled.write_text(
                "import os,time\nfrom pathlib import Path\n"
                f"Path({str(pid_file)!r}).write_text(str(os.getpid()))\n"
                "while True: time.sleep(1)\n"
            )
            next_scenario = root / "next.py"
            next_marker = root / "next-ran"
            next_scenario.write_text(f"from pathlib import Path\nPath({str(next_marker)!r}).touch()\n")
            errors = io.StringIO()
            started = time.monotonic()
            with patch.object(run, "SCENARIO_TIMEOUT_SECONDS", .5), \
                    patch.object(run, "cleanup_managed_processes", return_value=True) as cleanup, \
                    contextlib.redirect_stderr(errors):
                result = run.run_scenarios(
                    ["stalled", "next"], {"stalled": stalled, "next": next_scenario}, root, root
                )
            self.assertEqual(result, 124)
            self.assertIn("Scenario timed out after 0.5s: stalled", errors.getvalue())
            self.assertLess(time.monotonic() - started, 5)
            cleanup.assert_called_once()
            self.assertFalse(next_marker.exists())
            with self.assertRaises(ProcessLookupError):
                os.kill(int(pid_file.read_text()), 0)


if __name__ == "__main__":
    unittest.main()
