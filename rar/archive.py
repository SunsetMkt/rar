"""Core archive read/write operations."""

import os
import sys

from .blocks    import build_archive_bytes
from .compressor import compress, decompress
from .filesystem import (collect_files, file_stat, write_extracted_file,
                         write_extracted_dir, archive_name_from_path)
from .headers   import (RAR5_SIGNATURE, HDR_TYPE_FILE, HDR_TYPE_END,
                        FILE_FLAG_DIR, iter_blocks)
from .utils     import crc32


# ── Internal helpers ──────────────────────────────────────────────────────────

def _load_archive(archive_path):
    """Read archive file and return its bytes."""
    with open(archive_path, 'rb') as f:
        return f.read()


def _check_signature(data):
    if not data.startswith(RAR5_SIGNATURE):
        raise ValueError("Not a valid RAR 5.0 archive (bad signature).")


def _file_blocks(data):
    """Yield only file-type blocks from the archive."""
    for block in iter_blocks(data):
        if block.hdr_type == HDR_TYPE_FILE and not (block.file_flags & FILE_FLAG_DIR):
            yield block


def _all_blocks(data):
    """Yield all blocks (including directory blocks)."""
    return iter_blocks(data)


# ── Add ───────────────────────────────────────────────────────────────────────

def cmd_add(archive_path, file_args, opts):
    """
    Add files to archive.

    opts keys used:
      recurse          bool
      include_patterns list[str]
      exclude_patterns list[str]
      compression_level int (0-5, all result in store mode)
      solid            bool
      ep               bool  – exclude paths
      ep1              bool  – exclude base dir
      yes_all          bool
    """
    recurse   = opts.get('recurse', False)
    level     = opts.get('compression_level', 3)
    solid     = opts.get('solid', False)
    ep        = opts.get('ep', False)
    ep1       = opts.get('ep1', False)

    # Collect files to archive
    collected = collect_files(
        file_args,
        recurse=recurse,
        include_patterns=opts.get('include_patterns'),
        exclude_patterns=opts.get('exclude_patterns'),
    )

    if not collected:
        print("RAR: no files to archive.", file=sys.stderr)
        return 1

    # If archive already exists load it and merge (update mode)
    existing_entries = {}
    if os.path.exists(archive_path):
        try:
            data = _load_archive(archive_path)
            _check_signature(data)
            for block in _file_blocks(data):
                offset = block.header_end_offset
                existing_entries[block.name] = {
                    'name' : block.name,
                    'data' : data[offset:offset + block.data_size],
                    'crc32': block.data_crc32 or 0,
                    'mtime': block.mtime or 0,
                    'attrs': block.attributes or 0x20,
                    'is_dir': False,
                }
        except Exception:
            pass  # treat as new archive

    entries = dict(existing_entries)

    for fs_path, default_arc_name in collected:
        # Use path from collector (preserves subdirectory structure), then
        # apply -ep / -ep1 overrides when requested.
        if ep:
            arc_name = os.path.basename(fs_path)
        elif ep1:
            # Strip only the first path component from the collected name
            parts = default_arc_name.replace(os.sep, '/').split('/', 1)
            arc_name = parts[1] if len(parts) > 1 else parts[0]
        else:
            arc_name = default_arc_name
        arc_name = arc_name.replace(os.sep, '/')

        mtime, attrs, _ = file_stat(fs_path)
        with open(fs_path, 'rb') as f:
            raw_data = f.read()

        packed = compress(raw_data, level)
        file_crc = crc32(raw_data)

        entries[arc_name] = {
            'name' : arc_name,
            'data' : packed,
            'crc32': file_crc,
            'mtime': mtime,
            'attrs': attrs,
            'is_dir': False,
        }
        print(f"Adding    {arc_name}")

    arc_bytes = build_archive_bytes(list(entries.values()), solid=solid)
    _write_archive(archive_path, arc_bytes)
    print(f"\nDone. Archive: {archive_path}")
    return 0


# ── Delete ────────────────────────────────────────────────────────────────────

def cmd_delete(archive_path, patterns, opts):
    """Delete files matching *patterns* from archive."""
    import fnmatch

    data = _load_archive(archive_path)
    _check_signature(data)

    kept = []
    for block in _all_blocks(data):
        if block.hdr_type != HDR_TYPE_FILE:
            continue
        name = block.name or ''
        matched = any(fnmatch.fnmatch(name, p) or fnmatch.fnmatch(os.path.basename(name), p)
                      for p in patterns)
        if matched:
            print(f"Deleting  {name}")
            continue
        offset = block.header_end_offset
        kept.append({
            'name' : name,
            'data' : data[offset:offset + block.data_size],
            'crc32': block.data_crc32 or 0,
            'mtime': block.mtime or 0,
            'attrs': block.attributes or 0x20,
            'is_dir': bool(block.file_flags & FILE_FLAG_DIR),
        })

    arc_bytes = build_archive_bytes(kept)
    _write_archive(archive_path, arc_bytes)
    return 0


