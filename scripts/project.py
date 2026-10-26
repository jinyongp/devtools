#!/usr/bin/env python3
"""Portable developer commands. The installed CLI does not require Python."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from package import configure_output, package, package_skill, verify_skill


def run(args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def native_binary(name):
    return Path("bin") / (name + (".exe" if os.name == "nt" else ""))


def build(name, source, extra=()):
    Path("bin").mkdir(exist_ok=True)
    run(["go", "build", "-o", native_binary(name), source, *extra])


def node_tests(pattern, extra=()):
    files = sorted(Path("scripts").glob(pattern))
    if not files:
        raise ValueError("No Node tests match " + pattern)
    run(["node", "--test", *files, *extra])


def check_skill_release():
    run(["uvx", "--from", "skills-ref==0.1.1", "agentskills", "validate", "skills/devtools"])
    with tempfile.TemporaryDirectory(prefix="devtools-skill-check-") as scratch:
        first = package_skill("0.0.0-test", dict(os.environ, OUTPUT_DIR=str(Path(scratch) / "a")))
        second = package_skill("0.0.0-test", dict(os.environ, OUTPUT_DIR=str(Path(scratch) / "b"), TZ="Asia/Seoul"))
        if first.read_bytes() != second.read_bytes() or first.with_name(first.name + ".sha256").read_bytes() != second.with_name(second.name + ".sha256").read_bytes():
            raise ValueError("Repeated skill packages are not reproducible")
        # Preserve official validation's negative fixture without mutating source.
        invalid = Path(scratch) / "invalid" / "devtools"
        shutil.copytree("skills/devtools", invalid)
        skill = invalid / "SKILL.md"
        skill.write_text("\n".join(line for line in skill.read_text(encoding="utf-8").splitlines() if not line.startswith("description:")), encoding="utf-8")
        rejected = subprocess.run(["uvx", "--from", "skills-ref==0.1.1", "agentskills", "validate", str(invalid)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if rejected.returncode == 0:
            raise ValueError("Official validation accepted a missing description")
    run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "scripts", "-p", "test_package.py"])


GO_TEST_LANES = ("cli", "values", "vault", "handoff", "rest")
WINDOWS_GO_TEST_LANES = ("suite-cli", "suite-values", "suite-tasks", "suite-handoff", "suite-runtime", "suite-rest")


def go_test_lane(packages, lane):
    if lane not in GO_TEST_LANES:
        raise ValueError("Go test lane must be one of: " + ", ".join(GO_TEST_LANES))
    # Handoff runs on all four native Unix architectures; do not repeat those
    # packages in rest. Newly added packages still enter rest automatically.
    groups = {"cli": ("cli",), "values": ("values",), "vault": ("vault",),
              "handoff": ("valuehandoff", "runnerengine")}
    dedicated = {"github.com/jinyongp/devtools/internal/" + name: group
                 for group, names in groups.items() for name in names}
    selected = [package for package in packages if dedicated.get(package, "rest") == lane]
    if not selected:
        raise ValueError("Empty Go test lane: " + lane)
    return selected


def windows_go_test_lane(packages, lane):
    if lane not in WINDOWS_GO_TEST_LANES:
        raise ValueError("Windows Go test lane must be one of: " + ", ".join(WINDOWS_GO_TEST_LANES))
    groups = {
        "suite-cli": ("cli",),
        "suite-values": ("values",),
        "suite-tasks": ("tasks",),
        "suite-handoff": ("valuehandoff", "runnerengine"),
        "suite-runtime": ("expose", "lifecycle", "process", "proxy", "services"),
    }
    dedicated = {"github.com/jinyongp/devtools/internal/" + name: group
                 for group, names in groups.items() for name in names}
    selected = [package for package in packages if dedicated.get(package, "suite-rest") == lane]
    if not selected:
        raise ValueError("Empty Windows Go test lane: " + lane)
    return selected


def check_static():
    runner = native_binary("devtools")
    runner = str(runner.resolve()) if runner.is_file() and (os.name == "nt" or os.access(runner, os.X_OK)) else "devtools"
    for gate in ("skill-release", "dashboard", "installer", "release", "project", "perf"):
        run([runner, "run", "check:" + gate])
    formatted = subprocess.check_output(["gofmt", "-l", "cmd", "internal", "scripts", "skills", "perf", "verify"], text=True)
    if formatted.strip():
        raise ValueError("Run gofmt on:\n" + formatted)
    run(["go", "vet", "./..."])


def main(args):
    configure_output()
    if not args:
        raise ValueError("Project action required")
    action, extra = args[0], args[1:]
    if action == "python":
        run([sys.executable, *extra])
    elif action == "build-cli":
        build("devtools", "./cmd/devtools", extra)
    elif action == "dashboard":
        build("dashboard", "./scripts/dashboard")
        run([native_binary("dashboard").resolve(), *extra])
    elif action == "clean":
        if extra:
            raise ValueError("clean accepts no arguments")
        for path in (Path("bin"), Path("coverage.out")):
            if path.is_symlink() or path.is_file():
                path.unlink()
            elif path.exists():
                shutil.rmtree(path)
    elif action == "dashboard-check":
        node_tests("dashboard-*.test.mjs", extra)
    elif action == "proxy-compat":
        run([sys.executable, "-B", "verify/proxy_compat.py", *extra])
    elif action == "project-check":
        node_tests("project.test.mjs", extra)
        run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "scripts", "-p", "test_project.py"])
        run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "scripts", "-p", "test_fixture_paths.py"])
        run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "scripts", "-p", "test_cli_json.py"])
        run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "verify", "-p", "test_proxy_compat.py"])
    elif action == "check-static":
        if extra:
            raise ValueError("check-static accepts no arguments")
        check_static()
    elif action == "test-go-lane":
        if len(extra) != 1 or extra[0] not in GO_TEST_LANES:
            raise ValueError("test-go-lane requires one of: " + ", ".join(GO_TEST_LANES))
        packages = subprocess.check_output(["go", "list", "./..."], text=True).splitlines()
        # Native handoff jobs require fresh process/transport evidence, even when
        # setup-go restores a successful test cache from an earlier run.
        fresh = ["-count=1", "-timeout", "8m"] if extra[0] == "handoff" else []
        run(["go", "test", *([] if os.name == "nt" else ["-race"]), *fresh,
             *go_test_lane(packages, extra[0])])
    elif action == "windows-go-packages":
        if len(extra) != 1 or extra[0] not in WINDOWS_GO_TEST_LANES:
            raise ValueError("windows-go-packages requires one of: " + ", ".join(WINDOWS_GO_TEST_LANES))
        packages = subprocess.check_output(["go", "list", "./..."], text=True).splitlines()
        print("\n".join(windows_go_test_lane(packages, extra[0])))
    elif action == "check":
        check_static()
        # Native ARM64 Windows has no Go race detector. Unix keeps the existing
        # race gate; native product integration is separately required in CI.
        run(["go", "test", *([] if os.name == "nt" else ["-race"]), "./...", *extra])
    elif action == "release-check":
        if os.name != "nt":
            run(["sh", "-n", "scripts/project.sh"])
            run(["sh", "-n", "scripts/publish-release-tag.sh"])
        node_tests("release.test.mjs")
        run([sys.executable, "-B", "scripts/reuse-windows-validation.test.py"])
        run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "verify", "-p", "test_run.py", *extra])
    elif action == "installer-check":
        if os.name == "nt":
            run(["powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "scripts/check-installer.ps1", *extra])
        else:
            run(["sh", "-n", "scripts/install.sh"])
            run(["sh", "scripts/check-installer.sh"])
            run([sys.executable, "-B", "verify/scenarios/bootstrap.py", *extra], env=dict(os.environ, DEVTOOLS_TEST_README="README.md", DEVTOOLS_TEST_INSTALL_DOC="docs/install.md"))
    elif action == "skill-check":
        run(["uvx", "--from", "skills-ref==0.1.1", "agentskills", "validate", "skills/devtools", *extra])
    elif action == "skill-release-check":
        if extra:
            raise ValueError("skill-release-check accepts no arguments")
        check_skill_release()
    elif action in ("package-cli", "package-skill"):
        if len(extra) != 1:
            raise ValueError("Usage: project.py " + action + " VERSION")
        print((package if action == "package-cli" else package_skill)(extra[0]))
    elif action == "verify-skill":
        if len(extra) != 2:
            raise ValueError("verify-skill requires ARCHIVE CHECKSUM")
        verify_skill(*extra)
    else:
        raise ValueError("Unknown project action: " + action)


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except ValueError as error:
        print(error, file=sys.stderr)
        sys.exit(2)
    except subprocess.CalledProcessError as error:
        sys.exit(error.returncode)
    except OSError as error:
        print(error, file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        sys.exit(130)
