"""Path selection for owned test fixtures, never product path validation.

New fixture roots use physical paths on every platform. An already selected
Windows session root keeps its spelling because that string is endpoint identity.
"""
from contextlib import contextmanager
import os
from pathlib import Path
import tempfile


def fixture_path(path):
    """Resolve existing aliases before appending even not-yet-created children."""
    return Path(path).resolve()


def session_path(path, *, system=None):
    """Preserve the existing Windows session contract; new roots use fixture_path."""
    if (system or os.name) == "nt":
        return Path(os.path.abspath(path))
    return fixture_path(path)


def create_fixture_directory(*, prefix, directory=None):
    """Allocate a canonical root whose cleanup/retention belongs to the caller."""
    return fixture_path(tempfile.mkdtemp(prefix=prefix, dir=directory))


@contextmanager
def temporary_fixture(*, prefix, directory=None):
    """Select the physical root once, before subprocess environment construction."""
    with tempfile.TemporaryDirectory(prefix=prefix, dir=directory) as root:
        yield fixture_path(root)
