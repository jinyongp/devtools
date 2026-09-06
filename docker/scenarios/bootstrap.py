"""Exercise the documented bootstrap with deterministic download fixtures."""
import os
from pathlib import Path
import re
import subprocess
import tempfile

with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    tools = root / "bin"
    tools.mkdir()
    fake_curl = tools / "curl"
    fake_curl.write_text('''#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then destination=$2; shift 2; else shift; fi
done
case "$MODE" in
  fail) exit 22 ;;
esac
printf '%s\\n' 'printf "%s\\n" "$@" > "$MARKER"' > "$destination"
if [ "$MODE" = partial ]; then exit 22; fi
''')
    fake_curl.chmod(0o700)
    snippets = []
    for name in ("DEVTOOLS_TEST_README", "DEVTOOLS_TEST_INSTALL_DOC"):
        text = Path(os.environ[name]).read_text()
        snippets.extend(re.findall(r'^\(\n.*?^\)', text, re.M | re.S))
        assert "/tmp/devtools-install.sh" not in text
    assert len(snippets) == 4
    for index, snippet in enumerate(snippets):
        for mode in ("success", "fail", "partial"):
            temporary = root / f"tmp-{index}-{mode}"
            temporary.mkdir()
            sentinel = temporary / "devtools-install.sh"
            sentinel.write_text("fixture remains untouched\n")
            marker = root / "executed"
            marker.unlink(missing_ok=True)
            env = dict(os.environ, PATH=str(tools) + ":" + os.environ["PATH"],
                       TMPDIR=str(temporary), MODE=mode, MARKER=str(marker))
            result = subprocess.run(["sh", "-c", snippet], env=env, capture_output=True, timeout=5)
            assert (result.returncode == 0) == (mode == "success")
            assert marker.exists() == (mode == "success")
            if marker.exists():
                assert marker.read_text().strip() == ("update" if 'install.sh" update' in snippet else "install")
            assert sentinel.read_text() == "fixture remains untouched\n"
            assert list(temporary.iterdir()) == [sentinel], "Temporary directory was not cleaned"
print("Documented install/update bootstrap gates execution on download success and cleans private temporary files")
