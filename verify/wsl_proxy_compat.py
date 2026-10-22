"""Actual WSL2 server to Windows Chromium on a disposable Actions runner."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import urllib.request
import uuid

from proxy_compat import redact


def download(url, destination, expected):
    digest = hashlib.sha256()
    with urllib.request.urlopen(url, timeout=120) as response, destination.open("wb") as output:
        while body := response.read(1024 * 1024):
            digest.update(body)
            output.write(body)
    if digest.hexdigest() != expected:
        raise RuntimeError("Pinned test runtime checksum mismatch")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="Linux x64 candidate")
    parser.add_argument("--artifacts", type=Path, required=True)
    args = parser.parse_args()
    if os.name != "nt" or os.environ.get("DEVTOOLS_DISPOSABLE_HOST") != "1":
        parser.error("Requires an explicitly disposable Windows host")
    repository = Path(__file__).resolve().parents[1]
    artifacts = args.artifacts.resolve()
    artifacts.mkdir(parents=True, exist_ok=True)
    name = "devtools-proxy-" + uuid.uuid4().hex
    temporary = Path(tempfile.mkdtemp(prefix="devtools-wsl-proxy-"))
    report = {"status": "fail", "distribution": name, "recovery_root": str(temporary), "binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest()}
    attempted = False
    removed = False

    def wsl(*words, timeout=120):
        result = subprocess.run(["wsl.exe", *map(str, words)], capture_output=True, timeout=timeout)
        if result.returncode:
            diagnostic = (result.stdout + b"\n" + result.stderr).replace(b"\0", b"").decode("utf-8-sig", errors="replace")
            (artifacts / "wsl-command-error.log").write_text(redact(diagnostic[-30000:]), encoding="utf8")
            raise RuntimeError(f"WSL operation {words[0]} exited {result.returncode}")
        return result.stdout.replace(b"\0", b"").decode("utf-8-sig").strip()

    def linux(*words, user="root", timeout=120):
        return wsl("--distribution", name, "--user", user, "--exec", *words, timeout=timeout)

    try:
        report["wsl_version"] = wsl("--version")
        if name in wsl("--list", "--quiet").splitlines():
            raise RuntimeError("Refusing to reuse an existing distribution")
        image = temporary / "ubuntu.rootfs.tar.gz"
        download("https://cloud-images.ubuntu.com/wsl/releases/24.04/current/ubuntu-noble-wsl-amd64-24.04lts.rootfs.tar.gz", image, "2a790896740b14d637dbdc583cce1ba081ac53b9e9cdb46dc09a2f73abbd9934")
        node = temporary / "node.tar.xz"
        download("https://nodejs.org/dist/v22.22.2/node-v22.22.2-linux-x64.tar.xz", node, "88fd1ce767091fd8d4a99fdb2356e98c819f93f3b1f8663853a2dee9b438068a")
        source = temporary / "source.tar"
        subprocess.run(["git", "archive", "--format=tar", "-o", str(source), "HEAD"], cwd=repository, check=True, timeout=30)
        attempted = True
        wsl("--import", name, temporary / "instance", image, "--version", "2")
        linux("/usr/sbin/useradd", "--create-home", "--shell", "/bin/bash", "dvtfixture")
        linux("/usr/bin/mkdir", "-p", "/opt/node", "/home/dvtfixture/repository")
        translate = lambda path: linux("/usr/bin/wslpath", "-u", str(Path(path).resolve()))
        linux("/usr/bin/tar", "-xJf", translate(node), "-C", "/opt/node", "--strip-components=1")
        linux("/usr/bin/tar", "-xf", translate(source), "-C", "/home/dvtfixture/repository")
        linux("/usr/bin/install", "-m", "755", translate(args.binary), "/usr/local/bin/devtools")
        linux("/usr/bin/chown", "-R", "dvtfixture:dvtfixture", "/home/dvtfixture/repository")
        windows_node = shutil.which("node")
        if not windows_node:
            raise RuntimeError("Prepared Windows Node is required")
        # Only this child exports the public CA path to Windows. Product WSLENV
        # and the developer's distro/network configuration are never changed.
        command = ["/usr/bin/env", "HOME=/home/dvtfixture", "PATH=/opt/node/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "DEVTOOLS_DISPOSABLE_HOST=1", "/bin/bash", "-c",
                   "cd /home/dvtfixture/repository && corepack pnpm --dir verify/fixtures/proxy-compat install --frozen-lockfile --ignore-scripts && python3 -B verify/proxy_compat.py \"$@\"", "proxy-compat",
                   "--binary", "/usr/local/bin/devtools", "--case", "nuxt", "--case", "ws", "--scheme", "https", "--policy", "preserve", "--hostname", "wsl-compat.localhost", "--client-mode", "windows-peer", "--windows-node", translate(windows_node), "--windows-runner", translate(repository / "verify/proxy_compat_browser.mjs"), "--peer-packages", translate(repository / "verify/fixtures/proxy-compat/node_modules"), "--disposable-trust", "--artifacts", translate(artifacts)]
        output = linux(*command, user="dvtfixture", timeout=600)
        (artifacts / "wsl-run.log").write_text(output, encoding="utf8")
        report["checks_passed"] = True
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        try:
            if attempted:
                if name in wsl("--list", "--quiet").splitlines():
                    wsl("--terminate", name)
                    wsl("--unregister", name)
                removed = True
            if not attempted or removed:
                shutil.rmtree(temporary)
                report.pop("recovery_root", None)
            report["owned_distribution_removed"] = removed
            if report.get("checks_passed") and removed:
                report["status"] = "pass"
        finally:
            (artifacts / "wsl-result.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf8")
    print(json.dumps(report))


if __name__ == "__main__":
    main()
