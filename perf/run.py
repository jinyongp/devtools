#!/usr/bin/env python3
"""Reproducible end-to-end measurements with hyperfine and private fixtures."""

import argparse
import csv
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import re
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request
import uuid
from dataclasses import asdict, dataclass


REPO = Path(__file__).resolve().parents[1]
GROUPS = ("cli", "project", "values", "tasks", "backup", "process", "proxy", "dashboard", "discovery")
GIT_ENV = (
    "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
    "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX", "GIT_CONFIG", "GIT_CONFIG_COUNT",
    "GIT_CONFIG_PARAMETERS", "GIT_NAMESPACE", "GIT_SHALLOW_FILE", "GIT_REPLACE_REF_BASE",
)
BENCHMARKS = "Benchmark(TaskStorage(CurrentList|Mutation|CursorSecondPage|WorkstreamListTail|LifecycleReplay)|TaskContextRecentHistory|ProxyRequestRegisteredProjects|BoundedLog)"


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def execute(argv, *, env=None, cwd=None, timeout=180):
    """Bound the command and its ordinary children, including hyperfine hooks."""
    process = subprocess.Popen(argv, env=env, cwd=cwd, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        stdout, stderr = process.communicate(timeout=timeout)
    except BaseException as error:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            process.communicate(timeout=3)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.communicate()
        if isinstance(error, subprocess.TimeoutExpired):
            # TimeoutExpired normally prints the full argv, including HTTP tokens.
            raise subprocess.TimeoutExpired(Path(argv[0]).name, timeout) from None
        raise
    if process.returncode:
        # Fixture command lines can contain session credentials; do not log argv.
        raise RuntimeError(f"{Path(argv[0]).name} exited {process.returncode}: {stderr.strip() or stdout[-2000:]}")
    return stdout


def isolated_env(root, inherited=None):
    if platform.system() != "Linux":
        raise RuntimeError("The end-to-end fixtures currently support Linux/WSL (XDG isolation).")
    env = dict(os.environ if inherited is None else inherited)
    for key in list(env):
        if key in GIT_ENV or key.startswith(("GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_", "DEVTOOLS_")):
            del env[key]
    for key, directory in {"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data",
                           "XDG_CACHE_HOME": "cache", "XDG_RUNTIME_DIR": "runtime"}.items():
        env[key] = str(root / directory)
    env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1", PYTHONDONTWRITEBYTECODE="1")
    return env


def assert_owned(root):
    marker = root / ".devtools-perf"
    if root.is_symlink() or not root.is_absolute() or marker.is_symlink() or not marker.is_file():
        raise RuntimeError("Refusing to operate on an unowned fixture.")
    for name in ("config", "data", "cache", "runtime", "baseline", "project"):
        path = root / name
        if path.is_symlink() or not path.resolve().is_relative_to(root.resolve()):
            raise RuntimeError("Fixture path escapes its root.")


@dataclass
class Case:
    name: str
    group: str
    argv: list
    commands: list
    reset: bool = False
    service: str = ""
    changed: bool = False


class Fixture:
    def __init__(self, root, binary, env, info=None):
        self.root, self.binary, self.env = root, binary, env
        self.project = root / "project"
        self.info = info or {}

    def cli(self, *args):
        result = json.loads(execute([str(self.binary), *map(str, args)], env=self.env,
                                    cwd=self.project, timeout=45))
        if not result.get("ok"):
            raise RuntimeError("Fixture command returned an unsuccessful response.")
        return result["data"]

    def cleanup(self):
        # Keep the registry until detached daemons have acknowledged shutdown.
        items = self.cli("process", "list").get("items", [])
        for item in items:
            if item["state"] in ("running", "starting", "unknown"):
                self.cli("process", "stop", item["id"], "--request-id", str(uuid.uuid4()))
        self.cli("proxy", "stop", "--request-id", str(uuid.uuid4()))
        self.cli("dashboard", "stop")
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            active = [item for item in self.cli("process", "list").get("items", [])
                      if item["state"] in ("running", "starting", "unknown")]
            if not active and not self.cli("proxy", "status")["item"]["running"] and not self.cli("dashboard", "status")["item"]["running"]:
                return
            time.sleep(0.1)
        raise RuntimeError(f"Fixture services did not stop; retained {self.root}")

    def reset(self):
        assert_owned(self.root)
        self.cleanup()
        for name in ("config", "data", "cache"):
            target = self.root / name
            shutil.rmtree(target)
            shutil.copytree(self.root / "baseline" / name, target)
        for name in ("new.age", "new.identity", "new.recipient"):
            (self.root / name).unlink(missing_ok=True)

    def service(self, name):
        if name in ("process", "proxy"):
            item = self.cli("process", "start", "web", "--capture-logs", "--request-id", str(uuid.uuid4()))["item"]
            self.cli("process", "wait", item["id"], "--timeout", "10s")
            self.info["process_id"] = item["id"]
        if name == "proxy":
            self.cli("proxy", "start", "--port", self.info["proxy_port"], "--request-id", str(uuid.uuid4()))
        if name == "dashboard":
            link = self.cli("dashboard")["item"]["url"]
            parsed = urllib.parse.urlsplit(link)
            origin = f"{parsed.scheme}://{parsed.netloc}"
            token = urllib.parse.parse_qs(parsed.fragment)["token"][0]
            request = urllib.request.Request(origin + "/session", data=json.dumps({"token": token}).encode(),
                                             headers={"Origin": origin, "Content-Type": "application/json"})
            # Explicitly bypass host proxy settings for private loopback traffic.
            with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=10) as response:
                session = json.load(response)["token"]
            self.info.update(dashboard_origin=origin, dashboard_session=session)

    def prepare(self, case):
        if case.reset:
            self.reset()
            if case.service:
                self.service(case.service)

    def conclude(self, case):
        if case.reset:
            self.cleanup()


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def make_fixture(root, binary, seed_binary, size, history, body_kib):
    env = isolated_env(root)
    for name in ("config", "data", "cache", "runtime", "project"):
        (root / name).mkdir(mode=0o700)
    (root / ".devtools-perf").touch(mode=0o600)
    project = root / "project"
    # A small real backend keeps service and HTTP measurements deterministic.
    (project / "server.py").write_text('''import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"fixture\\n")
    def log_message(self, *args):
        pass
ThreadingHTTPServer(("127.0.0.1", int(os.environ["PORT"])), Handler).serve_forever()
''')
    python = json.dumps(sys.executable)
    readiness = json.dumps('exec curl --noproxy "*" --fail --silent --output /dev/null "http://127.0.0.1:$PORT/"')
    (project / "devtools.toml").write_text(f'''profile = "perf"
[ports.web]
range = [20000, 59999]
[proxies.app]
host = "${{instance.alias}}.${{profile}}.localhost"
port = "web"
[commands.noop]
exec = ["true"]
[commands."dev:echo"]
exec = ["printf", "%s\\n"]
[commands.web]
exec = [{python}, "server.py"]
serve = ["web"]
[commands.web.bind]
PORT = {{ port = "web" }}
[commands.web.ready]
exec = ["sh", "-c", {readiness}]
timeout = "2s"
''')
    info = json.loads(execute([str(seed_binary), "--root", str(root), "--size", str(size),
                               "--history", str(history), "--body-kib", str(body_kib)], env=env, cwd=REPO))
    info["proxy_port"] = free_port()
    fixture = Fixture(root, binary, env, info)
    fixture.cli("instance", "name", "bench")
    fixture.cli("port", "allocate", "web")
    fixture.cli("backup", "keygen", "--identity-file", root / "identity", "--recipient-file", root / "recipient")
    fixture.cli("backup", "configure", "--directory", root / "backups", "--recipient-file", root / "recipient")
    fixture.cli("backup", "create", "--output", root / "baseline.age")
    (root / "synthetic-secret").write_text("updated synthetic secret")
    (root / "synthetic-secret").chmod(0o600)
    (root / "import.env").write_text("IMPORTED_KEY=synthetic-value\n")
    write_json(root / "edit.json", {"reason": "Fixture edit", "operations": [{"op": "workstream.update", "value": {"title": "Edited fixture"}}]})
    write_json(root / "spec.json", {"body": "Updated fixture document", "requirements": [], "acceptance": []})
    write_json(root / "plan.json", {"body": "Updated fixture document", "task_ids": [], "validation_ids": []})
    fixture.cleanup()
    (root / "baseline").mkdir(mode=0o700)
    for name in ("config", "data", "cache"):
        shutil.copytree(root / name, root / "baseline" / name)
    return fixture


