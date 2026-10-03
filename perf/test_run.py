import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import run


class PerformanceTests(unittest.TestCase):
    def test_isolation_strips_context_and_git_routing(self):
        inherited = {"HOME": "/user", "PATH": "/bin", "DEVTOOLS_TASK_CONTEXT": "user-token",
                     "GIT_DIR": "/user/repo/.git", "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "foo",
                     "GIT_CONFIG_VALUE_0": "bar", "XDG_DATA_HOME": "/user/data"}
        with mock.patch("run.platform.system", return_value="Linux"):
            env = run.isolated_env(Path("/fixture"), inherited)
        self.assertEqual(env["HOME"], "/user")
        self.assertEqual(env["XDG_DATA_HOME"], "/fixture/data")
        self.assertEqual(env["GIT_CONFIG_GLOBAL"], os.devnull)
        for key in ("DEVTOOLS_TASK_CONTEXT", "GIT_DIR", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"):
            self.assertNotIn(key, env)
        self.assertEqual(inherited["XDG_DATA_HOME"], "/user/data")

    def test_unsupported_platform_fails_before_cli(self):
        with mock.patch("run.platform.system", return_value="Darwin"):
            with self.assertRaisesRegex(RuntimeError, "Linux/WSL"):
                run.isolated_env(Path("/fixture"))

    def test_reset_restores_receipts_and_state_for_every_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".devtools-perf").touch()
            (root / "baseline").mkdir()
            for name in ("config", "data", "cache"):
                (root / name).mkdir(mode=0o700)
                (root / "baseline" / name).mkdir(mode=0o700)
            state = root / "data" / "state.json"
            (root / "baseline" / "data" / "state.json").write_text('{"revision": 1, "receipts": []}')
            fixture = run.Fixture(root, Path("/binary"), {})
            case = run.Case("tasks/add", "tasks", [], [], reset=True)
            with mock.patch.object(fixture, "cleanup") as cleanup:
                for _ in range(3):
                    fixture.prepare(case)
                    self.assertEqual(json.loads(state.read_text()), {"revision": 1, "receipts": []})
                    state.write_text('{"revision": 2, "receipts": ["same-request"]}')
                    self.assertEqual((root / "data").stat().st_mode & 0o777, 0o700)
                self.assertEqual(cleanup.call_count, 3)

    def test_reset_rejects_symlink_before_removing_anything(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "fixture"
            root.mkdir()
            (root / ".devtools-perf").touch()
            external = Path(directory) / "external"
            external.mkdir()
            (root / "data").symlink_to(external, target_is_directory=True)
            fixture = run.Fixture(root, Path("/binary"), {})
            with mock.patch.object(fixture, "cleanup") as cleanup:
                with self.assertRaisesRegex(RuntimeError, "escapes"):
                    fixture.reset()
                cleanup.assert_not_called()
            self.assertTrue(external.is_dir())

    def test_failed_commands_and_timeouts_are_errors(self):
        with self.assertRaisesRegex(RuntimeError, "exited 7"):
            run.execute([sys.executable, "-c", "raise SystemExit(7)"])
        with self.assertRaises(subprocess.TimeoutExpired) as caught:
            run.execute([sys.executable, "-c", "import time; time.sleep(60)", "private-token"], timeout=0.1)
        self.assertNotIn("private-token", str(caught.exception))

    def test_mutation_preflight_rejects_replay_and_noop(self):
        case = run.Case("tasks/add", "tasks", [], [], reset=True, changed=True)
        run.validate_preflight(case, json.dumps({"data": {"changed": True, "replayed": False}}))
        for data in ({"changed": False}, {"changed": True, "replayed": True}):
            with self.assertRaisesRegex(RuntimeError, "fresh mutation"):
                run.validate_preflight(case, json.dumps({"data": data}))

    def test_hyperfine_version_is_checked_before_measurements(self):
        for version in ("hyperfine 1.20.0", "hyperfine 2.0.0"):
            run.require_hyperfine(version)
        for version in ("hyperfine 1.19.0", "unknown"):
            with self.assertRaises(RuntimeError):
                run.require_hyperfine(version)

    def test_result_rejects_partial_failure_and_nonfinite_samples(self):
        result = {"results": [{"times": [0.1, 0.2], "exit_codes": [0, 0], "mean": 0.15,
                               "median": 0.15, "stddev": 0.07, "min": 0.1, "max": 0.2}]}
        self.assertEqual(run.valid_result(copy.deepcopy(result), 2)["p95"], 0.2)
        for field, value in (("times", [0.1]), ("times", [0.1, float("nan")]), ("exit_codes", [0, 1])):
            invalid = copy.deepcopy(result)
            invalid["results"][0][field] = value
            with self.assertRaises(ValueError):
                run.valid_result(invalid, 2)

    def test_comparison_matches_fixture_key_and_requires_compatible_metadata(self):
        old = {"complete": True, "metadata": {"cpu": "CPU", "runs": 10},
               "results": [{"key": "tasks/list:size=10", "mean": 0.1}]}
        new = {"metadata": dict(old["metadata"]), "results": [
            {"key": "tasks/list:size=10", "mean": 0.12}, {"key": "tasks/list:size=100", "mean": 0.2}]}
        run.compare(new, old)
        self.assertAlmostEqual(new["results"][0]["change_percent"], 20)
        self.assertIsNone(new["results"][1]["change_percent"])
        new["metadata"]["cpu"] = "Other CPU"
        with self.assertRaisesRegex(ValueError, "cpu"):
            run.compare(new, old)
        old["complete"] = False
        new["metadata"]["cpu"] = "CPU"
        with self.assertRaisesRegex(ValueError, "incomplete"):
            run.compare(new, old)

    def test_placeholders_leave_literal_command_arguments_untouched(self):
        case = run.Case("test", "cli", ["printf", "{hello}", "{process_id}"], [])
        self.assertEqual(run.resolved_case(case, {"process_id": "123"}).argv, ["printf", "{hello}", "123"])


if __name__ == "__main__":
    unittest.main()
