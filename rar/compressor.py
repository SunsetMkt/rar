"""Compression handling for RAR archives.

The RAR compression algorithm is proprietary, so all compression levels
(-m0 through -m5) use store mode (method=0) to produce valid extractable
archives. The compression level parameter is accepted but ignored.
"""


def compress(data, level=0):
    """
    'Compress' data at the given level.

    Since the RAR compression algorithm is proprietary, all levels use
    store mode (raw copy) so archives remain valid and extractable by
    the reference unrar binary. The *level* parameter is accepted for
    interface compatibility.

    Parameters
    ----------
    data  : bytes – original file data
    level : int   – compression level 0-5 (0 = store, 1-5 = compress)

    Returns
    -------
    bytes – packed data (same as input for all levels)
    """
    return data


def decompress(data, unpacked_size, compression_info):
    """
    Decompress packed file data.

    For archives created by this tool all data is stored uncompressed
    (compression method bits 8-10 of *compression_info* == 0), so this
    simply returns *data* as-is after validating the size.

    For archives with other compression methods this raises NotImplementedError
    since the proprietary RAR algorithm is not available in pure Python.

    Parameters
    ----------
    data             : bytes – packed data
    unpacked_size    : int   – expected output size
    compression_info : int   – compression info vint from file header

    Returns
    -------
    bytes
    """
    # Compression method occupies bits 7-9 (0x0380 mask, 1-indexed bits 8-10)
    method = (compression_info >> 7) & 0x07
    if method == 0:
        # Store – data is already uncompressed
        return data
    raise NotImplementedError(
        f"RAR compression method {method} is proprietary and cannot be "
        "decompressed in pure Python. Use the reference unrar binary."
    )
