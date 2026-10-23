import json
import os
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

from sandbox_query_fixture import fixture_environment, held, sandbox_command
from fixture_paths import temporary_fixture


class SandboxBoundary(unittest.TestCase):
    def setUp(self):
        self.fixture = temporary_fixture(prefix="devtools-sandbox-unit-")
        self.root = self.fixture.__enter__()
        self.addCleanup(self.fixture.__exit__, None, None, None)

    def test_mac_policy_quotes_fixture_and_keeps_child_argv_literal(self):
        data = self.root / 'fixture "quoted" \\ path'
        command = sandbox_command(data, self.root / "scratch", ["tool", "$(literal)"], system="darwin")
        self.assertIn('(subpath ' + json.dumps(str(data)) + ')', command[2])
        self.assertEqual(command[-2:], ["tool", "$(literal)"])
        self.assertIn("deny file-write*", command[2])

    def test_writable_scratch_cannot_reopen_data_subtree(self):
        parent = self.root / "data"
        for data, scratch in ((parent, parent / "child"), (parent / "child", parent), (parent, parent)):
            with self.subTest(data=data, scratch=scratch), self.assertRaises(ValueError):
                sandbox_command(data, scratch, ["true"], system="linux")

    def test_linux_root_is_read_only_with_one_disjoint_scratch(self):
        command = sandbox_command(self.root / "data", self.root / "scratch", ["tool"], system="linux")
        self.assertEqual(command[:4], ["bwrap", "--ro-bind", "/", "/"])
        scratch = str(self.root / "scratch")
        self.assertEqual(command[4:7], ["--bind", scratch, scratch])

    def test_privileged_linux_launcher_drops_to_invoking_host_identity(self):
        command = sandbox_command(self.root / "data", self.root / "scratch", ["tool"], system="linux", privileged=True)
        self.assertEqual(command[:3], ["sudo", "-n", "bwrap"])
        self.assertNotIn("--unshare-user", command)
        start = command.index("/usr/bin/setpriv")
        self.assertEqual(command[start:start + 7], ["/usr/bin/setpriv", "--reuid", str(os.getuid()), "--regid", str(os.getgid()), "--clear-groups", "--"])

    def test_fixture_environment_excludes_credentials_and_real_user_home(self):
        with patch.dict(os.environ, {"HOME": "/real/home", "GITHUB_TOKEN": "private",
                                    "DEVTOOLS_TASK_CONTEXT": "private", "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "private"}):
            env = fixture_environment(self.root / "query-home")
        self.assertEqual(env["HOME"], str(self.root / "query-home"))
        self.assertFalse(any(key in env for key in ("GITHUB_TOKEN", "DEVTOOLS_TASK_CONTEXT", "ACTIONS_ID_TOKEN_REQUEST_TOKEN")))

    def test_holder_is_stopped_if_assertion_fails_while_it_owns_lock(self):
        child = [sys.executable, "-c", 'import sys; print(\'{"event":"acquired"}\', flush=True); sys.stdin.read()']
        with self.assertRaisesRegex(RuntimeError, "body failed"):
            with held(child) as process:
                raise RuntimeError("body failed")
        self.assertIsNotNone(process.poll())

    def test_alias_cannot_hide_writable_scratch_over_data(self):
        data = self.root / "physical"
        data.mkdir()
        alias = self.root / "alias"
        alias.symlink_to(data, target_is_directory=True)
        with self.assertRaises(ValueError):
            sandbox_command(data, alias, ["true"], system="linux")


if __name__ == "__main__":
    unittest.main()
