"""Archive-level operations: add, list, extract, test, delete."""

import fnmatch
import os
import struct
import subprocess

from .crc import crc32
from .headers import (
    RAR5_SIGNATURE, BLOCK_FILE, BLOCK_END,
    FHFL_UNIX_TIME, FHFL_CRC32,
)
from .blocks import (
    write_main_block, write_file_block, write_eof_block, read_block,
)
from .utils import decode_vint

_DEFAULT_UNRAR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    'rarbinary', 'unrar',
)
UNRAR_BINARY = os.environ.get('UNRAR_BINARY', _DEFAULT_UNRAR)

# Validate the unrar binary on module load so misconfiguration is caught early.
def _validate_unrar_binary(path: str) -> str:
    real = os.path.realpath(path)
    if not os.path.isfile(real):
        raise FileNotFoundError(f"unrar binary not found: {real}")
    if not os.access(real, os.X_OK):
        raise PermissionError(f"unrar binary is not executable: {real}")
    return real

try:
    UNRAR_BINARY = _validate_unrar_binary(UNRAR_BINARY)
except (FileNotFoundError, PermissionError):
    # Allow import to succeed even if unrar is absent; extraction will fail gracefully.
    UNRAR_BINARY = os.path.realpath(UNRAR_BINARY)


class Archive:
    # ------------------------------------------------------------------
    # Add
    # ------------------------------------------------------------------

    def add(self, archive_path: str, file_paths, method: int = 0,
            solid: bool = False, recurse: bool = False,
            base_dir: str = None):
        """Create or recreate *archive_path* containing *file_paths*."""
        output = bytearray()
        output += RAR5_SIGNATURE
        output += write_main_block()

        files_to_add = []
        for fp in file_paths:
            if os.path.isdir(fp) and recurse:
                for root, _dirs, files in os.walk(fp):
                    for f in files:
                        files_to_add.append(os.path.join(root, f))
            else:
                files_to_add.append(fp)

        for fp in files_to_add:
            if not os.path.isfile(fp):
                print(f"Warning: {fp} not found, skipping")
                continue

            arc_name = (
                os.path.relpath(fp, base_dir) if base_dir else os.path.basename(fp)
            )
            arc_name = arc_name.replace('\\', '/')

            with open(fp, 'rb') as fh:
                file_data = fh.read()

            mtime = int(os.path.getmtime(fp))
            output += write_file_block(arc_name, file_data, method=method, mtime=mtime)

        output += write_eof_block()

        with open(archive_path, 'wb') as fh:
            fh.write(output)

    # ------------------------------------------------------------------
    # List
    # ------------------------------------------------------------------

    def list_contents(self, archive_path: str):
        """Return a list of dicts describing each stored file."""
        data = _read_archive(archive_path)
        entries = []
        offset = len(RAR5_SIGNATURE)

        while offset < len(data):
            result = read_block(data, offset)
            if result is None:
                break
            block_type, header_flags, block_specific, block_data, new_offset = result

            if block_type == BLOCK_FILE:
                entries.append(parse_file_block(block_specific, block_data))
            elif block_type == BLOCK_END:
                break

            offset = new_offset

        return entries

    # ------------------------------------------------------------------
    # Extract
    # ------------------------------------------------------------------

    def extract(self, archive_path: str, dest_path: str,
                include_paths: bool = True, files=None):
        """Extract files from *archive_path* to *dest_path*."""
        data = _read_archive(archive_path)
        os.makedirs(dest_path, exist_ok=True)
        offset = len(RAR5_SIGNATURE)

        # Collect which files need unrar (compressed)
        needs_unrar = False

        while offset < len(data):
            result = read_block(data, offset)
            if result is None:
                break
            block_type, header_flags, block_specific, block_data, new_offset = result

            if block_type == BLOCK_FILE:
                info = parse_file_block(block_specific, block_data)
                name = info['name']

                if files and not any(
                    name == f or name.endswith('/' + f) for f in files
                ):
                    offset = new_offset
                    continue

                if info['method'] == 0:
                    # Store — extract directly
                    out_path = _build_output_path(dest_path, name, include_paths)
                    os.makedirs(os.path.dirname(out_path), exist_ok=True)
                    with open(out_path, 'wb') as fh:
                        fh.write(block_data)
                    if 'crc32' in info and crc32(block_data) != info['crc32']:
                        print(f"Warning: CRC mismatch for {name}")
                else:
                    needs_unrar = True

            elif block_type == BLOCK_END:
                break

            offset = new_offset

        if needs_unrar:
            _extract_with_unrar(archive_path, dest_path, include_paths)

    # ------------------------------------------------------------------
    # Test
    # ------------------------------------------------------------------

    def test(self, archive_path: str) -> bool:
        """Verify CRC32 for all store-mode files. Returns True if all OK."""
        data = _read_archive(archive_path)
        offset = len(RAR5_SIGNATURE)
        ok = True

        while offset < len(data):
            result = read_block(data, offset)
            if result is None:
                break
            block_type, _hflags, block_specific, block_data, new_offset = result

            if block_type == BLOCK_FILE:
                info = parse_file_block(block_specific, block_data)
                if info['method'] == 0 and 'crc32' in info:
                    if crc32(block_data) == info['crc32']:
                        print(f"OK: {info['name']}")
                    else:
                        print(f"CRC error: {info['name']}")
                        ok = False
            elif block_type == BLOCK_END:
                break

            offset = new_offset

        return ok

    # ------------------------------------------------------------------
    # Delete
    # ------------------------------------------------------------------

    def delete(self, archive_path: str, patterns):
        """Remove files matching *patterns* (wildcards OK) from the archive."""
        data = _read_archive(archive_path)
        new_output = bytearray()
        new_output += RAR5_SIGNATURE
        new_output += write_main_block()

        offset = len(RAR5_SIGNATURE)

        while offset < len(data):
            result = read_block(data, offset)
            if result is None:
                break
            block_type, _hflags, block_specific, block_data, new_offset = result

            if block_type == BLOCK_FILE:
                info = parse_file_block(block_specific, block_data)
                name = info['name']
                basename = os.path.basename(name)
                skip = any(
                    fnmatch.fnmatch(basename, p) or fnmatch.fnmatch(name, p)
                    for p in patterns
                )
                if not skip:
                    new_output += data[offset:new_offset]
            elif block_type == BLOCK_END:
                break
            else:
                new_output += data[offset:new_offset]

            offset = new_offset

        new_output += write_eof_block()

        with open(archive_path, 'wb') as fh:
            fh.write(new_output)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _read_archive(path: str) -> bytes:
    with open(path, 'rb') as fh:
        data = fh.read()
    if not data.startswith(RAR5_SIGNATURE):
        raise ValueError(f"{path!r} is not a RAR5 archive")
    return data


