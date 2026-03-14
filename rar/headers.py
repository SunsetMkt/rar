"""RAR 5.0 header constants, structures and block builders."""

import struct
from .utils import encode_vint, decode_vint, crc32, uint32_le, read_uint32_le, read_uint64_le, filetime_to_unix

# ── Signature ─────────────────────────────────────────────────────────────────
RAR5_SIGNATURE = b'\x52\x61\x72\x21\x1a\x07\x01\x00'

# ── Header types ──────────────────────────────────────────────────────────────
HDR_TYPE_MAIN    = 1
HDR_TYPE_FILE    = 2
HDR_TYPE_SERVICE = 3
HDR_TYPE_ENCRYPT = 4
HDR_TYPE_END     = 5

# ── Common header flags ───────────────────────────────────────────────────────
HDR_FLAG_EXTRA_AREA        = 0x0001  # extra area present
HDR_FLAG_DATA_AREA         = 0x0002  # data area present
HDR_FLAG_SKIP_UNKNOWN      = 0x0004  # skip block on archive update if type unknown
HDR_FLAG_DATA_PREV         = 0x0008  # data area continues from previous volume
HDR_FLAG_DATA_NEXT         = 0x0010  # data area continues in next volume

# ── Archive (main) header flags ───────────────────────────────────────────────
ARC_FLAG_VOLUME     = 0x0001
ARC_FLAG_VOLNUM     = 0x0002
ARC_FLAG_SOLID      = 0x0004
ARC_FLAG_RECOVERY   = 0x0008
ARC_FLAG_LOCKED     = 0x0010

# ── File header flags ─────────────────────────────────────────────────────────
FILE_FLAG_DIR          = 0x0001
FILE_FLAG_UNIX_TIME    = 0x0002  # mtime stored as uint32 in header
FILE_FLAG_CRC32        = 0x0004  # data CRC32 stored in header
FILE_FLAG_UNKNOWN_SIZE = 0x0008

# ── End-of-archive flags ──────────────────────────────────────────────────────
END_FLAG_NOT_LAST = 0x0001

# ── File time extra record flags (type 0x03) ──────────────────────────────────
TIME_FLAG_UNIX   = 0x0001  # Unix time_t format (otherwise Windows FILETIME)
TIME_FLAG_MTIME  = 0x0002
TIME_FLAG_CTIME  = 0x0004
TIME_FLAG_ATIME  = 0x0008
TIME_FLAG_NSEC   = 0x0010  # nanosecond precision

# ── Host OS ───────────────────────────────────────────────────────────────────
HOST_OS_WINDOWS = 0
HOST_OS_UNIX    = 1


# ── Low-level block builder ───────────────────────────────────────────────────

def build_block(hdr_type, hdr_flags, type_specific, extra_area=b'', data_size=0):
    """
    Assemble a complete RAR block (CRC32 | hdr_size_vint | content).

    content = type_vint | flags_vint | [extra_size_vint] | [data_size_vint]
              | type_specific | extra_area

    CRC32 covers the hdr_size_vint and everything in content.
    The data area itself (file bytes) is appended by the caller.
    """
    body = bytearray()
    body += encode_vint(hdr_type)
    body += encode_vint(hdr_flags)

    if hdr_flags & HDR_FLAG_EXTRA_AREA:
        body += encode_vint(len(extra_area))
    if hdr_flags & HDR_FLAG_DATA_AREA:
        body += encode_vint(data_size)

    body += type_specific
    body += extra_area

    hdr_size_vint = encode_vint(len(body))
    crc_input = hdr_size_vint + bytes(body)
    hdr_crc = crc32(crc_input)

    return uint32_le(hdr_crc) + hdr_size_vint + bytes(body)


# ── Main archive header ───────────────────────────────────────────────────────

def build_main_header(arc_flags=0):
    """Build main archive header (type 1)."""
    type_specific = encode_vint(arc_flags)
    return build_block(HDR_TYPE_MAIN, HDR_FLAG_SKIP_UNKNOWN, type_specific)


