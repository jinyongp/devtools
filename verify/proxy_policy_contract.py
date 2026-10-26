"""Installed CLI Host policy, old-daemon rejection and rewrite recovery."""
import argparse
import http.client
import json
import os
from pathlib import Path
import subprocess
import tempfile
import uuid

from network_fixture import Backend, WebSocket, free_port, serving


def check(binary, baseline=None):
    evidence = {"checks": []}
    with tempfile.TemporaryDirectory(prefix="devtools-host-contract-") as temporary, serving(Backend) as backend:
        root = Path(temporary)
        environment = dict(os.environ, DEVTOOLS_FORMAT="json")
        environment.pop("DEVTOOLS_TASK_CONTEXT", None)
        environment.pop("DEVTOOLS_ISOLATED_ROOT", None)

        def api(*args, cli=binary, request=None, error=None):
            argv = [str(cli), "--isolated-root", str(root), *args]
            if request:
                argv += ["--request-id", request]
            run = subprocess.run(argv, capture_output=True, text=True, env=environment, cwd=root, timeout=45)
            if error:
                assert run.returncode and json.loads(run.stderr)["error"]["code"] == error, run.stderr
                return
            assert run.returncode == 0, run.stderr
            return json.loads(run.stdout)["data"]

        def mutate(*args, **kwargs):
            return api(*args, request=str(uuid.uuid4()), **kwargs)

        port = free_port()
        backend_port = backend.server_port
        started = False
        try:
            initial = baseline or binary
            mutate("proxy", "configure", "--http-address", f"0.0.0.0:{port}", cli=initial)
            add = ["proxy", "add", "app", str(backend_port), "--global", "--host", "contract.localhost"]
            default = mutate(*add, cli=initial)["item"]
            if not baseline:
                assert default["host_policy"] == "rewrite"
            mutate("proxy", "up", "app", "--global", cli=initial)
            started = True
            if baseline:
                mutate("proxy", "start", "--all", error="old_daemon_version")
                mutate("proxy", "restart", "--all")
                evidence["checks"].append("old daemon rejected; explicit restart succeeded")

            def headers():
                connection = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
                try:
                    connection.request("GET", "/", headers={"Host": f"contract.localhost:{port}", "Origin": f"http://contract.localhost:{port}", "X-Forwarded-Host": "spoof.invalid"})
                    response = connection.getresponse()
                    assert response.status == 200
                    return json.loads(response.read())
                finally:
                    connection.close()

            assert headers()["Host"] == f"127.0.0.1:{backend_port}"
            for value in ("", "invalid", "${POLICY}"):
                mutate("proxy", "add", "invalid", str(free_port()), "--global", "--host", "invalid.localhost", "--host-policy", value, error="invalid_argument")
            mutate("proxy", "down", "app", "--global")
            mutate("proxy", "rm", "app", "--global")
            request = str(uuid.uuid4())
            preserved = api(*add, "--host-policy", "preserve", request=request)["item"]
            assert preserved["id"] != default["id"] and preserved["host_policy"] == "preserve"
            assert api(*add, "--host-policy", "preserve", request=request)["replayed"]
            api(*add, "--host-policy", "rewrite", request=request, error="request_conflict")
            mutate("proxy", "up", "app", "--global")
            assert headers()["Host"] == f"contract.localhost:{port}"
            assert headers()["X-Forwarded-Host"] == f"contract.localhost:{port}"
            ws = WebSocket({"url": f"http://contract.localhost:{port}", "origin_port": port})
            ws.echo()
            ws.close()
            share = mutate("expose", "start", "app", "--global", "--provider", "lan", "--alias", "contract.local", "--no-auth")["item"]
            mutate("expose", "stop", share["id"])
            if baseline:
                # Strict old readers must accept even the stopped preserve history.
                assert api("expose", "list", cli=baseline)["items"][0]["state"] == "stopped"
            mutate("proxy", "down", "app", "--global")
            mutate("proxy", "rm", "app", "--global")
            restored = mutate(*add, "--host-policy", "rewrite")["item"]
            assert restored["id"] != preserved["id"]
            mutate("proxy", "up", "app", "--global")
            assert headers()["Host"] == f"127.0.0.1:{backend_port}"
            if baseline:
                api("proxy", "list", "--global", cli=baseline)
                mutate("proxy", "restart", "--all", cli=baseline)
                assert headers()["Host"] == f"127.0.0.1:{backend_port}"
            ws = WebSocket({"url": f"http://contract.localhost:{port}", "origin_port": port})
            ws.echo()
            ws.close()
            evidence["checks"].append("invalid enum, default/replay/conflict, preserve HTTP/WS, stopped history, global UUID recreation and rewrite HTTP/WS recovery")
            evidence["status"] = "pass"
        finally:
            if started:
                # Restart through the candidate if the old daemon is running.
                mutate("proxy", "restart", "--all")
                mutate("proxy", "stop", "--all")
    return evidence


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--baseline", type=Path)
    args = parser.parse_args()
    print(json.dumps(check(args.binary.resolve(), args.baseline.resolve() if args.baseline else None)))


if __name__ == "__main__":
    main()
