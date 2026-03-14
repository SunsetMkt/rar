import struct
from .crc import crc32
from .utils import encode_vint, decode_vint
from .headers import (
    BLOCK_MAIN, BLOCK_FILE, BLOCK_END,
    HFL_EXTRA, HFL_DATA,
    FHFL_UNIX_TIME, FHFL_CRC32,
)


def write_block(block_type: int, header_flags: int, block_specific: bytes,
                data: bytes = b'') -> bytes:
    """Assemble a complete RAR5 block (header + optional data area)."""
    hdr_data = encode_vint(block_type) + encode_vint(header_flags)
    if header_flags & HFL_DATA:
        hdr_data += encode_vint(len(data))
    hdr_data += block_specific

    hdr_size = len(hdr_data)
    crc_input = encode_vint(hdr_size) + hdr_data
    block_crc = crc32(crc_input)

    result = struct.pack('<I', block_crc)
    result += encode_vint(hdr_size)
    result += hdr_data
    result += data
    return result


def write_main_block(archive_flags: int = 0) -> bytes:
    """Write the RAR5 archive header block (type 1)."""
    block_specific = encode_vint(archive_flags)
    return write_block(BLOCK_MAIN, 0, block_specific)


def write_file_block(name: str, data: bytes, method: int = 0,
                     mtime: int = 0) -> bytes:
    """Write a RAR5 file block (type 2)."""
    file_flags = FHFL_UNIX_TIME | FHFL_CRC32

    # Build comp_info: version=0 on disk (RAR5), solid=0, method, dict_index
    # (Internally RAR calls this VER_PACK5=50 but stores 0 on disk)
    if method == 0:
        comp_info = 0  # store: version=0, method=0, dict=0
    else:
        dict_map = {1: 0, 2: 1, 3: 2, 4: 3, 5: 5}
        dict_bits = dict_map.get(method, 2)
        comp_info = 0 | (method << 7) | (dict_bits << 10)

    if method > 0:
        from .compressor import compress_rar5
        compressed_data = compress_rar5(data, method)
    else:
        compressed_data = data

    file_crc = crc32(data)
    attributes = 0o100644  # Unix st_mode for a regular file with 644 permissions
    name_bytes = name.encode('utf-8')

    block_specific = (
        encode_vint(file_flags) +
        encode_vint(len(data)) +
        encode_vint(attributes) +
        struct.pack('<I', mtime) +
        struct.pack('<I', file_crc) +
        encode_vint(comp_info) +
        encode_vint(1) +              # host_os = Unix
        encode_vint(len(name_bytes)) +
        name_bytes
    )

    return write_block(BLOCK_FILE, HFL_DATA, block_specific, compressed_data)


def write_eof_block() -> bytes:
    """Write the end-of-archive block (type 5)."""
    block_specific = encode_vint(0)  # eof_flags = 0
    return write_block(BLOCK_END, 0, block_specific)


def read_block(data: bytes, offset: int):
    """Read a single RAR5 block.

    Returns (block_type, header_flags, block_specific, block_data, new_offset)
    or None if insufficient data remains.
    """
    if offset + 4 > len(data):
        return None

    # stored_crc = struct.unpack_from('<I', data, offset)[0]  # validated if needed
    offset += 4

    hdr_size, offset = decode_vint(data, offset)
    hdr_start = offset

    if hdr_start + hdr_size > len(data):
        return None

    pos = hdr_start
    block_type, pos = decode_vint(data, pos)
    header_flags, pos = decode_vint(data, pos)

    if header_flags & HFL_EXTRA:
        _extra_size, pos = decode_vint(data, pos)

    data_size = 0
    if header_flags & HFL_DATA:
        data_size, pos = decode_vint(data, pos)

    block_specific = data[pos: hdr_start + hdr_size]
    data_start = hdr_start + hdr_size
    block_data = data[data_start: data_start + data_size]

    new_offset = data_start + data_size
    return block_type, header_flags, block_specific, block_data, new_offset
