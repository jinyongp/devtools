"""Read-only CLI capability bootstrap for consumers of old and new releases."""

import json
import os
from pathlib import Path
import shutil
import subprocess


_capabilities = {}


def json_environment(environment=None):
    """Override only the child preference; never mutate the caller's mapping."""
    return dict(os.environ if environment is None else environment, DEVTOOLS_FORMAT="json")


def json_argv(binary, arguments=(), *, environment=None, cwd=None):
    """Negotiate before an operation, never by retrying a failed mutation.

    A binary replacement invalidates the in-process capability cache. Private
    helper entrypoints keep their original wire and argv.
    """
    binary = os.fspath(binary)
    arguments = [str(arg) for arg in arguments]
    if arguments and arguments[0].startswith("__"):
        return [binary, *arguments]
    child_env = json_environment(environment)
    resolved = shutil.which(binary, path=child_env.get("PATH")) if not Path(binary).is_absolute() else binary
    if resolved is None:
        raise RuntimeError("Cannot locate CLI for JSON capability discovery.")
    path = Path(resolved).resolve()
    stat = path.stat()
    identity = (str(path), stat.st_dev, stat.st_ino, stat.st_size, stat.st_mtime_ns)
    if identity not in _capabilities:
        try:
            result = subprocess.run([binary, "schema", "version"], cwd=cwd, env=child_env,
                                    capture_output=True, text=True, timeout=10)
            response = json.loads(result.stdout)
            if (result.returncode or not isinstance(response, dict)
                    or response.get("ok") is not True or response.get("schema_version") != 1
                    or not isinstance(response.get("data"), dict)):
                raise ValueError("invalid bootstrap response")
            options = response["data"].get("global_options", [])
            if not isinstance(options, list):
                raise ValueError("invalid global options")
            supported = any(isinstance(option, dict) and option.get("name") == "format"
                            and option.get("placement") == "root-prefix"
                            and isinstance(option.get("values"), list) and "json" in option["values"]
                            for option in options)
        except (OSError, subprocess.TimeoutExpired, ValueError, KeyError, TypeError) as error:
            raise RuntimeError("Cannot discover the CLI JSON contract; operation was not started.") from error
        _capabilities[identity] = supported
    prefix = ["--format", "json"] if _capabilities[identity] else []
    return [binary, *prefix, *arguments]
