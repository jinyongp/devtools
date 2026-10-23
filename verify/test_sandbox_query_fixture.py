import json
import os
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

from sandbox_query_fixture import fixture_environment, held, sandbox_command


class SandboxBoundary(unittest.TestCase):
    def test_mac_policy_quotes_fixture_and_keeps_child_argv_literal(self):
        data = '/tmp/fixture "quoted" \\ path'
        command = sandbox_command(data, "/tmp/other-scratch", ["tool", "$(literal)"], system="darwin")
        self.assertIn('(subpath ' + json.dumps(str(Path(data).resolve())) + ')', command[2])
        self.assertEqual(command[-2:], ["tool", "$(literal)"])
        self.assertIn("deny file-write*", command[2])

    def test_writable_scratch_cannot_reopen_data_subtree(self):
        for data, scratch in (("/tmp/data", "/tmp/data/child"), ("/tmp/data/child", "/tmp/data"), ("/tmp/data", "/tmp/data")):
            with self.subTest(data=data, scratch=scratch), self.assertRaises(ValueError):
                sandbox_command(data, scratch, ["true"], system="linux")

    def test_linux_root_is_read_only_with_one_disjoint_scratch(self):
        command = sandbox_command("/tmp/data", "/tmp/scratch", ["tool"], system="linux")
        self.assertEqual(command[:4], ["bwrap", "--ro-bind", "/", "/"])
        scratch = str(Path("/tmp/scratch").resolve())
        self.assertEqual(command[4:7], ["--bind", scratch, scratch])

    def test_privileged_linux_launcher_drops_to_invoking_host_identity(self):
        command = sandbox_command("/tmp/data", "/tmp/scratch", ["tool"], system="linux", privileged=True)
        self.assertEqual(command[:3], ["sudo", "-n", "bwrap"])
        self.assertNotIn("--unshare-user", command)
        start = command.index("/usr/bin/setpriv")
        self.assertEqual(command[start:start + 7], ["/usr/bin/setpriv", "--reuid", str(os.getuid()), "--regid", str(os.getgid()), "--clear-groups", "--"])

    def test_fixture_environment_excludes_credentials_and_real_user_home(self):
        with patch.dict(os.environ, {"HOME": "/real/home", "GITHUB_TOKEN": "private",
                                    "DEVTOOLS_TASK_CONTEXT": "private", "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "private"}):
            env = fixture_environment("/tmp/query-home")
        self.assertEqual(env["HOME"], str(Path("/tmp/query-home").resolve()))
        self.assertFalse(any(key in env for key in ("GITHUB_TOKEN", "DEVTOOLS_TASK_CONTEXT", "ACTIONS_ID_TOKEN_REQUEST_TOKEN")))

    def test_holder_is_stopped_if_assertion_fails_while_it_owns_lock(self):
        child = [sys.executable, "-c", 'import sys; print(\'{"event":"acquired"}\', flush=True); sys.stdin.read()']
        with self.assertRaisesRegex(RuntimeError, "body failed"):
            with held(child) as process:
                raise RuntimeError("body failed")
        self.assertIsNotNone(process.poll())


if __name__ == "__main__":
    unittest.main()
