"""Rename a profile while its service and proxy are owned by an earlier CLI."""

import argparse
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import time
from urllib.parse import urlsplit

from fixture_paths import create_fixture_directory


SERVER = '''import http.server, json, os, socketserver
class Backend(http.server.ThreadingHTTPServer):
    def server_bind(self):
        socketserver.TCPServer.server_bind(self)
        self.server_name, self.server_port = self.server_address[:2]
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        data = json.dumps({"pid": os.getpid(), "port": self.server.server_port}).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
    def log_message(self, *args):
        pass
Backend(("127.0.0.1", int(os.environ["PORT"])), Handler).serve_forever()
'''


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def fetch(port, host="live-rename.localhost"):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=3)
    try:
        connection.request("GET", "/", headers={"Host": host, "Connection": "close"})
        response = connection.getresponse()
        body = response.read()
        if response.status == 200:
            return json.loads(body)
        raise OSError(f"Expected HTTP 200, received {response.status}")
    finally:
        connection.close()


def ready(port, host="live-rename.localhost"):
    deadline = time.monotonic() + 10
    while True:
        try:
            return fetch(port, host)
        except (OSError, http.client.HTTPException):
            if time.monotonic() >= deadline:
                raise
            time.sleep(0.1)


def stop_controller(state):
    proxy = state / "data" / "proxy"
    control_file = proxy / "control.json"
    if not control_file.is_file():
        return
    control = json.loads(control_file.read_text(encoding="utf-8"))
    address, token = urlsplit(control["address"]), control["token"]
    assert address.scheme == "http" and address.hostname == "127.0.0.1" and address.port
    assert not any((address.username, address.path, address.query, address.fragment)) and len(token) == 64
    connection = http.client.HTTPConnection("127.0.0.1", address.port, timeout=5)
    try:
        connection.request("POST", "/stop", headers={"Authorization": "Bearer " + token})
        response = connection.getresponse()
        response.read()
        assert response.status == 200, "Owned proxy controller rejected shutdown"
    finally:
        connection.close()


