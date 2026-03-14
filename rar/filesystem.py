"""File-system helpers: gathering files, applying path rules, writing extracted files."""

import fnmatch
import os
import stat


def collect_files(paths, recurse=False, include_patterns=None, exclude_patterns=None):
    """
    Expand *paths* into a list of (fs_path, archive_name) tuples.

    Parameters
    ----------
    paths            : list of str – files, directories, or glob masks
    recurse          : bool        – descend into directories
    include_patterns : list of str – only include files matching one pattern
    exclude_patterns : list of str – exclude files matching any pattern

    Returns
    -------
    list of (str, str) – (filesystem_path, archive_name)
    """
    results = []
    for path in paths:
        path = os.path.normpath(path)
        if os.path.isdir(path):
            _collect_dir(path, path, recurse, results)
        elif os.path.isfile(path):
            results.append((path, os.path.basename(path)))
        else:
            # Wildcard / glob-like – handled by caller or shell
            if os.path.isfile(path):
                results.append((path, os.path.basename(path)))

    # Apply include/exclude filters on the archive name part
    if include_patterns:
        results = [
            (fp, an) for fp, an in results
            if any(fnmatch.fnmatch(an, p) for p in include_patterns)
        ]
    if exclude_patterns:
        results = [
            (fp, an) for fp, an in results
            if not any(fnmatch.fnmatch(an, p) for p in exclude_patterns)
        ]

    return results


def _collect_dir(base_dir, current_dir, recurse, results):
    """Recursively (or not) collect files from *current_dir*."""
    try:
        entries = sorted(os.listdir(current_dir))
    except PermissionError:
        return

    for entry in entries:
        full = os.path.join(current_dir, entry)
        rel  = os.path.relpath(full, os.path.dirname(base_dir))
        rel  = rel.replace(os.sep, '/')

        if os.path.isdir(full):
            if recurse:
                _collect_dir(base_dir, full, recurse, results)
        elif os.path.isfile(full):
            results.append((full, rel))


def archive_name_from_path(fs_path, base_dir=None, ep=False, ep1=False):
    """
    Derive the in-archive name from a filesystem path.

    Parameters
    ----------
    fs_path  : str  – real filesystem path
    base_dir : str  – base directory for relative names
    ep       : bool – -ep: exclude all paths, use basename only
    ep1      : bool – -ep1: exclude base directory from path
    """
    if ep:
        return os.path.basename(fs_path)

    if ep1 and base_dir:
        rel = os.path.relpath(fs_path, base_dir)
        return rel.replace(os.sep, '/')

    return os.path.basename(fs_path)


def write_extracted_file(dest_dir, archive_name, data, mtime=None,
                         overwrite=True, create_path=True):
    """
    Write *data* to *dest_dir*/*archive_name*.

    Parameters
    ----------
    dest_dir     : str   – destination directory
    archive_name : str   – path within the archive (may contain '/')
    data         : bytes – file contents
    mtime        : int   – Unix timestamp for file mtime (optional)
    overwrite    : bool  – overwrite existing files
    create_path  : bool  – create parent directories

    Returns
    -------
    str – full path of extracted file, or None if skipped
    """
    # Cosmetic: strip leading slashes / drive letters.
    # Real path-traversal protection is the realpath check below.
    safe_name = archive_name.lstrip('/')
    out_path = os.path.normpath(os.path.join(dest_dir, safe_name))

    # Ensure the path is actually inside dest_dir
    dest_real = os.path.realpath(dest_dir)
    out_real  = os.path.realpath(os.path.join(dest_dir, safe_name))
    if not out_real.startswith(dest_real + os.sep) and out_real != dest_real:
        raise ValueError(f"Path traversal detected: {archive_name!r}")

    if os.path.exists(out_path) and not overwrite:
        return None

    if create_path:
        os.makedirs(os.path.dirname(out_path), exist_ok=True)

    with open(out_path, 'wb') as f:
        f.write(data)

    if mtime is not None:
        try:
            os.utime(out_path, (mtime, mtime))
        except OSError:
            pass

    return out_path


def write_extracted_dir(dest_dir, archive_name, mtime=None):
    """Create a directory entry during extraction."""
    safe_name = archive_name.lstrip('/').replace('..', '_')
    out_path  = os.path.normpath(os.path.join(dest_dir, safe_name))
    os.makedirs(out_path, exist_ok=True)
    if mtime is not None:
        try:
            os.utime(out_path, (mtime, mtime))
        except OSError:
            pass
    return out_path


def file_stat(path):
    """Return (mtime_unix, attrs, size) for a file."""
    s = os.stat(path)
    mtime = int(s.st_mtime)
    # Use lower 16 bits of st_mode as attributes
    attrs = s.st_mode & 0xFFFF
    return mtime, attrs, s.st_size
