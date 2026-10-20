"""WSL/Windows interop and temporary loopback probe; no OS setup changes."""
import json
import os
from pathlib import Path
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


PS = Path("/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe")


def powershell(script, env=None):
    process = subprocess.run(
        [str(PS), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script],
        cwd="/mnt/c/Windows", env=env, capture_output=True, timeout=20,
    )
    return process.returncode, process.stdout.decode("utf-8", errors="replace").strip()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"devtools-preflight-loopback"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


report = {"kernel": Path("/proc/sys/kernel/osrelease").read_text().strip()}
code, output = powershell(
    "$v=Get-ItemProperty 'HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion';"
    "[ordered]@{build=$v.CurrentBuild;revision=$v.UBR;displayVersion=$v.DisplayVersion;"
    "powershell=$PSVersionTable.PSVersion.ToString();"
    "architecture=$env:PROCESSOR_ARCHITECTURE;pwshAvailable=[bool](Get-Command pwsh -ErrorAction SilentlyContinue)}|ConvertTo-Json -Compress"
)
report["windows_metadata"] = {"exit_code": code, "data": json.loads(output) if code == 0 else output}
for label, value in [("without_wslenv", ""), ("with_wslenv", "DEVTOOLS_PREFLIGHT_VALUE/w")]:
    env = dict(os.environ, DEVTOOLS_PREFLIGHT_VALUE="preflight-public-canary", WSLENV=value)
    code, output = powershell("if($env:DEVTOOLS_PREFLIGHT_VALUE -eq 'preflight-public-canary'){'present'}else{'absent'}", env)
    report[label] = {"exit_code": code, "value": output}
server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
try:
    port = server.server_address[1]
    code, output = powershell(
        "$ErrorActionPreference='Stop';try{"
        f"$r=Invoke-WebRequest -UseBasicParsing -TimeoutSec 5 'http://127.0.0.1:{port}/';"
        "if($r.Content -eq 'devtools-preflight-loopback'){'reachable'}else{exit 2}"
        "}catch{'unreachable';exit 1}"
    )
    report["windows_to_wsl_ipv4"] = {"exit_code": code, "value": output}
finally:
    server.shutdown()
    server.server_close()
    thread.join()
print(json.dumps(report, ensure_ascii=False, indent=2))
# Connectivity observations can fail without making the evidence collection fail.
# Readiness of a supported networking mode must be assessed separately.
