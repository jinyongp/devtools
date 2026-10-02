import contextlib
import io
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

import run


class ScenarioTimeoutTest(unittest.TestCase):
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
