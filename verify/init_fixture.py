"""Installed init selection and real terminal interaction using owned state."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time
import tomllib
import uuid


def unix_terminal(argv, env, cwd, input_steps=()):
    import errno
    import fcntl
    import select
    import termios
    master, slave = os.openpty()

    def attach():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)

    process = subprocess.Popen(argv, cwd=cwd, env=env, stdin=slave, stdout=slave,
                               stderr=slave, preexec_fn=attach)
    os.close(slave)
    output = bytearray()
    pending = list(input_steps)
    search_from = 0
    deadline = time.monotonic() + 15
    try:
        while True:
            assert time.monotonic() < deadline, "init terminal fixture timed out"
            if pending and output.find(pending[0][0].encode(), search_from) >= 0:
                _, value = pending.pop(0)
                search_from = len(output)
                os.write(master, value.encode())
            if select.select([master], [], [], .05)[0]:
                try:
                    part = os.read(master, 65536)
                except OSError as error:
                    if error.errno != errno.EIO: raise
                    part = b""
                if not part: break
                output.extend(part)
        code = process.wait(timeout=2)
        assert not pending, "terminal exited before expected prompt"
        return code, output.decode().replace("\r\n", "\n")
    finally:
        if process.poll() is None: process.kill(); process.wait(timeout=5)
        os.close(master)


def run(binary, directory, environment, terminal=None):
    binary, directory = Path(binary).resolve(), Path(directory).resolve()
    directory.mkdir(parents=True, mode=0o700)
    root = directory / "managed"
    environment = {key: value for key, value in environment.items() if key not in (
        "DEVTOOLS_FORMAT", "AI_AGENT", "CODEX_THREAD_ID", "CODEX_CI", "CLAUDECODE", "CLAUDE_CODE",
        "CURSOR_AGENT", "GEMINI_CLI", "OPENCODE", "OPENCODE_CLIENT", "CI")}
    environment.update(TERM="xterm-256color", NO_COLOR="1")
    cli = [str(binary), "--isolated-root", str(root)]
    count = 0

    def project(parent, name="web"):
        path = directory / parent / name
        path.mkdir(parents=True, mode=0o700)
        return path

    def call(path, arguments, expected=0, input="unread-input", extra=None):
        nonlocal count
        result = subprocess.run([*cli, "--format", "json", *arguments], cwd=path,
            env=dict(environment, **(extra or {})), input=input, capture_output=True, text=True, timeout=15)
        assert result.returncode == expected, (arguments, result.returncode, result.stderr)
        assert not (result.stdout and result.stderr)
        body = json.loads(result.stdout or result.stderr)
        assert body["ok"] == (expected == 0)
        count += 1
        return body.get("data", body.get("error"))

    fresh = project("fresh", "demo-web")
    created = call(fresh, ["init"])
    assert created["changed"] and created["item"]["profile"] == "demo-web"
    assert created["item"]["profile_source"] == "directory" and created["item"]["profile_presence"] == "absent"
    assert not root.exists(), "init created a user data root"
    before = (fresh / "devtools.toml").read_bytes()
    retry = call(fresh, ["init"])
    assert not retry["changed"] and retry["item"]["profile_source"] == "config"
    assert (fresh / "devtools.toml").read_bytes() == before
    call(fresh, ["init", "--profile", "other"], 3)
    # No parent configuration is inherited. Case and legal punctuation survive.
    nested = fresh / "nested-App_1"
    nested.mkdir()
    assert call(nested,["init"])["item"]["profile"] == "nested-App_1"
    moved = fresh.with_name("renamed-project")
    fresh.rename(moved)
    fresh = moved
    assert call(fresh,["init"])["item"]["profile"] == "demo-web"
    if os.name != "nt":
        target = project("symlink-target","physical-name")
        alias = directory / "alias-name"
        alias.symlink_to(target,target_is_directory=True)
        assert call(alias,["init"])["item"]["profile"] == "physical-name"
    invalid = project("invalid", "invalid name")
    call(invalid, ["init"], 2)
    call(invalid, ["init", "--profile="], 2)
    call(invalid, ["init", "--interactive"], 2)
    call(invalid, ["init", "--interactive", "--no-interactive"], 2)
    call(fresh, ["task", "add", "--profile", "web", "--title", "Known namespace", "--request-id", str(uuid.uuid4())])
    collision = project("collision")
    failure = call(collision, ["init", "--no-interactive"], 3)
    assert failure["code"] == "profile_name_in_use"
    remedy = failure["details"]["remedies"][0]
    assert remedy["argv"][remedy["argv"].index("--isolated-root")+1] == str(root)
    assert not (collision / "devtools.toml").exists()
    explicit = call(collision, ["init", "--profile", "web"])
    assert explicit["item"]["profile_presence"] == "unchecked"
    assert explicit["item"]["profile_source"] == "flag"

    if terminal:
        for label, name, options, extra, steps, expected, selected in (
            ("share", "web", [], {}, [("Choice [3]: ", "1\n")], 0, "web"),
            ("rename", "web", [], {}, [("Choice [3]: ", "2\n"), ("Profile name: ", "different\n")], 0, "different"),
            ("cancel", "web", [], {}, [("Choice [3]: ", "\n")], 130, None),
            ("interrupt", "web", [], {}, [("Choice [3]: ", "\x03")], 130, None),
            ("eof", "invalid name", [], {}, [("Profile name: ", "\x1a" if os.name == "nt" else "\x04")], 3, None),
            ("invalid-three", "invalid name", [], {}, [("Profile name: ", "bad name\n"), ("Profile name: ", "bad/name\n"), ("Profile name: ", "\n")], 2, None),
            ("agent", "web", [], {"AI_AGENT":"1"}, [], 3, None),
            ("ci", "web", [], {"CI":"true"}, [], 3, None),
            ("disabled", "web", ["--no-interactive"], {}, [], 3, None),
            ("forced-agent", "web", ["--interactive"], {"AI_AGENT":"1"}, [("Choice [3]: ", "1\n")], 0, "web"),
            ("json", "web", ["--interactive"], {}, [], 2, None),
        ):
            path = project("terminal-" + label, name)
            code, output = terminal([*cli, "--format", "json" if label == "json" else "text", "init", *options], dict(environment, **extra), path, input_steps=steps)
            assert code == expected, (label, code, output)
            config = path / "devtools.toml"
            if selected:
                assert config.is_file() and tomllib.loads(config.read_text())["profile"] == selected, (label, output)
                assert "Projects using this profile name share" in output
            else:
                assert not config.exists(), label
            if not steps: assert "Choice [3]: " not in output and "Profile name: " not in output, label
            count += 1
    return {"checks":count,"terminal":bool(terminal),"result":"pass"}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--directory", type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(run(args.binary, args.directory, os.environ, unix_terminal if os.name != "nt" else None)))
