#!/usr/bin/env python3
"""Measure real encrypted bulk writes using independent disposable roots."""

import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import sys
import tempfile
import time

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "scripts"))
from vault_test_support import retire_vaults


def binary_identity(path):
    version = subprocess.run([str(path), "version"], capture_output=True, check=True, timeout=10)
    return {"sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "version": json.loads(version.stdout)["data"]}


class Fixture:
    def __init__(self, binary):
        self.binary = binary
        self.root = Path(tempfile.mkdtemp(prefix="devtools-mutations-")).resolve()
        self.root.chmod(0o700)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("DEVTOOLS_")}
        self.env.update(DEVTOOLS_ISOLATED_ROOT=str(self.root), LC_ALL="C")
        self.serial = 0

    def call(self, *args, timed=False):
        argv = [str(self.binary), *map(str, args)]
        self.serial += 1
        rss = self.root / ("rss-%d.txt" % self.serial)
        if timed:
            argv = ["/usr/bin/time", "-f", "%M", "-o", str(rss), *argv]
        start = time.perf_counter_ns()
        r = subprocess.run(argv, cwd=self.root, env=self.env, capture_output=True, timeout=240)
        elapsed = time.perf_counter_ns() - start
        if r.returncode or r.stderr:
            # Inputs are synthetic, but keep unexpected command output out of reports.
            raise RuntimeError("Fixture command failed: " + " ".join(map(str, args[:2])))
        data = json.loads(r.stdout)["data"]
        return data, {"wall_ns": elapsed, "peak_cli_and_waited_children_RSS_KiB": int(rss.read_text().strip())} if timed else None

    def setup(self):
        self.call("init", "--profile", "bulk")
        r = subprocess.run([str(self.binary), "vault", "init", "--backend", "passphrase", "--passphrase-stdin"],
                           cwd=self.root, env=self.env, input=b"synthetic-mutation-passphrase\n",
                           capture_output=True, timeout=30)
        if r.returncode or r.stderr:
            raise RuntimeError("Cannot initialize synthetic passphrase fixture")

    def source(self, count, label):
        path = self.root / (label + ".env")
        path.write_text("".join("TOKEN_%04d=synthetic-%s-%04d-abcdefghijklmnop\n" % (i, label, i) for i in range(count)))
        path.chmod(0o600)
        return path

    def close(self):
        try:
            retire_vaults(self.root / "data" / "vault")
        except BaseException:
            raise RuntimeError("Cannot confirm daemon retirement; fixture retained at " + str(self.root)) from None
        shutil.rmtree(self.root)


def measure(binary, size, repeat):
    fixture = Fixture(binary)
    try:
        fixture.setup()
        source = fixture.source(size, "a")
        data, sample = fixture.call("import", "--file", source, timed=True)
        if not data["changed"] or len(data["items"]) != size or any(i["action"] != "add" for i in data["items"]):
            raise RuntimeError("Import did not add the expected values")
        samples = {"new_import": sample}
        if size <= 1000:
            replacement = fixture.source(size, "b")
            data, sample = fixture.call("import", "--file", replacement, "--overwrite", timed=True)
            if not data["changed"] or len(data["items"]) != size or any(i["action"] != "update" for i in data["items"]):
                raise RuntimeError("Overwrite did not update every selected slot")
            samples["overwrite"] = sample
            # Profile filenames are opaque and versioned; select profile documents by shape.
            images = [p for p in (fixture.root / "data" / "profiles").glob("*.json") if "values" in json.loads(p.read_text())]
            before = {p.name: p.read_bytes() for p in images}
            data, sample = fixture.call("import", "--file", replacement, "--overwrite", timed=True)
            if data["changed"] or any(i["action"] != "unchanged" for i in data["items"]):
                raise RuntimeError("Same-value import changed versions")
            if any(p.read_bytes() != before[p.name] for p in images):
                raise RuntimeError("Same-value import rewrote the profile")
            samples["same_value"] = sample
            preview = fixture.source(size, "c")
            data, sample = fixture.call("import", "--file", preview, "--overwrite", "--dry-run", timed=True)
            if data["changed"] or not data["applicable"] or any(i["action"] != "update" for i in data["items"]):
                raise RuntimeError("Preview classification changed")
            if any(p.read_bytes() != before[p.name] for p in images):
                raise RuntimeError("Preview changed the profile")
            samples["dry_run"] = sample
        data, _ = fixture.call("sec", "list")
        if len(data["items"]) != size:
            raise RuntimeError("Stored secret inventory differs from import")
        selection = "all"
        if size > 4096:
            # Bulk storage capacity is separate from the existing output matcher
            # cap. Authenticate the whole profile and open one imported slot.
            bundles, _ = fixture.call("bundle", "list")
            selection = "mutation-check"
            fixture.call("bundle", "set", selection, "--key", "TOKEN_0000",
                         "--revision", bundles["revision"],
                         "--request-id", "00000000-0000-4000-8000-000000000501")
        r = subprocess.run([str(binary), "run", "--bundle", selection, "--", "/usr/bin/true"], cwd=fixture.root,
                           env=fixture.env, capture_output=True, timeout=30)
        if r.returncode or r.stdout or r.stderr:
            raise RuntimeError("Imported encrypted profile cannot prepare a real child execution")
        return {"size": size, "repeat": repeat, "input_bytes": source.stat().st_size, "execution_selection": selection, "samples": samples}
    finally:
        fixture.close()


