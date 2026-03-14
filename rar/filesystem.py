"""Filesystem helpers: directory walking and archive name normalisation."""

import glob as _glob
import os


def walk_files(paths, recurse: bool = False):
    """Expand *paths* to a flat list of file paths.

    Directories are walked when *recurse* is True.  Glob patterns are
    expanded automatically.
    """
    result = []
    for path in paths:
        if os.path.isfile(path):
            result.append(path)
        elif os.path.isdir(path) and recurse:
            for root, _dirs, files in os.walk(path):
                for f in files:
                    result.append(os.path.join(root, f))
        else:
            matches = _glob.glob(path, recursive=recurse)
            result.extend(m for m in matches if os.path.isfile(m))
    return result


def normalize_archive_name(path: str, base_dir: str = None) -> str:
    """Return the name to store in the archive for *path*."""
    if base_dir:
        name = os.path.relpath(path, base_dir)
    else:
        name = os.path.basename(path)
    return name.replace('\\', '/')
