"""Proxy evidence must not retain credentials from common diagnostic formats."""
import unittest
from proxy_compat import redact


class EvidenceRedaction(unittest.TestCase):
    def test_header_and_url_credentials_are_absent(self):
        transcript = "Authorization: Basic dXNlcjpwYXNz\nAuthorization=Bearer application-secret\nCookie=session-cookie\nwss://app.test/socket?token=vite-secret&sid=engine-session&transport=websocket"
        cleaned = redact(transcript)
        for secret in ("dXNlcjpwYXNz", "application-secret", "session-cookie", "vite-secret", "engine-session"):
            self.assertNotIn(secret, cleaned)
        self.assertIn("transport=websocket", cleaned)


if __name__ == "__main__":
    unittest.main()