def verify(root, legacy, candidate):
    project = root / "project"
    project.mkdir()
    state = root / "state"
    environment = {key: value for key, value in os.environ.items() if not key.startswith("DEVTOOLS_")}
    frontend, backend = free_port(), free_port()
    while frontend == backend:
        backend = free_port()
    config = f'''profile = "old"
[ports.web]
port = {backend}
range = [20000, 65000]
[proxies.front]
host = "live-rename.localhost"
port = "web"
[commands.web]
exec = {json.dumps([sys.executable, "server.py"])}
inject = false
serve = ["web"]
[commands.web.bind]
PORT = {{port = "web"}}
'''
    (project / "server.py").write_text(SERVER, encoding="utf-8")
    config_file = project / "devtools.toml"
    config_file.write_text(config, encoding="utf-8")

    def call(binary, *args, error=None):
        result = subprocess.run(
            [str(binary), "--format", "json", "--isolated-root", str(state), *args],
            cwd=project, env=environment, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=35,
        )
        if error is not None:
            assert result.returncode and json.loads(result.stderr)["error"]["code"] == error, (args, result.stderr)
            return None
        assert result.returncode == 0, (args, result.stderr)
        return json.loads(result.stdout)["data"]

    legacy_version = call(legacy, "version")
    candidate_version = call(candidate, "version")
    execution = None
    try:
        call(legacy, "proxy", "configure", "--http-address", f"127.0.0.1:{frontend}", "--disable-https")
        call(legacy, "instance", "name", "live")
        call(legacy, "var", "set", "FIXTURE", "--value", "original")
        call(legacy, "var", "set", "FIXTURE", "--value", "other", "--profile", "occupied")
        task = call(legacy, "task", "add", "--title", "Existing task")["item"]["id"]
        started = call(legacy, "process", "start", "web", "--proxy", "--capture-logs")
        execution = started["item"]["id"]
        instance = call(legacy, "port", "show", "web")["item"]
        try:
            backend_before = ready(instance["port"], "127.0.0.1")
            before = ready(frontend)
        except (OSError, http.client.HTTPException) as failure:
            status = call(legacy, "process", "status", execution)["item"]
            logs = call(legacy, "process", "logs", execution)["content"]
            raise RuntimeError(f"Previous-version fixture did not become ready: {status['state']}; {logs}") from failure
        assert before == backend_before
        proxy_before = call(legacy, "proxy", "status")["item"]
        assert proxy_before["running"]
        call(candidate, "profile", "rename", "old", "occupied", error="profile_exists")
        call(candidate, "profile", "rename", "missing", "new", error="profile_not_found")

        renamed = call(candidate, "profile", "rename", "old", "new")
        assert renamed["changed"] and renamed["profile"] == "new"
        assert {row["profile"] for row in call(candidate, "profile", "list")["items"]} == {"new", "occupied"}
        assert config_file.read_text(encoding="utf-8") == config
        assert fetch(frontend) == before
        proxy_after = call(legacy, "proxy", "status")["item"]
        assert proxy_after["running"] and proxy_after["started_at"] == proxy_before["started_at"]
        retry = call(candidate, "profile", "rename", "old", "new")
        assert not retry["changed"] and retry["profile_id"] == renamed["profile_id"]
        assert call(candidate, "task", "show", task, "--profile", "new")["item"]["id"] == task
        assert call(candidate, "var", "get", "FIXTURE", "--profile", "new")["value"] == "original"
        call(legacy, "var", "set", "FIXTURE", "--value", "legacy-update")
        assert call(candidate, "var", "get", "FIXTURE", "--profile", "new")["value"] == "legacy-update"
        call(candidate, "var", "set", "FIXTURE", "--value", "candidate-update", "--profile", "new")
        assert call(legacy, "var", "get", "FIXTURE")["value"] == "candidate-update"
        after = call(candidate, "port", "show", "web", "--profile", "new")["item"]
        assert (after["port"], after["instance_id"]) == (instance["port"], instance["instance_id"])
        assert after["profile"] == "new"
        existing = call(candidate, "process", "start", "web", "--proxy", "--capture-logs")
        assert not existing["changed"] and existing["item"]["id"] == execution
        assert fetch(frontend) == before
        records = call(candidate, "profile", "inspect", "new")["item"]["processes"]
        assert any(row["execution_id"] == execution and row["state"] == "running" for row in records)
        # A newer client can also select the same execution from a project
        # configured with the new name. Restore the previous proxy's input
        # immediately so its route continues to use the original address.
        try:
            config_file.write_text(config.replace('profile = "old"', 'profile = "new"', 1), encoding="utf-8")
            existing = call(candidate, "process", "start", "web", "--proxy", "--capture-logs")
            assert not existing["changed"] and existing["item"]["id"] == execution
        finally:
            config_file.write_text(config, encoding="utf-8")
        assert fetch(frontend) == before
        call(legacy, "process", "stop", execution)
        assert call(candidate, "process", "status", execution)["item"]["state"] == "stopped"
        execution = None
        call(candidate, "task", "add", "--title", "Another task", "--profile", "new")
        page = call(candidate, "task", "list", "--limit", "1", "--profile", "new")
        tree = call(candidate, "task", "tree", "--limit", "1", "--profile", "new")
        registry = state / "data" / "profile-names.json"
        detached = state / "profile-names.saved"
        registry.rename(detached)
        try:
            next_page = call(candidate, "task", "list", "--cursor", page["next_cursor"], "--profile", "new")
            next_tree = call(candidate, "task", "tree", "--cursor", tree["cursor"], "--profile", "new")
            assert next_page["revision"] == page["revision"] and next_tree["revision"] == tree["revision"]
            call(candidate, "task", "list", "--cursor", page["next_cursor"], "--profile", "occupied", error="cursor_invalid")
        finally:
            detached.rename(registry)
        with ThreadPoolExecutor(max_workers=2) as executor:
            operations = [executor.submit(call, candidate, "profile", "rename", "new", "next") for _ in range(2)]
            chain = [operation.result() for operation in operations]
        assert sorted(row["changed"] for row in chain) == [False, True]
        assert all(row["profile_id"] == renamed["profile_id"] for row in chain)
        call(candidate, "profile", "rename", "old", "invalid-branch", error="profile_renamed")
        reverted = call(candidate, "profile", "rename", "next", "old")
        assert reverted["changed"] and reverted["profile_id"] == renamed["profile_id"]
        assert call(candidate, "var", "get", "FIXTURE", "--profile", "new")["value"] == "candidate-update"
        call(legacy, "var", "set", "FIXTURE", "--value", "separate", "--profile", "new")
        call(candidate, "profile", "list", error="profile_storage_conflict")
        call(candidate, "profile", "rename", "old", "fresh", error="profile_storage_conflict")
        return {"legacy": legacy_version, "candidate": candidate_version, "same_process_and_port": True,
                "same_proxy": True,
                "same_task_and_value_store": True, "legacy_exit_record": True,
                "aliases_and_conflicts": True, "cached_alias_queries": True}
    finally:
        try:
            if execution is not None:
                call(legacy, "process", "stop", execution)
        finally:
            try:
                call(legacy, "proxy", "stop", "--all")
            finally:
                stop_controller(state)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--legacy", type=Path, required=True)
    parser.add_argument("--legacy-commit")
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--candidate-commit")
    args = parser.parse_args()
    root = create_fixture_directory(prefix="devtools-profile-live-")
    try:
        result = verify(root, args.legacy.resolve(strict=True), args.candidate.resolve(strict=True))
    finally:
        for attempt in range(20):
            try:
                shutil.rmtree(root)
                break
            except PermissionError:
                if os.name != "nt" or attempt == 19:
                    raise
                time.sleep(0.1)
    for role, commit in [("legacy", args.legacy_commit), ("candidate", args.candidate_commit)]:
        if commit is not None:
            assert result[role]["commit"] == commit, (role, result[role])
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