def cases_for(fixture, catalog):
    root, info = fixture.root, fixture.info
    binary = str(fixture.binary)
    cases = []

    def add(group, name, args, *, reset=False, service="", changed=False, command=None, argv=None):
        cases.append(Case(f"{group}/{name}", group, argv or [binary, *map(str, args)],
                          [command or " ".join(args[:2])], reset, service, changed))

    for name, args in [("version", ["version"]), ("help", ["help"]), ("schema-all", ["schema", "--all"]),
                       ("schema-task", ["schema", "task", "add"]), ("completion-bash", ["completion", "bash"]),
                       ("completion-zsh", ["completion", "zsh"]), ("completion-fish", ["completion", "fish"])]:
        add("cli", name, args, command=args[0])
    for name, args in [("inspect", ["project", "inspect"]), ("commands", ["command", "list"]),
                       ("command-inspect", ["command", "inspect", "noop"]),
                       ("run-noop", ["command", "run", "noop"]),
                       ("run-arguments", ["command", "run", "dev:echo", "--flag", "value"]),
                       ("doctor", ["doctor", "noop"]), ("diagnostics", ["diagnostics"]),
                       ("port-list", ["port", "list"]), ("port-show", ["port", "show", "web"]),
                       ("port-check", ["port", "check", "web"]), ("instance-list", ["instance", "list"]),
                       ("instance-show", ["instance", "show"]), ("profile-list", ["profile", "list"]),
                       ("profile-inspect", ["profile", "inspect", "perf"]),
                       ("cleanup-preview", ["cleanup", "preview"]), ("cleanup-archives", ["cleanup", "archives"])]:
        add("project", name, args)
    for name, args in [("variable-list", ["variable", "list"]), ("variable-get", ["variable", "get", "KEY_000000"]),
                       ("secret-list", ["secret", "list"]), ("env-list", ["env", "list"])]:
        add("values", name, args)
    for name, args in [("variable-set", ["variable", "set", "KEY_000000", "--value", "updated"]),
                       ("variable-unset", ["variable", "unset", "KEY_000000"]),
                       ("secret-unset", ["secret", "unset", "SECRET"]),
                       ("env-create", ["env", "create", "new"]), ("env-remove", ["env", "remove", "local"])]:
        add("values", name, args, reset=True, changed=True)
    add("values", "secret-set", ["secret", "set", "SECRET", "--file", root / "synthetic-secret"], reset=True, changed=True)
    add("values", "import-preview", ["import", "--file", root / "import.env", "--dry-run"], command="import")
    add("values", "import", ["import", "--file", root / "import.env"], reset=True, changed=True, command="import")
    task_id = info["task_id"]
    for name, args in [("list", ["task", "list"]), ("show", ["task", "show", task_id]),
                       ("next", ["task", "next"]), ("current", ["task", "current"]),
                       ("context", ["task", "context", task_id]), ("history", ["task", "history", task_id]),
                       ("impact", ["task", "impact", task_id]), ("tree", ["task", "tree"]),
                       ("export", ["task", "export", task_id]), ("workstreams", ["task", "workstream", "list"]),
                       ("validations", ["task", "validation", "list"])]:
        add("tasks", name, args, command=" ".join(args[:3] if args[1] in ("workstream", "validation") else args[:2]))
    request = ["--request-id", str(uuid.uuid4())]
    add("tasks", "add", ["task", "add", "--title", "Added fixture", *request], reset=True, changed=True)
    add("tasks", "update", ["task", "update", task_id, "--if-revision", str(info["revision"]),
                             "--description", "Updated fixture", *request], reset=True, changed=True)
    add("tasks", "claim", ["task", "claim", task_id, *request], reset=True, changed=True)
    add("tasks", "workstream-create", ["task", "workstream", "create", "--title", "New stream", *request],
        reset=True, changed=True, command="task workstream create")
    for action in ("hold", "cancel"):
        add("tasks", action, ["task", action, task_id, "--if-revision", str(info["revision"]),
                               "--reason", "Fixture intervention", *request], reset=True, changed=True)
    workstream_id = info["workstream_id"]
    for action in ("show", "context", "history", "impact", "tree", "export", "check"):
        add("tasks", "workstream-" + action, ["task", "workstream", action, workstream_id], command="task workstream " + action)
    for action in ("spec", "plan"):
        add("tasks", "workstream-" + action, ["task", "workstream", action, "show", workstream_id], command="task workstream " + action + " show")
        add("tasks", "workstream-" + action + "-set", ["task", "workstream", action, "set", workstream_id,
            "--if-revision", str(info["revision"]), "--file", root / (action + ".json"), *request],
            reset=True, changed=True, command="task workstream " + action + " set")
    for preview in (True, False):
        add("tasks", "workstream-edit" + ("-preview" if preview else ""), ["task", "workstream", "edit", workstream_id,
            "--if-revision", str(info["revision"]), "--file", root / "edit.json", *(["--dry-run"] if preview else request)],
            reset=not preview, changed=not preview, command="task workstream edit")
    add("backup", "status", ["backup", "status"])
    add("backup", "inspect", ["backup", "inspect", "--file", root / "baseline.age", "--identity-file", root / "identity"])
    add("backup", "create", ["backup", "create", "--output", root / "new.age"], reset=True)
    add("backup", "keygen", ["backup", "keygen", "--identity-file", root / "new.identity", "--recipient-file", root / "new.recipient"], reset=True)
    add("backup", "restore-preview", ["backup", "restore", "--file", root / "baseline.age", "--identity-file", root / "identity",
                                        "--profile", "perf", "--as", "restored"])
    add("process", "start", ["process", "start", "web", "--capture-logs", *request], reset=True, changed=True)
    add("process", "list", ["process", "list"], service="process")
    for action in ("status", "logs", "check", "wait"):
        args = ["process", action, "{process_id}"]
        if action == "wait":
            args += ["--timeout", "10s"]
        add("process", action, args, service="process")
    for action in ("status", "logs"):
        add("process", "project-" + action, ["project", action, "web"], service="process")
    add("process", "project-down", ["project", "down", *request], reset=True, service="process")
    add("proxy", "start", ["proxy", "start", "--port", info["proxy_port"], *request], reset=True)
    add("proxy", "status", ["proxy", "status"], service="proxy")
    add("proxy", "list", ["proxy", "list"], service="proxy")
    add("proxy", "stop", ["proxy", "stop", *request], reset=True, service="proxy")
    curl = ["curl", "--noproxy", "*", "--fail", "--silent", "--show-error", "--max-time", "10"]
    add("proxy", "http", [], service="proxy", command="proxy HTTP", argv=[*curl, "--header", "Host: bench.perf.localhost",
                                                                          f"http://127.0.0.1:{info['proxy_port']}/"])
    add("dashboard", "start", ["dashboard"], reset=True, command="dashboard")
    add("dashboard", "reuse", ["dashboard"], service="dashboard", command="dashboard")
    add("dashboard", "status", ["dashboard", "status"], service="dashboard", command="dashboard status")
    for name, path in [("page", "/"), ("profiles", "/api/profiles"),
                       ("values", "/api/values?profile=perf"), ("processes", "/api/processes?profile=perf"),
                       ("workstreams", "/api/query?profile=perf&command=workstream%20list"),
                       ("task-list", "/api/query?profile=perf&command=list"),
                       ("task-context", f"/api/query?profile=perf&command=context&id={task_id}")]:
        add("dashboard", name, [], service="dashboard", command="Dashboard HTTP",
            argv=[*curl, "--header", "Authorization: Bearer {dashboard_session}", "{dashboard_origin}" + path])
    for command in catalog["commands"]:
        name = command["name"] if isinstance(command, dict) else command
        if isinstance(name, list):
            name = " ".join(name)
        add("discovery", name.replace(" ", "-"), [*name.split(), "--help"], command=name)
    return cases