def _build_output_path(dest_path: str, name: str, include_paths: bool) -> str:
    if include_paths:
        rel = name.replace('/', os.sep)
    else:
        rel = os.path.basename(name)
    full = os.path.join(dest_path, rel)
    parent = os.path.dirname(full)
    if parent:
        os.makedirs(parent, exist_ok=True)
    return full


def _extract_with_unrar(archive_path: str, dest_path: str,
                        include_paths: bool = True):
    """Delegate extraction of compressed files to the unrar binary."""
    # Resolve to absolute paths to prevent any relative-path traversal surprises
    archive_path = os.path.realpath(archive_path)
    dest_path = os.path.realpath(dest_path)

    if not os.path.isfile(archive_path):
        raise FileNotFoundError(f"Archive not found: {archive_path}")

    cmd_char = 'x' if include_paths else 'e'
    cmd = [UNRAR_BINARY, cmd_char, archive_path, dest_path + os.sep, '-y']
    result = subprocess.run(cmd, capture_output=True)
    if result.returncode != 0:
        stderr = result.stderr.decode(errors='replace').strip()
        stdout = result.stdout.decode(errors='replace').strip()
        print(f"Warning: unrar exited with code {result.returncode}")
        if stderr:
            print(f"  stderr: {stderr}")
        if stdout:
            print(f"  stdout: {stdout}")


def parse_file_block(block_specific: bytes, block_data: bytes) -> dict:
    """Parse file-specific header fields from a BLOCK_FILE block."""
    pos = 0
    data = block_specific

    file_flags, pos = decode_vint(data, pos)
    unpacked_size, pos = decode_vint(data, pos)
    attributes, pos = decode_vint(data, pos)

    mtime = 0
    if file_flags & FHFL_UNIX_TIME:
        mtime = struct.unpack_from('<I', data, pos)[0]
        pos += 4

    file_crc = None
    if file_flags & FHFL_CRC32:
        file_crc = struct.unpack_from('<I', data, pos)[0]
        pos += 4

    comp_info, pos = decode_vint(data, pos)
    host_os, pos = decode_vint(data, pos)
    name_len, pos = decode_vint(data, pos)
    name = data[pos: pos + name_len].decode('utf-8')

    method = (comp_info >> 7) & 0x7

    result = {
        'name': name,
        'unpacked_size': unpacked_size,
        'packed_size': len(block_data),
        'method': method,
        'mtime': mtime,
        'attributes': attributes,
        'comp_info': comp_info,
    }
    if file_crc is not None:
        result['crc32'] = file_crc

    return result
