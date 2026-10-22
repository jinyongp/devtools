"""Proxy evidence must not retain credentials from common diagnostic formats."""
import unittest
from pathlib import Path
import shutil
import tempfile
from unittest.mock import patch
from proxy_compat import loopback_mapping, owned_scratch, redact


class EvidenceRedaction(unittest.TestCase):
    def test_header_and_url_credentials_are_absent(self):
        transcript = "Authorization: Basic dXNlcjpwYXNz\nAuthorization=Bearer application-secret\nCookie=session-cookie\nwss://app.test/socket?token=vite-secret&sid=engine-session&transport=websocket"
        cleaned = redact(transcript)
        for secret in ("dXNlcjpwYXNz", "application-secret", "session-cookie", "vite-secret", "engine-session"):
            self.assertNotIn(secret, cleaned)
        self.assertIn("transport=websocket", cleaned)

    def test_cleanup_failure_retains_primary_error_and_recovery_root(self):
        result = {"status": "fail", "error": "browser verification failed"}
        with patch("proxy_compat.shutil.rmtree", side_effect=PermissionError("owned lease is locked")):
            with owned_scratch(result) as root:
                self.assertTrue(root.is_dir())
        try:
            self.assertEqual(result["error"], "browser verification failed")
            self.assertIn("filesystem_cleanup_error", result)
            self.assertEqual(Path(result["recovery_root"]), root)
            self.assertTrue(root.is_dir())
        finally:
            shutil.rmtree(root)

    def test_disposable_dns_cleanup_preserves_unowned_bytes_after_failure(self):
        with tempfile.TemporaryDirectory() as root:
            hosts = Path(root) / "hosts"
            before = b"# existing\r\n127.0.0.1 existing.test"
            sibling = b"\r\n192.0.2.1 sibling.test\r\n"
            hosts.write_bytes(before)
            with self.assertRaisesRegex(RuntimeError, "browser failed"):
                with loopback_mapping(hosts, "compat.localhost"):
                    self.assertIn(b"127.0.0.1 compat.localhost", hosts.read_bytes())
                    with hosts.open("ab") as stream:
                        stream.write(sibling)
                    raise RuntimeError("browser failed")
            self.assertEqual(hosts.read_bytes(), before + sibling)
            with self.assertRaisesRegex(RuntimeError, "unowned hosts entry"):
                with loopback_mapping(hosts, "existing.test"):
                    self.fail("an existing hostname must not be taken over")
            self.assertEqual(hosts.read_bytes(), before + sibling)


if __name__ == "__main__":
    unittest.main()