def resolved_case(case, info):
    # Substitute only explicitly defined placeholders; literal child braces survive.
    argv = []
    for argument in case.argv:
        for key in ("process_id", "dashboard_session", "dashboard_origin"):
            if "{" + key + "}" in argument:
                argument = argument.replace("{" + key + "}", str(info[key]))
        argv.append(argument)
    return Case(case.name, case.group, argv, case.commands, case.reset, case.service, case.changed)


def validate_preflight(case, stdout):
    if case.changed:
        data = json.loads(stdout)["data"]
        if data.get("changed") is not True or data.get("replayed") is True:
            raise RuntimeError(f"{case.name} did not perform a fresh mutation")


def require_hyperfine(version):
    match = re.fullmatch(r"hyperfine (\d+)\.(\d+)\.\d+(?:\S*)", version)
    if not match or tuple(map(int, match.groups())) < (1, 20):
        raise RuntimeError("hyperfine 1.20 or newer is required")


def valid_result(raw, runs):
    results = raw.get("results", [])
    if len(results) != 1:
        raise ValueError("Expected exactly one hyperfine result.")
    result = results[0]
    samples = result.get("times", [])
    if len(samples) != runs or any(not math.isfinite(t) or t <= 0 for t in samples):
        raise ValueError("Missing or invalid timing samples.")
    if result.get("exit_codes") != [0] * runs:
        raise ValueError("A measured command failed.")
    for key in ("mean", "stddev", "min", "max", "median"):
        if not math.isfinite(result[key]) or result[key] < 0:
            raise ValueError("Invalid timing statistics.")
    ordered = sorted(samples)
    result["p95"] = ordered[max(0, math.ceil(len(ordered) * 0.95) - 1)]
    return result


