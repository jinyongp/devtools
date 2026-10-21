import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("storage_trace_build", Path(__file__).with_name("build.py"))
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class InstrumentationContract(unittest.TestCase):
    def test_ambiguous_or_missing_source_anchor_is_rejected(self):
        for source in ("other source", "anchor anchor"):
            with self.subTest(source=source), self.assertRaises(ValueError):
                BUILD.replace_once(source, "anchor", "replacement")

    def test_pinned_legacy_predicate_and_candidate_decisions_are_retained(self):
        for mode, (commit, _) in BUILD.SOURCES.items():
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as scratch:
                root = Path(scratch)
                private = root / "internal/platformfs"
                private.mkdir(parents=True)
                source_file = "private_windows.go" if mode == "legacy" else "containment_windows.go"
                originals = {}
                for name in (source_file, "error.go"):
                    text = subprocess.check_output(["git", "show", f"{commit}:internal/platformfs/{name}"], cwd=BUILD.REPO, text=True)
                    (private / name).write_text(text)
                    originals[name] = text
                hashes = BUILD.instrument(root, mode, commit)
                changed = (private / source_file).read_text()
                if mode == "legacy":
                    decision = 'if !strings.HasPrefix(actual, strings.TrimRight(base, `\\`)+`\\`) {\n\t\treturn fsError("verify_file_identity", f.Name(), "path_mismatch", ErrUnsafe)\n\t}'
                    self.assertIn(decision, originals[source_file])
                    self.assertIn(decision, changed)
                    self.assertIn("traceActual = actual", changed)
                else:
                    self.assertIn('return fsError("verify_file_identity", path, "file_identity_mismatch", ErrUnsafe)', changed)
                    self.assertIn("expected.VolumeSerialNumber != actual.VolumeSerialNumber", changed)
                    decision = 'return fsError("verify_file_identity", path, "file_identity_mismatch", ErrUnsafe)\n\t\t}\n\t\tstorageTraceVerified(r, f, path, h)'
                    self.assertIn(decision, changed)
                for name in hashes:
                    self.assertNotIn("@SOURCE_COMMIT@", (root / name).read_text())
                    self.assertNotIn("@VERIFY_CALL@", (root / name).read_text())
                self.assertIn("return storageerr.Wrap(operation, storageResource(path), reason, cause)", (private / "error.go").read_text())


if __name__ == "__main__":
    unittest.main()
