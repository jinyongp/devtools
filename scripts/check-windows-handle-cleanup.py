"""Prove the isolated native failure check detects a leaked Job handle."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

if sys.platform != "win32":
    raise SystemExit("This runtime check only runs on native Windows Actions.")
source = Path("internal/process/launcher_windows.go").resolve()
original = source.read_text(encoding="utf-8")
needle = "\t\t\t_ = windows.CloseHandle(job)"
if original.count(needle) != 1:
    raise SystemExit("Cannot identify the exact failed-launch Job cleanup boundary.")
with tempfile.TemporaryDirectory(prefix="devtools-handle-mutant-") as temporary:
    scratch = Path(temporary)
    mutant = scratch / "launcher_windows.go"
    mutant.write_text(original.replace(needle, "\t\t\t_ = job // intentionally leak the failure Job for this check"), encoding="utf-8")
    overlay = scratch / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(source): str(mutant)}}), encoding="utf-8")
    result = subprocess.run(["go", "test", "-count=1", "-overlay", str(overlay), "-run", "^TestWindowsOwnedLaunchFailureClosesHandles$", "./internal/process"], capture_output=True, text=True, timeout=120)
    output = result.stdout + result.stderr
    Path("handle-cleanup-mutant.log").write_text(output, encoding="utf-8")
    if result.returncode == 0 or "failed starts leaked handles" not in output:
        raise SystemExit("Failure-handle check did not detect the deliberate Job leak; inspect handle-cleanup-mutant.log.")
print("PASS: isolated failure check rejects a deliberately leaked Job handle; expected failing mutant output retained.")
