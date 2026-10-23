import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from fixture_paths import fixture_path, session_path, temporary_fixture


class FixturePaths(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="devtools-path-contract-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.physical = self.root / "physical"
        self.physical.mkdir()
        self.alias = self.root / "alias"
        if os.name == "nt":
            subprocess.run(["cmd.exe", "/d", "/c", "mklink", "/J", str(self.alias), str(self.physical)],
                           check=True, capture_output=True, timeout=10)
        else:
            self.alias.symlink_to(self.physical, target_is_directory=True)

    def test_existing_alias_and_missing_suffix_select_one_physical_root(self):
        self.assertEqual(fixture_path(self.alias), self.physical)
        self.assertEqual(fixture_path(self.alias / "not-created" / "child"), self.physical / "not-created" / "child")
        self.assertFalse((self.physical / "not-created").exists())

    def test_temporary_root_canonicalizes_alias_parent_before_use_and_cleans_up(self):
        with temporary_fixture(prefix="new-", directory=self.alias) as root:
            self.assertEqual(root.parent, self.physical)
            self.assertTrue(root.is_dir())
        self.assertFalse(root.exists())

    def test_windows_selected_session_keeps_alias_identity(self):
        self.assertEqual(session_path(self.alias / "vault", system="nt"), self.alias / "vault")
        self.assertEqual(fixture_path(self.alias / "vault"), self.physical / "vault")

    def test_unix_session_uses_same_physical_root_as_new_fixture(self):
        self.assertEqual(session_path(self.alias / "vault", system="posix"), self.physical / "vault")


if __name__ == "__main__":
    unittest.main()