def contention(binary, count):
    fixture = Fixture(binary)
    writer = None
    try:
        fixture.setup()
        fixture.call("var", "set", "URL", "--profile", "small", "--value", "https://example.invalid")
        fixture.call("import", "--profile", "small", "--file", fixture.source(1, "small"))
        source = fixture.source(count, "concurrent")
        started = time.perf_counter_ns()
        writer = subprocess.Popen([str(binary), "import", "--file", str(source)], cwd=fixture.root,
                                  env=fixture.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        time.sleep(.1)

        def read(args):
            running = writer.poll() is None
            start = time.perf_counter_ns()
            r = subprocess.run([str(binary), *args], cwd=fixture.root, env=fixture.env,
                               capture_output=True, timeout=240)
            code = None
            if r.returncode:
                code = json.loads(r.stderr)["error"]["code"]
            return {"args": args, "writer_running_at_start": running, "wall_ns": time.perf_counter_ns() - start,
                    "exit_code": r.returncode, "error_code": code}

        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
            reads = list(pool.map(read, [["run", "--profile", "small", "--", "/usr/bin/true"],
                                         ["var", "get", "URL", "--profile", "small"], ["sec", "list", "--profile", "small"]]))
        out, err = writer.communicate(timeout=240)
        if writer.returncode or err or len(json.loads(out)["data"]["items"]) != count:
            raise RuntimeError("Concurrent writer failed")
        return {"size": count, "writer_wall_until_readers_collected_ns": time.perf_counter_ns() - started, "reads": reads}
    finally:
        if writer is not None and writer.poll() is None:
            writer.terminate()
            try:
                writer.communicate(timeout=240)
            except subprocess.TimeoutExpired:
                writer.kill()
                writer.communicate(timeout=5)
        fixture.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--sizes", default="100,1000,5000")
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--large-repeats", type=int, default=1)
    parser.add_argument("--contention-size", type=int, default=4000)
    args = parser.parse_args()
    try:
        sizes = [int(n) for n in args.sizes.split(",")]
    except ValueError:
        parser.error("Sizes must be comma-separated integers")
    if not sys.platform.startswith("linux") or not Path("/usr/bin/time").is_file():
        parser.error("Requires Linux/WSL and GNU time")
    if not sizes or len(set(sizes)) != len(sizes) or min(sizes) < 1 or max(sizes) > 5000 or min(args.repeats, args.large_repeats) < 1 or args.contention_size < 0 or args.contention_size > 5000:
        parser.error("Use distinct sizes 1..5000, positive repetitions, and contention size 0..5000")
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    result = {"measurement": "encrypted-bulk-mutations-v1", "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              "platform": platform.platform(), "python": platform.python_version(),
              "go": subprocess.check_output(["go", "version"], text=True).strip(),
              "baseline": binary_identity(baseline), "candidate": binary_identity(candidate),
              "rows": [], "contention": [], "complete": False,
              "limitations": ["CLI wall time includes GNU time startup; initialization, explicit unlock, input creation and cleanup are excluded.",
                              "Warm passphrase session, independent disposable roots, alternating arm order; external host load uncontrolled.",
                              "Sizes above 1000 measure new import only; large repetitions and contention are exploratory when sampled once.",
                              "Profiles above 4096 values use a one-value bundle for child validation, preserving the existing redaction-pattern limit.",
                              "RSS includes waited children and is not allocated bytes, live secret memory, or proof of erasure.",
                              "Contention uses process overlap observation; deterministic paused-candidate coverage is in Go tests."]}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("x") as output:
        def save():
            output.seek(0)
            json.dump(result, output, indent=2)
            output.truncate()
            output.flush()
        save()
        for size in sizes:
            for repeat in range(args.large_repeats if size > 1000 else args.repeats):
                arms = [("baseline", baseline), ("candidate", candidate)]
                if repeat % 2:
                    arms.reverse()
                for arm, binary in arms:
                    row = {"arm": arm, **measure(binary, size, repeat)}
                    result["rows"].append(row)
                    save()
                    print(json.dumps({"arm": arm, "size": size, "repeat": repeat,
                                      "new_import_seconds": row["samples"]["new_import"]["wall_ns"] / 1e9}), flush=True)
        if args.contention_size:
            for arm, binary in [("baseline", baseline), ("candidate", candidate)]:
                result["contention"].append({"arm": arm, **contention(binary, args.contention_size)})
                save()
        result["complete"] = True
        save()
    for size in sizes:
        for arm in ["baseline", "candidate"]:
            values = [r["samples"]["new_import"]["wall_ns"] / 1e9 for r in result["rows"] if r["size"] == size and r["arm"] == arm]
            print(json.dumps({"arm": arm, "size": size, "n": len(values), "median_seconds": statistics.median(values), "range_seconds": [min(values), max(values)]}))


if __name__ == "__main__":
    main()