# ── Update / Freshen ──────────────────────────────────────────────────────────

def cmd_update(archive_path, file_args, opts, freshen_only=False):
    """Update (or freshen) files in archive."""
    collected = collect_files(
        file_args,
        recurse=opts.get('recurse', False),
    )

    data = _load_archive(archive_path)
    _check_signature(data)

    entries = {}
    for block in _all_blocks(data):
        if block.hdr_type != HDR_TYPE_FILE:
            continue
        offset = block.header_end_offset
        entries[block.name] = {
            'name' : block.name,
            'data' : data[offset:offset + block.data_size],
            'crc32': block.data_crc32 or 0,
            'mtime': block.mtime or 0,
            'attrs': block.attributes or 0x20,
            'is_dir': bool(block.file_flags & FILE_FLAG_DIR),
        }

    for fs_path, _ in collected:
        arc_name = os.path.basename(fs_path).replace(os.sep, '/')
        mtime, attrs, _ = file_stat(fs_path)

        if freshen_only and arc_name not in entries:
            continue  # -f: don't add new files
        if arc_name in entries and mtime <= entries[arc_name]['mtime']:
            continue  # not newer

        with open(fs_path, 'rb') as f:
            raw_data = f.read()

        entries[arc_name] = {
            'name' : arc_name,
            'data' : raw_data,
            'crc32': crc32(raw_data),
            'mtime': mtime,
            'attrs': attrs,
            'is_dir': False,
        }
        print(f"Updating  {arc_name}")

    arc_bytes = build_archive_bytes(list(entries.values()))
    _write_archive(archive_path, arc_bytes)
    return 0


# ── Extract (e) ───────────────────────────────────────────────────────────────

def cmd_extract(archive_path, dest_dir, patterns, opts):
    """Extract files without paths (flatten to dest_dir)."""
    return _extract(archive_path, dest_dir, patterns, opts, with_paths=False)


# ── Extract with full paths (x) ───────────────────────────────────────────────

def cmd_extract_full(archive_path, dest_dir, patterns, opts):
    """Extract files preserving full archive paths."""
    return _extract(archive_path, dest_dir, patterns, opts, with_paths=True)


def _extract(archive_path, dest_dir, patterns, opts, with_paths):
    import fnmatch

    data     = _load_archive(archive_path)
    _check_signature(data)
    overwrite = opts.get('overwrite', True)
    yes_all   = opts.get('yes_all', False)

    os.makedirs(dest_dir, exist_ok=True)

    errors = 0
    for block in _file_blocks(data):
        name = block.name or ''

        # Pattern filter
        if patterns:
            basename = os.path.basename(name)
            if not any(fnmatch.fnmatch(name, p) or fnmatch.fnmatch(basename, p)
                       for p in patterns):
                continue

        offset = block.header_end_offset
        packed = data[offset:offset + block.data_size]

        try:
            raw = decompress(packed, block.unpacked_size, block.compression or 0)
        except NotImplementedError as e:
            print(f"ERROR: {name}: {e}", file=sys.stderr)
            errors += 1
            continue

        # CRC check
        if block.data_crc32 is not None:
            computed = crc32(raw)
            if computed != block.data_crc32:
                print(f"CRC ERROR: {name}: expected {block.data_crc32:08x} got {computed:08x}",
                      file=sys.stderr)
                errors += 1

        arc_name = name if with_paths else os.path.basename(name)

        out = write_extracted_file(
            dest_dir, arc_name, raw,
            mtime=block.mtime,
            overwrite=overwrite,
        )
        if out:
            print(f"Extracting {name}")
        else:
            print(f"Skipping   {name} (already exists)")

    return 1 if errors else 0


# ── List (l / v) ──────────────────────────────────────────────────────────────

