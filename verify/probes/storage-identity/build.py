#!/usr/bin/env python3
"""Build isolated, instrumented source snapshots; never patch the checkout."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]
SOURCES = {
    "legacy": ("c14191b84475b583d9a480139157fb25ba71c713", "0.29.0-storage-trace"),
    "candidate": ("2432f9c75d5a90971b4453accc1a35353c50ee6c", "0.30.0-storage-trace"),
}


def replace_once(text, before, after):
    if text.count(before) != 1:
        raise ValueError("Source instrumentation anchor changed: " + before[:100])
    return text.replace(before, after, 1)


def instrument(root, mode, commit):
    private = root / "internal/platformfs"
    changed = []
    if mode == "legacy":
        target = private / "private_windows.go"
        text = target.read_text()
        text = replace_once(text, "func containedFile(r *Root, f *os.File) error {", """func containedFile(r *Root, f *os.File) (traceFailure error) {
    var traceBase, traceActual string
    defer func() {
        if traceFailure != nil { storageTraceContainment(r, f, f.Name(), traceBase, traceActual, traceFailure) }
    }()""")
        text = replace_once(text, "actual, err := handlePath(windows.Handle(f.Fd()))", "traceBase = base\n\tactual, err := handlePath(windows.Handle(f.Fd()))")
        anchor = 'if !strings.HasPrefix(actual, strings.TrimRight(base, `\\`)+`\\`) {'
        text = replace_once(text, anchor, "traceActual = actual\n\t" + anchor)
    else:
        target = private / "containment_windows.go"
        text = target.read_text()
        anchor = "func containedFile(r *Root, f *os.File, path string) (err error) {"
        text = replace_once(text, anchor, anchor + """
    defer func() {
        if err != nil { storageTraceContainment(r, f, path, "", "", err) }
    }()""")
        anchor = 'return fsError("verify_file_identity", path, "file_identity_mismatch", ErrUnsafe)\n\t\t}'
        text = replace_once(text, anchor, anchor + '\n\t\tstorageTraceVerified(r, f, path, h)')
    target.write_text(text)
    changed.append(str(target.relative_to(root)))
    target = private / "error.go"
    target.write_text(replace_once(target.read_text(),
        "return storageerr.Wrap(operation, storageResource(path), reason, cause)",
        "if cause != nil { storageTraceFailure(operation, path, reason, cause) }\n\treturn storageerr.Wrap(operation, storageResource(path), reason, cause)"))
    changed.append(str(target.relative_to(root)))
    for template, filename in [("trace_windows.go.in", "storage_trace_windows.go"), ("matrix_windows_test.go.in", "storage_trace_windows_test.go"), ("compatibility_windows_test.go.in", "storage_compatibility_windows_test.go")]:
        content = (HERE / template).read_text().replace("@SOURCE_COMMIT@", commit).replace("@MODE@", mode)
        call = "containedFile(root, file.File)" if mode == "legacy" else "containedFile(root, file.File, expected)"
        content = content.replace("@VERIFY_CALL@", call)
        (private / filename).write_text(content)
        changed.append("internal/platformfs/" + filename)
    subprocess.run(["gofmt", "-w", *changed], cwd=root, check=True)
    return {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in changed}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="New evidence folder; existing folders are never reused")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    manifest = {"format": "devtools-storage-trace-build-v1", "go_version": subprocess.check_output(["go", "version"], text=True).strip(), "artifacts": [], "templates": {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in HERE.glob("*.in")}}
    for mode, (commit, version) in SOURCES.items():
        with tempfile.TemporaryDirectory(prefix="devtools-trace-source-") as scratch:
            root = Path(scratch)
            archive = subprocess.check_output(["git", "archive", "--format=tar", commit], cwd=REPO)
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                tar.extractall(root, filter="data")
            hashes = instrument(root, mode, commit)
            env = dict(os.environ, GOOS="windows", GOARCH="amd64", CGO_ENABLED="0")
            for kind, command in [("cli", ["go", "build", "-buildvcs=false", "-trimpath", "-ldflags", f"-X main.version={version} -X main.commit={commit}"]), ("matrix", ["go", "test", "-c", "-buildvcs=false", "-trimpath"])]:
                name = f"{mode}-{kind}.exe"
                package = "./cmd/devtools" if kind == "cli" else "./internal/platformfs"
                subprocess.run([*command, "-o", str(output / name), package], cwd=root, env=env, check=True)
                manifest["artifacts"].append({"file": name, "source_commit": commit, "version": version if kind == "cli" else None, "mode": mode, "kind": kind, "sha256": hashlib.sha256((output / name).read_bytes()).hexdigest(), "instrumented_files": hashes})
    (output / "build.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(output / "build.json")


if __name__ == "__main__":
    main()
