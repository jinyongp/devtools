import json
import os
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
from unittest import mock

import cli_json


class JSONCapabilityTests(unittest.TestCase):
    def setUp(self):
        cli_json._capabilities.clear()

    def response(self, supported):
        data = {"protocol_version": 16 if supported else 14}
        if supported:
            data["global_options"] = [{"name": "format", "values": ["auto", "text", "json"], "placement": "root-prefix"}]
        return types.SimpleNamespace(returncode=0, stdout=json.dumps({"schema_version": 1, "ok": True, "data": data}))

    def test_old_and_new_binary_negotiate_before_any_operation(self):
        for supported in (False, True):
            with self.subTest(supported=supported), tempfile.TemporaryDirectory() as temporary:
                binary = Path(temporary) / "devtools"
                binary.write_bytes(b"fixture")
                parent = dict(os.environ, DEVTOOLS_FORMAT="text")
                with mock.patch.object(cli_json.subprocess, "run", return_value=self.response(supported)) as probe:
                    argv = cli_json.json_argv(binary, ["task", "add", "--request-id", "same-uuid"], environment=parent)
                    expected = [str(binary), *( ["--format", "json"] if supported else []), "task", "add", "--request-id", "same-uuid"]
                    self.assertEqual(argv, expected)
                    self.assertEqual(probe.call_args.args[0], [str(binary), "schema", "version"])
                    self.assertEqual(probe.call_args.kwargs["env"]["DEVTOOLS_FORMAT"], "json")
                    self.assertEqual(parent["DEVTOOLS_FORMAT"], "text")
                    self.assertEqual(cli_json.json_argv(binary, ["version"], environment=parent), [str(binary), *( ["--format", "json"] if supported else []), "version"])
                    self.assertEqual(probe.call_count, 1)

    def test_bootstrap_failure_does_not_construct_or_execute_a_mutation(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "devtools"
            binary.write_bytes(b"fixture")
            malformed = ["text, not JSON", "[]", '{"schema_version":1,"ok":true,"data":[]}']
            for result in [types.SimpleNamespace(returncode=2, stdout=""), *(
                    types.SimpleNamespace(returncode=0, stdout=value) for value in malformed)]:
                with mock.patch.object(cli_json.subprocess, "run", return_value=result) as probe:
                    with self.assertRaisesRegex(RuntimeError, "operation was not started"):
                        cli_json.json_argv(binary, ["task", "add"], environment={"DEVTOOLS_FORMAT": "invalid"})
                    self.assertEqual(probe.call_count, 1)
                    self.assertEqual(probe.call_args.args[0][1:], ["schema", "version"])

    def test_private_wire_never_receives_a_new_public_option(self):
        with mock.patch.object(cli_json.subprocess, "run") as probe:
            self.assertEqual(cli_json.json_argv("/not-installed", ["__vault-session", "/fixture"]), ["/not-installed", "__vault-session", "/fixture"])
            probe.assert_not_called()

    def test_timeout_is_not_an_operation_retry(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "devtools"
            binary.write_bytes(b"fixture")
            with mock.patch.object(cli_json.subprocess, "run", side_effect=subprocess.TimeoutExpired("bootstrap", 10)) as probe:
                with self.assertRaisesRegex(RuntimeError, "operation was not started"):
                    cli_json.json_argv(binary, ["task", "done"])
                self.assertEqual(probe.call_count, 1)


if __name__ == "__main__":
    unittest.main()