# ── File header ───────────────────────────────────────────────────────────────

def build_file_header(name, unpacked_size, data_crc, mtime_unix, packed_size, attrs=0x20):
    """
    Build a file header (type 2) for a stored (uncompressed) file.

    Uses file_flags = FILE_FLAG_UNIX_TIME | FILE_FLAG_CRC32.
    Compression info = 0 (version 0, store, no solid, 128 KB dict).
    """
    file_flags = FILE_FLAG_UNIX_TIME | FILE_FLAG_CRC32  # 0x0006
    hdr_flags  = HDR_FLAG_DATA_AREA                     # 0x0002

    name_bytes = name.encode('utf-8')

    # compression: ver=0, solid=0, method=0 (store), dict=0 (128 KB)
    compression = 0

    type_specific = (
        encode_vint(file_flags) +
        encode_vint(unpacked_size) +
        encode_vint(attrs) +
        struct.pack('<I', mtime_unix) +   # present: FILE_FLAG_UNIX_TIME
        struct.pack('<I', data_crc) +     # present: FILE_FLAG_CRC32
        encode_vint(compression) +
        encode_vint(HOST_OS_UNIX) +
        encode_vint(len(name_bytes)) +
        name_bytes
    )

    return build_block(HDR_TYPE_FILE, hdr_flags, type_specific, data_size=packed_size)


# ── Directory header ──────────────────────────────────────────────────────────

def build_dir_header(name, mtime_unix, attrs=0x41ED):
    """Build a directory file header (type 2, FILE_FLAG_DIR set)."""
    file_flags = FILE_FLAG_DIR | FILE_FLAG_UNIX_TIME  # 0x0003
    hdr_flags  = 0                                    # no data area

    name_bytes = name.encode('utf-8')
    compression = 0

    type_specific = (
        encode_vint(file_flags) +
        encode_vint(0) +                              # unpacked_size = 0
        encode_vint(attrs) +
        struct.pack('<I', mtime_unix) +               # present: FILE_FLAG_UNIX_TIME
        encode_vint(compression) +
        encode_vint(HOST_OS_UNIX) +
        encode_vint(len(name_bytes)) +
        name_bytes
    )

    return build_block(HDR_TYPE_FILE, hdr_flags, type_specific)


# ── End of archive header ─────────────────────────────────────────────────────

def build_end_header(end_flags=0):
    """Build end-of-archive header (type 5)."""
    type_specific = encode_vint(end_flags)
    return build_block(HDR_TYPE_END, HDR_FLAG_SKIP_UNKNOWN, type_specific)


# ── Block reader ──────────────────────────────────────────────────────────────

class BlockInfo:
    """Parsed information from a RAR block header."""
    __slots__ = (
        'offset', 'crc32', 'hdr_size', 'hdr_type', 'hdr_flags',
        'extra_area_size', 'data_size', 'type_specific', 'extra_area',
        'header_end_offset', '_crc_ok',
        # file-header specific
        'file_flags', 'unpacked_size', 'attributes', 'mtime',
        'data_crc32', 'compression', 'host_os', 'name',
        # end-header specific
        'end_flags',
        # main-header specific
        'arc_flags',
    )

    def __init__(self):
        for s in self.__slots__:
            setattr(self, s, None)


