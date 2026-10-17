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
mutations = {
    "Job": ("\t\t\t_ = windows.CloseHandle(job)", "\t\t\t_ = job // intentionally leak failed-launch Job"),
    "File": ("\t\t\tstreams.closeAll()", "\t\t\t_ = streams // intentionally leak failed-launch pipes"),
}
outputs = []
for kind, (needle, replacement) in mutations.items():
    if original.count(needle) != 1:
        raise SystemExit("Cannot identify exact failed-launch " + kind + " cleanup boundary.")
    with tempfile.TemporaryDirectory(prefix="devtools-handle-mutant-") as temporary:
        scratch = Path(temporary)
        mutant = scratch / "launcher_windows.go"
        mutant.write_text(original.replace(needle, replacement), encoding="utf-8")
        overlay = scratch / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(source): str(mutant)}}), encoding="utf-8")
        result = subprocess.run(["go", "test", "-count=1", "-overlay", str(overlay), "-run", "^TestWindowsOwnedLaunchFailureClosesHandles$", "./internal/process"], capture_output=True, text=True, timeout=120)
        output = result.stdout + result.stderr
        outputs.append("Expected " + kind + " leak failure:\n" + output)
        Path("handle-cleanup-mutant.log").write_text("\n".join(outputs), encoding="utf-8")
        if result.returncode == 0 or "failed starts leaked handles: type=" + kind not in output:
            raise SystemExit("Failure-handle check did not detect deliberate " + kind + " leak; inspect handle-cleanup-mutant.log.")
print("PASS: native resource-type accounting rejects deliberately leaked Job and File handles; expected failing mutant outputs retained.")