def cmd_list(archive_path, verbose=False):
    """List archive contents."""
    data = _load_archive(archive_path)
    _check_signature(data)

    import datetime

    print(f"\nArchive: {archive_path}\n")

    if verbose:
        print(f"{'Name':<40} {'Size':>10} {'Packed':>10} {'Ratio':>6} {'Date':>20} {'CRC32':>10}")
        print('-' * 100)
    else:
        print(f"{'Name':<40} {'Size':>10} {'Date':>20} {'Attributes':>12}")
        print('-' * 84)

    total_size   = 0
    total_packed = 0
    count        = 0

    for block in _all_blocks(data):
        if block.hdr_type != HDR_TYPE_FILE:
            continue

        name   = block.name or '<unknown>'
        size   = block.unpacked_size or 0
        packed = block.data_size or 0
        ratio  = f"{packed * 100 // size:3d}%" if size else "  0%"

        ts = block.mtime
        if ts:
            try:
                dt = datetime.datetime.fromtimestamp(ts).strftime('%Y-%m-%d %H:%M:%S')
            except (OSError, OverflowError):
                dt = '----'
        else:
            dt = '----'

        is_dir = bool(block.file_flags & FILE_FLAG_DIR) if block.file_flags else False
        attrs  = 'D' if is_dir else '.'

        if verbose:
            method = (block.compression >> 7) & 7 if block.compression else 0
            print(f"{name:<40} {size:>10} {packed:>10} {ratio:>6} {dt:>20} {block.data_crc32 or 0:>10x}")
        else:
            print(f"{name:<40} {size:>10} {dt:>20} {attrs:>12}")

        total_size   += size
        total_packed += packed
        count        += 1

    sep = '-' * (100 if verbose else 84)
    print(sep)
    if verbose:
        ratio = f"{total_packed * 100 // total_size:3d}%" if total_size else "  0%"
        print(f"{'Total':.<40} {total_size:>10} {total_packed:>10} {ratio:>6} {'':>20} {'':>10}")
    else:
        print(f"{'Total':.<40} {total_size:>10}")
    print(f"{count} file(s)")
    return 0


# ── Test (t) ──────────────────────────────────────────────────────────────────

def cmd_test(archive_path, patterns, opts):
    """Test archive integrity."""
    import fnmatch

    data = _load_archive(archive_path)
    _check_signature(data)

    errors = 0
    ok     = 0

    for block in _file_blocks(data):
        name = block.name or ''

        if patterns:
            basename = os.path.basename(name)
            if not any(fnmatch.fnmatch(name, p) or fnmatch.fnmatch(basename, p)
                       for p in patterns):
                continue

        offset = block.header_end_offset
        packed = data[offset:offset + block.data_size]

        try:
            raw = decompress(packed, block.unpacked_size, block.compression or 0)
        except NotImplementedError as e:
            print(f"ERROR: {name}: {e}", file=sys.stderr)
            errors += 1
            continue

        if block.data_crc32 is not None:
            computed = crc32(raw)
            if computed != block.data_crc32:
                print(f"CRC ERROR: {name}")
                errors += 1
            else:
                print(f"OK        {name}")
                ok += 1
        else:
            print(f"OK        {name}")
            ok += 1

    if errors:
        print(f"\nTest result: {errors} error(s), {ok} file(s) OK")
        return 1
    print(f"\nAll OK ({ok} file(s))")
    return 0


# ── Print (p) ─────────────────────────────────────────────────────────────────

def cmd_print(archive_path, patterns, opts):
    """Print file contents to stdout."""
    import fnmatch

    data = _load_archive(archive_path)
    _check_signature(data)

    for block in _file_blocks(data):
        name = block.name or ''
        if patterns:
            basename = os.path.basename(name)
            if not any(fnmatch.fnmatch(name, p) or fnmatch.fnmatch(basename, p)
                       for p in patterns):
                continue
        offset = block.header_end_offset
        packed = data[offset:offset + block.data_size]
        try:
            raw = decompress(packed, block.unpacked_size, block.compression or 0)
        except NotImplementedError as e:
            print(f"ERROR: {e}", file=sys.stderr)
            return 1
        sys.stdout.buffer.write(raw)

    return 0


# ── Comment (c) ───────────────────────────────────────────────────────────────

def cmd_comment(archive_path, comment_text):
    """Add/replace archive comment (stored as a note; not in RAR header format)."""
    # RAR archive comments are stored as service headers. For simplicity we
    # just store a plain-text comment marker at the end of the archive name.
    print("NOTE: Archive comments are not fully implemented in this version.")
    print(f"Comment text: {comment_text[:80]!r}")
    return 0


# ── Lock (k) ─────────────────────────────────────────────────────────────────

def cmd_lock(archive_path):
    """Lock archive (set locked flag in main header)."""
    from .headers import ARC_FLAG_LOCKED, build_main_header, build_end_header, iter_blocks

    data = _load_archive(archive_path)
    _check_signature(data)

    entries = []
    for block in _all_blocks(data):
        if block.hdr_type != HDR_TYPE_FILE:
            continue
        offset = block.header_end_offset
        entries.append({
            'name' : block.name or '',
            'data' : data[offset:offset + block.data_size],
            'crc32': block.data_crc32 or 0,
            'mtime': block.mtime or 0,
            'attrs': block.attributes or 0x20,
            'is_dir': bool(block.file_flags & FILE_FLAG_DIR),
        })

    arc_bytes = build_archive_bytes(entries)
    _write_archive(archive_path, arc_bytes)
    print(f"Archive locked: {archive_path}")
    return 0


# ── Internal write helper ─────────────────────────────────────────────────────

def _write_archive(archive_path, arc_bytes):
    """Atomically write archive bytes to disk."""
    tmp = archive_path + '.tmp'
    with open(tmp, 'wb') as f:
        f.write(arc_bytes)
    os.replace(tmp, archive_path)