def cpu_name():
    if platform.system() == "Linux":
        for line in Path("/proc/cpuinfo").read_text().splitlines():
            if line.startswith("model name"):
                return line.split(":", 1)[1].strip()
    return platform.processor()


def compare(current, baseline):
    # Versions/hashes may change; hardware, sampler and fixture parameters must match.
    for key in ("system", "machine", "cpu", "kernel", "runs", "warmup", "hyperfine", "measurement"):
        if current["metadata"].get(key) != baseline["metadata"].get(key):
            raise ValueError(f"Baseline has incompatible {key}.")
    if not baseline.get("complete"):
        raise ValueError("Baseline is incomplete.")
    old = {row["key"]: row for row in baseline["results"]}
    for row in current["results"]:
        previous = old.get(row["key"])
        row["change_percent"] = None if previous is None else (row["mean"] / previous["mean"] - 1) * 100


def reports(output, summary):
    write_json(output / "summary.json", summary)
    columns = ("key", "mean", "median", "p95", "stddev", "min", "max", "change_percent")
    with (output / "summary.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, columns, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(summary["results"])
    lines = ["# devtools performance", "", "Times in milliseconds. Positive change means slower.", "",
             "| Case / fixture | Mean | p95 | Stddev | Change |", "| --- | ---: | ---: | ---: | ---: |"]
    for row in summary["results"]:
        change = row.get("change_percent")
        label = row["key"].replace("|", "\\|")
        lines.append(f"| {label} | {row['mean'] * 1000:.3f} | {row['p95'] * 1000:.3f} | {row['stddev'] * 1000:.3f} | "
                     + ("—" if change is None else f"{change:+.1f}%") + " |")
    if not summary["complete"]:
        lines += ["", "This run is incomplete; see `summary.json` for the failure."]
    (output / "summary.md").write_text("\n".join(lines) + "\n")


def positive(value):
    number = int(value)
    if number < 1:
        raise argparse.ArgumentTypeError("must be positive")
    return number


def nonnegative(value):
    number = int(value)
    if number < 0:
        raise argparse.ArgumentTypeError("must be nonnegative")
    return number


def numbers(value, minimum=0):
    try:
        result = list(dict.fromkeys(int(part) for part in value.split(",")))
        if not result or min(result) < minimum:
            raise ValueError()
        return result
    except ValueError as exc:
        raise argparse.ArgumentTypeError(f"expected comma-separated integers >= {minimum}") from exc


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("groups", nargs="*", help="Groups (default: all); use --list to inspect cases")
    parser.add_argument("--list", action="store_true")
    parser.add_argument("--case", help="Select case names containing this text")
    parser.add_argument("--sizes", type=lambda value: numbers(value, 1), default=[10, 100, 1000])
    parser.add_argument("--histories", type=numbers, default=[0], help="Additional updates; crossed with --sizes")
    parser.add_argument("--body-kib", type=numbers, default=[1], help="Task description KiB; crossed with --sizes and --histories")
    parser.add_argument("--runs", type=positive, default=10)
    parser.add_argument("--warmup", type=nonnegative, default=3)
    parser.add_argument("--timeout", type=positive, default=300, help="Wall time budget for hyperfine, including hooks")
    parser.add_argument("--binary", type=Path, help="Measure this binary; default builds current source")
    parser.add_argument("--hyperfine", default="hyperfine")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--compare", type=Path, help="Previous summary.json from a compatible host/configuration")
    parser.add_argument("--go", action="store_true", help="Run internal Go benchmarks instead of end-to-end cases")
    parser.add_argument("--benchtime", default="3x", help="Go benchmark iteration/time budget")
    parser.add_argument("--hook", nargs=3, metavar=("PHASE", "MANIFEST", "CASE"), help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.hook:
        phase, manifest, name = args.hook
        data = json.loads(Path(manifest).read_text())
        root = Path(data["root"])
        assert_owned(root)
        fixture = Fixture(root, Path(data["binary"]), isolated_env(root), data["info"])
        case = Case(**data["cases"][name])
        if phase not in ("prepare", "conclude"):
            raise ValueError("Unknown hook phase")
        getattr(fixture, phase)(case)
        return
    groups = set(args.groups or GROUPS)
    unknown = groups - (set(GROUPS) | {"all"})
    if unknown:
        parser.error("unknown groups: " + ", ".join(sorted(unknown)))
    if "all" in groups:
        groups = set(GROUPS)
    if args.runs < 2:
        parser.error("--runs must be at least 2 for sample statistics")
    if args.go and (args.case or args.compare or args.binary or args.groups):
        parser.error("--go supports --runs, --benchtime and --output; case selection and comparison use hyperfine")
    if args.list:
        if args.go:
            print(BENCHMARKS)
            return
        dummy = Fixture(Path("/fixture"), Path("/binary"), {}, {"task_id": "TASK", "workstream_id": "WORKSTREAM", "revision": 11, "proxy_port": 20200})
        for case in cases_for(dummy, {"commands": []}):
            if case.group in groups and (not args.case or args.case in case.name):
                print(case.name)
        if "discovery" in groups:
            print("discovery/<every canonical command from devtools schema --all>")
        return
    output = (args.output or REPO / "perf-results" / time.strftime("%Y%m%d-%H%M%S")).resolve()
    # Never append fresh results to a previous or partial run.
    output.mkdir(parents=True, exist_ok=False)
    if args.go:
        stdout = execute(["go", "test", "-p", "1", "-run", "^$", "-bench", BENCHMARKS, "-benchmem",
                          "-benchtime", args.benchtime, "-count", str(args.runs), "-timeout", "30m",
                          "./internal/tasks", "./internal/proxy", "./internal/services"], cwd=REPO, timeout=1800)
        (output / "go-benchmarks.txt").write_text(stdout)
        print(f"Go benchmarks: {output / 'go-benchmarks.txt'}")
        return
    hyperfine = shutil.which(args.hyperfine)
    if not hyperfine and args.hyperfine == "hyperfine" and os.access(REPO / "bin" / "hyperfine", os.X_OK):
        hyperfine = str(REPO / "bin" / "hyperfine")
    if not hyperfine:
        raise RuntimeError("hyperfine is required; install it or pass --hyperfine /path/to/hyperfine")
    hyperfine = str(Path(hyperfine).resolve())
    hyperfine_version = execute([hyperfine, "--version"]).strip()
    require_hyperfine(hyperfine_version)
    summary = {"complete": False, "metadata": {"system": platform.system(), "machine": platform.machine(),
               "cpu": cpu_name(), "runs": args.runs, "warmup": args.warmup,
               "hyperfine": hyperfine_version, "measurement": "hyperfine-shell-none-output-pipe-v1",
               "kernel": platform.release(), "python": platform.python_version()}, "results": []}
    scratch = Path(tempfile.mkdtemp(prefix="devtools-perf-"))
    fixture = None
    cleanup_ok = True
    try:
        # Reject unsupported isolation before running any devtools command.
        isolated_env(scratch)
        binary = args.binary.resolve() if args.binary else scratch / "devtools"
        if not args.binary:
            execute(["go", "build", "-trimpath", "-o", str(binary), "./cmd/devtools"], cwd=REPO)
        seed_binary = scratch / "fixture"
        execute(["go", "build", "-trimpath", "-o", str(seed_binary), "./perf/fixture"], cwd=REPO)
        summary["metadata"].update(binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                                   binary_version=json.loads(execute([str(binary), "version"])),
                                   source_commit=execute(["git", "rev-parse", "HEAD"], cwd=REPO).strip(),
                                   source_dirty=bool(execute(["git", "status", "--porcelain"], cwd=REPO).strip()))
        catalog = json.loads(execute([str(binary), "schema", "--all"]))["data"]
        write_json(output / "catalog.json", catalog)
        selected_commands = set()
        discovered_commands = set()
        http_cases = set()
        for size in args.sizes:
            for history in args.histories:
                for body_kib in args.body_kib:
                    root = scratch / f"fixture-{size}-{history}-{body_kib}"
                    root.mkdir(mode=0o700)
                    # Set fixture before setup so failed setup still has a cleanup path.
                    fixture = Fixture(root, binary, isolated_env(root))
                    fixture = make_fixture(root, binary, seed_binary, size, history, body_kib)
                    selected = [case for case in cases_for(fixture, catalog) if case.group in groups
                                and (not args.case or args.case in case.name)]
                    if not selected:
                        raise ValueError("No cases match the selected groups/filter.")
                    for number, original in enumerate(selected):
                        key = f"{original.name}:size={size}:history={history}:body_kib={body_kib}"
                        print(f"[{number + 1}/{len(selected)}] {key}", flush=True)
                        summary["active_case"] = key
                        fixture.reset()
                        if original.service and not original.reset:
                            fixture.service(original.service)
                        fixture.prepare(original)
                        case = resolved_case(original, fixture.info)
                        preflight = execute(case.argv, env=fixture.env, cwd=fixture.project, timeout=45)
                        validate_preflight(case, preflight)
                        fixture.conclude(case)
                        manifest = root / "manifest.json"
                        write_json(manifest, {"root": str(root), "binary": str(binary), "info": fixture.info,
                                             "cases": {case.name: asdict(case)}})
                        manifest.chmod(0o600)
                        raw_path = output / f"{size}-{history}-{body_kib}-{number:03d}.json"
                        command = [hyperfine, "--shell=none", "--output=pipe", "--runs", str(args.runs),
                                   "--warmup", str(args.warmup), "--command-name", key, "--export-json", str(raw_path)]
                        if case.reset:
                            for phase in ("prepare", "conclude"):
                                hook = [sys.executable, str(Path(__file__).resolve()), "--hook", phase, str(manifest), case.name]
                                command += ["--" + phase, shlex.join(hook)]
                        command.append(shlex.join(case.argv))
                        log = execute(command, env=fixture.env, cwd=fixture.project, timeout=args.timeout)
                        (output / f"{raw_path.stem}.log").write_text(log)
                        result = valid_result(json.loads(raw_path.read_text()), args.runs)
                        summary["results"].append({"key": key, "name": case.name, "size": size, "history": history,
                                                   "body_kib": body_kib, **{k: result[k] for k in ("mean", "median", "p95", "stddev", "min", "max", "times")}})
                        if case.group == "discovery":
                            discovered_commands.update(case.commands)
                        elif case.argv[0] == "curl":
                            http_cases.add(case.name)
                        else:
                            selected_commands.update(case.commands)
                        fixture.cleanup()
                        reports(output, summary)
                    fixture.cleanup()
                    fixture = None
        names = [command["name"] for command in catalog["commands"]]
        write_json(output / "coverage.json", {"runtime_commands": sorted(selected_commands),
                   "runtime_http_cases": sorted(http_cases),
                   "discovery_commands": sorted(discovered_commands),
                   "discovery_only_commands": sorted(discovered_commands - selected_commands),
                   "unmeasured_commands": sorted(set(names) - selected_commands - discovered_commands),
                   "limits": ["HTTP measurements include curl startup; browser rendering is not measured.",
                              "Network updates, interactive editors, destructive lifecycle variants and migration are discovery-only.",
                              "Projects/proxy routes: one; use just perf-go for the 1/50/200-project resolver matrix."]})
        summary["complete"] = True
        summary.pop("active_case", None)
        if args.compare:
            compare(summary, json.loads(args.compare.read_text()))
    except BaseException as exc:
        summary["complete"] = False
        summary["failure"] = f"{type(exc).__name__}: {exc}"
        raise
    finally:
        if fixture and fixture.project.is_dir():
            try:
                fixture.cleanup()
            except Exception as exc:
                cleanup_ok = False
                summary["complete"] = False
                summary["cleanup_failure"] = str(exc)
                print(f"Cleanup failed; fixture retained at {scratch}: {exc}", file=sys.stderr)
        reports(output, summary)
        if cleanup_ok:
            shutil.rmtree(scratch)
    if not cleanup_ok:
        raise RuntimeError("Performance run cleanup failed")
    print(f"Results: {output / 'summary.md'}", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        print(f"perf: {error}", file=sys.stderr)
        sys.exit(1)
