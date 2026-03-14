"""CRC32 and variable-length integer utilities for RAR 5.0."""

import binascii
import struct


def encode_vint(n):
    """Encode a non-negative integer as a RAR variable-length integer."""
    if n < 0:
        raise ValueError(f"vint must be non-negative, got {n}")
    result = bytearray()
    while True:
        byte = n & 0x7F
        n >>= 7
        if n:
            byte |= 0x80
        result.append(byte)
        if not n:
            break
    return bytes(result)


def decode_vint(data, pos=0):
    """Decode a vint from data at pos. Returns (value, new_pos)."""
    n, shift = 0, 0
    while True:
        if pos >= len(data):
            raise ValueError("Truncated vint")
        b = data[pos]
        pos += 1
        n |= (b & 0x7F) << shift
        shift += 7
        if not (b & 0x80):
            break
    return n, pos


def crc32(data):
    """Compute CRC32 and return as unsigned 32-bit integer."""
    return binascii.crc32(data) & 0xFFFFFFFF


def uint32_le(n):
    """Pack integer as 4-byte little-endian uint32."""
    return struct.pack('<I', n)


def read_uint32_le(data, pos):
    """Read 4-byte little-endian uint32 from data at pos."""
    return struct.unpack_from('<I', data, pos)[0], pos + 4


def read_uint64_le(data, pos):
    """Read 8-byte little-endian uint64 from data at pos."""
    return struct.unpack_from('<Q', data, pos)[0], pos + 8


def filetime_to_unix(ft):
    """Convert Windows FILETIME (100ns intervals since 1601-01-01) to Unix timestamp."""
    # 116444736000000000 = 100ns intervals between 1601-01-01 and 1970-01-01
    return (ft - 116444736000000000) // 10000000


def unix_to_filetime(ts):
    """Convert Unix timestamp to Windows FILETIME."""
    return ts * 10000000 + 116444736000000000
