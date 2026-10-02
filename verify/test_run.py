import contextlib
import io
import os
from pathlib import Path
import tempfile
import subprocess
import time
import unittest
from unittest.mock import patch

import run


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
