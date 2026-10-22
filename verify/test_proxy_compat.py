"""Proxy evidence must not retain credentials from common diagnostic formats."""
import unittest
from pathlib import Path
import shutil
from unittest.mock import patch
from proxy_compat import owned_scratch, redact


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


if __name__ == "__main__":
    unittest.main()