def read_block(data, pos):
    """
    Parse one block from *data* starting at *pos*.
    Returns (BlockInfo, next_pos_after_header) where the data area
    (if any) begins at next_pos_after_header.
    """
    info = BlockInfo()
    info.offset = pos

    if pos + 4 > len(data):
        return None, pos

    info.crc32, pos = read_uint32_le(data, pos)

    hdr_size_vint_start = pos
    info.hdr_size, pos = decode_vint(data, pos)
    hdr_size_vint_end = pos

    content_start = pos
    content_end   = content_start + info.hdr_size

    if content_end > len(data):
        return None, info.offset

    # Verify CRC
    crc_input = data[hdr_size_vint_start:hdr_size_vint_end] + data[content_start:content_end]
    computed = crc32(crc_input)
    # We continue even on CRC mismatch (test command reports the error)
    info._crc_ok = (computed == info.crc32)

    info.hdr_type,  pos = decode_vint(data, pos)
    info.hdr_flags, pos = decode_vint(data, pos)

    info.extra_area_size = 0
    info.data_size       = 0

    if info.hdr_flags & HDR_FLAG_EXTRA_AREA:
        info.extra_area_size, pos = decode_vint(data, pos)
    if info.hdr_flags & HDR_FLAG_DATA_AREA:
        info.data_size, pos = decode_vint(data, pos)

    # ── Type-specific parsing ──────────────────────────────────────────────
    if info.hdr_type == HDR_TYPE_MAIN:
        info.arc_flags, pos = decode_vint(data, pos)
        if info.arc_flags & ARC_FLAG_VOLNUM:
            _, pos = decode_vint(data, pos)

    elif info.hdr_type in (HDR_TYPE_FILE, HDR_TYPE_SERVICE):
        info.file_flags,   pos = decode_vint(data, pos)
        info.unpacked_size, pos = decode_vint(data, pos)
        info.attributes,   pos = decode_vint(data, pos)

        if info.file_flags & FILE_FLAG_UNIX_TIME:
            info.mtime, pos = read_uint32_le(data, pos)
        if info.file_flags & FILE_FLAG_CRC32:
            info.data_crc32, pos = read_uint32_le(data, pos)

        info.compression, pos = decode_vint(data, pos)
        info.host_os,     pos = decode_vint(data, pos)
        name_len,         pos = decode_vint(data, pos)
        info.name = data[pos:pos + name_len].decode('utf-8', errors='replace')
        pos += name_len

    elif info.hdr_type == HDR_TYPE_END:
        info.end_flags, pos = decode_vint(data, pos)

    # ── Extra area ────────────────────────────────────────────────────────
    if info.hdr_flags & HDR_FLAG_EXTRA_AREA:
        extra_start = content_end - info.extra_area_size
        info.extra_area = data[extra_start:content_end]
        # Parse mtime from extra area if not already in header
        if info.hdr_type in (HDR_TYPE_FILE, HDR_TYPE_SERVICE) and info.mtime is None:
            _parse_file_time_extra(info)

    info.header_end_offset = content_end
    return info, content_end


def _parse_file_time_extra(info):
    """Try to extract mtime from file-time extra record (type 0x03)."""
    if not info.extra_area:
        return
    ea = info.extra_area
    epos = 0
    while epos < len(ea):
        try:
            rec_size, epos = decode_vint(ea, epos)
        except ValueError:
            break
        rec_start = epos
        if epos >= len(ea):
            break
        rec_type, epos = decode_vint(ea, epos)
        if rec_type == 0x03:  # File time record
            flags, epos = decode_vint(ea, epos)
            if flags & TIME_FLAG_MTIME:
                if flags & TIME_FLAG_UNIX:
                    if flags & TIME_FLAG_NSEC:
                        ts = struct.unpack_from('<Q', ea, epos)[0]
                        info.mtime = ts // 1000000000
                    else:
                        info.mtime = struct.unpack_from('<I', ea, epos)[0]
                else:
                    # Windows FILETIME
                    ft = struct.unpack_from('<Q', ea, epos)[0]
                    info.mtime = filetime_to_unix(ft)
            break
        epos = rec_start + rec_size  # skip unknown record


def iter_blocks(data, start=8):
    """Yield BlockInfo objects for each block, starting after the signature."""
    pos = start
    while pos < len(data):
        info, pos = read_block(data, pos)
        if info is None:
            break
        yield info
        pos += info.data_size  # skip data area
        if info.hdr_type == HDR_TYPE_END:
            break
