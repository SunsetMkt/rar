"""Block-building utilities for constructing RAR archives."""

from .headers import (
    RAR5_SIGNATURE,
    build_main_header,
    build_file_header,
    build_dir_header,
    build_end_header,
    ARC_FLAG_SOLID,
)


def build_archive_bytes(entries, solid=False):
    """
    Build a complete RAR 5.0 archive as bytes.

    Parameters
    ----------
    entries : list of dict
        Each dict must have:
          'name'  : str   – archive path (forward slashes)
          'data'  : bytes – raw file data  (empty for directories)
          'mtime' : int   – Unix timestamp
          'attrs' : int   – file system attributes
          'is_dir': bool  – True for directory entries
    solid : bool
        If True set the solid flag in the main archive header.

    Returns
    -------
    bytes
    """
    arc_flags = ARC_FLAG_SOLID if solid else 0
    parts = [RAR5_SIGNATURE, build_main_header(arc_flags)]

    for entry in entries:
        if entry.get('is_dir'):
            parts.append(build_dir_header(
                entry['name'],
                entry['mtime'],
                entry.get('attrs', 0x41ED),
            ))
        else:
            data = entry['data']
            parts.append(build_file_header(
                entry['name'],
                len(data),
                entry['crc32'],
                entry['mtime'],
                len(data),
                entry.get('attrs', 0x20),
            ))
            parts.append(data)

    parts.append(build_end_header())
    return b''.join(parts)
