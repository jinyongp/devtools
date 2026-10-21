#!/usr/bin/env python3
"""Matched protected automation benchmarks using immutable compiler overlays."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import signal
import shutil
import statistics
import subprocess
import sys
import tempfile
import time

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "scripts"))
from vault_test_support import retire_vaults

CASES = {
    "edit": ("values", "BenchmarkAutomationFreshEdit"),
    "replay": ("values", "BenchmarkAutomationReplay"),
    "lookup": ("values", "BenchmarkAutomationLookup"),
    "legacy_lookup": ("values", "BenchmarkAutomationLegacyLookup"),
    "prepare": ("valuehandoff", "BenchmarkAutomationPrepare"),
    "deliver": ("valuehandoff", "BenchmarkAutomationDeliver"),
}
GIT_ENV = {"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
           "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX",
           "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_NAMESPACE",
           "GIT_SHALLOW_FILE", "GIT_REPLACE_REF_BASE", "GIT_GRAFT_FILE"}


def digest(body):
    return hashlib.sha256(body).hexdigest()


def identity(repo, env):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(repo), *args], env=env, text=True).strip()
    return {"commit": git("rev-parse", "HEAD"), "dirty": bool(git("status", "--porcelain"))}


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise ValueError("Gate instrumentation anchor must occur exactly once")
    return text.replace(old, new, 1)


def overlay(repo, candidate, package, scratch):
    """Generate compiler inputs, without changing a checkout or stored fixture."""
    replacements, sources = {}, []

    def add(relative, body=None, source=None):
        original = repo / relative
        if source is not None:
            backing = candidate / source
            raw = backing.read_bytes()
        else:
            raw = body.encode()
            backing = scratch / (relative.replace("/", "_") + ".go")
            backing.write_bytes(raw)
        replacements[str(original)] = str(backing)
        sources.append({"path": relative, "original_sha256": digest(original.read_bytes()) if original.exists() else None,
                        "compiler_input_sha256": digest(raw)})

    add(f"internal/{package}/automation_benchmark_test.go", source=f"internal/{package}/automation_benchmark_test.go")
    if package == "valuehandoff":
        for name in ("prepare_test.go", "runner_test.go"):
            # Identical owned fixture setup; the candidate adapters accept TB
            # so benchmarks share the integration fixture rather than bypassing it.
            add("internal/valuehandoff/" + name, source="internal/valuehandoff/" + name)
        source = (repo / "internal/valuehandoff/store.go").read_text()
        source = replace_once(source, "release := func() { gate(); slot() }",
                              's.hook("bench-gate-acquired")\n\trelease := func() { gate(); s.hook("bench-gate-released"); slot() }')
        add("internal/valuehandoff/store.go", source)
        source = (repo / "internal/valuehandoff/deliver.go").read_text()
        source = replace_once(source, "\tgateHeld := true", '\ts.hook("bench-intent-gate-acquired")\n\tgateHeld := true')
        source = replace_once(source, "\tgate()\n\tgateHeld = false", '\tgate()\n\ts.hook("bench-intent-gate-released")\n\tgateHeld = false')
        add("internal/valuehandoff/deliver.go", source)
    config = scratch / "overlay.json"
    config.write_text(json.dumps({"Replace": replacements}))
    return config, sources


def parse_samples(output, repeats):
    rows = [json.loads(line.split("AUTOMATION_SAMPLE ", 1)[1]) for line in output.splitlines() if "AUTOMATION_SAMPLE " in line]
    if len(rows) != repeats or [r["sample"] for r in rows] != list(range(repeats)):
        raise ValueError("Expected every ordered single-operation sample exactly once")
    for row in rows:
        metrics = row["metrics"]
        if set(metrics) != {"ns/op", "B/op", "allocs/op", "maintenance-held-ns/op"} or any(type(v) not in (int, float) or not math.isfinite(v) or v < 0 for v in metrics.values()):
            raise ValueError("Missing or invalid required benchmark metric")
        if metrics["ns/op"] <= 0 or metrics["maintenance-held-ns/op"] > metrics["ns/op"]:
            raise ValueError("Invalid wall/gate timing")
    return [row["metrics"] for row in rows]


def execute(command, repo, env, deadline):
    child = subprocess.Popen(command, cwd=repo, env=env, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        output, error = child.communicate(timeout=max(.1, deadline - time.monotonic()))
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        output,error=child.communicate(timeout=10)
        return 124,output,error
    except BaseException:
        os.killpg(child.pid, signal.SIGKILL)
        child.communicate(timeout=10)
        raise
    return child.returncode, output, error


def summarize(rows):
    result = []
    for case in dict.fromkeys(row["case"] for row in rows):
        for size in sorted({row["receipts"] for row in rows if row["case"] == case}):
            item = {"case": case, "receipts": size, "arms": {}, "candidate_change_percent": {}}
            for arm in ("baseline", "candidate"):
                samples = [r["metrics"] for r in rows if r["case"] == case and r["receipts"] == size and r["arm"] == arm]
                if not samples:
                    continue
                item["arms"][arm] = {metric: {"median": statistics.median(s[metric] for s in samples),
                                             "min": min(s[metric] for s in samples), "max": max(s[metric] for s in samples),
                                             "n": len(samples)} for metric in samples[0]}
            if len(item["arms"]) == 2:
                for metric, base in item["arms"]["baseline"].items():
                    if base["median"]:
                        value = item["arms"]["candidate"][metric]["median"]
                        item["candidate_change_percent"][metric] = 100 * (value / base["median"] - 1)
            result.append(item)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, default=REPO)
    parser.add_argument("--mutation-baseline", type=Path, required=True)
    parser.add_argument("--handoff-baseline", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--cases", default=",".join(CASES))
    parser.add_argument("--sizes", default="1000,10000")
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--budget-seconds", type=int, default=1200)
    args = parser.parse_args()
    cases, sizes = args.cases.split(","), [int(s) for s in args.sizes.split(",")]
    if sys.platform != "linux" or not cases or any(c not in CASES for c in cases) or not sizes or any(s not in (1000, 10000) for s in sizes) or args.repeats < 1 or args.budget_seconds < 1:
        parser.error("Use Linux/WSL, known cases, sizes1000/10000 and positive repeats/budget")
    paths = {"candidate": args.candidate.resolve(), "mutation_baseline": args.mutation_baseline.resolve(), "handoff_baseline": args.handoff_baseline.resolve()}
    env = {k: v for k, v in os.environ.items() if k not in GIT_ENV and not k.startswith("DEVTOOLS_")}
    env.update(LC_ALL="C", CGO_ENABLED="0", GOMAXPROCS="1", GODEBUG="", GOFLAGS="", GOWORK="off")
    args.output.mkdir(parents=True, exist_ok=False)
    report = {"measurement": "protected-automation-receipts-v1", "complete": False, "rows": [],
              "platform": platform.platform(), "runtime": subprocess.check_output(["go", "version"], text=True, env=env).strip(),
              "source": {name: identity(path, env) for name, path in paths.items()},
              "build": {"race": False, "cgo": False, "gomaxprocs": 1, "benchtime": f"{args.repeats}x", "trimpath": True},
              "harness_sha256": digest(Path(__file__).read_bytes()),
              "limitations": ["Baselines already contain the measured API; this is not a pre-feature comparison for absent APIs.",
                              "Setup, explicit unlock, independently fsynced authenticated no-op corpus and cleanup are untimed.",
                              "Wall time includes real protected storage/private IPC and identical private gate-timing callbacks.",
                              "Go allocation bytes/counts cover the benchmark process and its receiver, not the separate session daemon or C allocations.",
                              "Repeated operations share one authenticated corpus per arm/case/size; owned target metadata is reset untimed. Samples are correlated and caches warm.",
                              "Arm order alternates between case/size blocks. Three samples describe spread, not equivalence, significance or a release SLO.",
                              "Receipt counts apply to the relevant namespace; constant setup metadata and transport leases are separate.",
                              "Legacy lookup directory scanning is intentionally a separate workload; no crypto/fsync/masking/expiry check is removed."]}
    started = time.monotonic()
    deadline = started + args.budget_seconds

    def save():
        report["elapsed_seconds"] = time.monotonic() - started
        report["summary"] = summarize(report["rows"])
        (args.output / "results.json").write_text(json.dumps(report, indent=2) + "\n")

    save()
    try:
        block = 0
        for case in cases:
            package, bench = CASES[case]
            base = paths["mutation_baseline" if package == "values" else "handoff_baseline"]
            for size in sizes:
                arms = [("baseline", base), ("candidate", paths["candidate"])]
                if block % 2:
                    arms.reverse()
                block += 1
                for arm, repo in arms:
                    if time.monotonic() >= deadline:
                        raise TimeoutError("Measurement budget exhausted")
                    label = f"{case}-{size}-{arm}"
                    scratch = Path(tempfile.mkdtemp(prefix="devtools-automation-bench-")).resolve()
                    try:
                        config, sources = overlay(repo, paths["candidate"], package, scratch)
                        fixture = scratch / "fixtures"
                        fixture.mkdir(mode=0o700)
                        command = ["go", "test", "-overlay", str(config), "-tags", "automationbench", "-trimpath", "-run", "^$", "-v",
                                   "-bench", f"^{bench}$/^receipts_{size}$", f"-benchtime={args.repeats}x", "-count=1", "-benchmem", "-cpu=1", "-timeout=3m", "./internal/" + package]
                        try:
                            code, output, error = execute(command, repo, dict(env, TMPDIR=str(fixture)), deadline)
                            (args.output / (label + ".log")).write_text(output + error)
                            if code:
                                raise RuntimeError("Benchmark failed; see " + label + ".log")
                            for repeat, metrics in enumerate(parse_samples(output, args.repeats)):
                                report["rows"].append({"case": case, "receipts": size, "repeat": repeat, "arm": arm,
                                                       "metrics": metrics, "source": identity(repo, env), "compiler_inputs": sources,
                                                       "command": command, "raw_log": label + ".log"})
                                print(json.dumps({"case": case, "receipts": size, "repeat": repeat, "arm": arm, "ms": metrics["ns/op"] / 1e6}), flush=True)
                            save()
                        finally:
                            retire_vaults(*(p.parent for p in fixture.rglob("session.json") if p.parent.name == "vault"))
                    except BaseException:
                        # Keep the private root if retirement or any cleanup fails.
                        print("Retained owned benchmark fixture: " + str(scratch), file=sys.stderr)
                        raise
                    else:
                        shutil.rmtree(scratch)
        report["complete"] = True
        save()
    except BaseException as error:
        report["failure"] = type(error).__name__
        save()
        raise
    return 0


if __name__ == "__main__":
    sys.exit(main())
