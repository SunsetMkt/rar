# RAR Compression Levels

## Overview

RAR supports six compression levels, selected with the `-m<n>` switch when using the `a`, `u`, or `f` commands. The default level is `-m3` (Normal).

| Level | Flag  | Name     | Description |
|-------|-------|----------|-------------|
| 0     | `-m0` | Store    | No compression – files are stored verbatim. Fastest; largest archives. |
| 1     | `-m1` | Fastest  | Minimal compression. Very fast with small size reduction. |
| 2     | `-m2` | Fast     | Fast compression with moderate size reduction. |
| 3     | `-m3` | Normal   | Balanced compression (default). Good ratio / speed trade-off. |
| 4     | `-m4` | Good     | Better ratio, slower. |
| 5     | `-m5` | Best     | Maximum compression. Slowest; smallest archives. |

---

## Behaviour per level

### `-m0` – Store
- Compression method byte in file header: **0** (no compression)
- Data area = raw file bytes
- CRC32 of uncompressed data stored in header
- Dictionary size field irrelevant (set to 0 = 128 KB)
- Fastest operation; archive size ≈ sum of file sizes + header overhead

### `-m1` – Fastest
- Compression method: **1**
- Uses a small sliding dictionary (typically 256 KB)
- Suitable for compressing many small files quickly

### `-m2` – Fast
- Compression method: **2**
- Larger dictionary than `-m1`; slightly better ratio

### `-m3` – Normal (default)
- Compression method: **3**
- Dictionary size typically 4 MB
- Good general-purpose compression

### `-m4` – Good
- Compression method: **4**
- Larger dictionary; slower but better ratio than `-m3`

### `-m5` – Best
- Compression method: **5**
- Largest dictionary (up to the value specified by `-md`)
- Best ratio; suitable for maximum size reduction at the cost of time

---

## Compression information field (RAR 5.0 binary)

The compression info is stored as a `vint` in the file header.
All bit positions below use **0-based** (LSB = bit 0) indexing:

```
Bits  0-5   (0x003F): Algorithm version. 0 = RAR 5.0+, 1 = RAR 7.0+
Bit   6     (0x0040): Solid flag – continue dictionary from previous file
Bits  7-9   (0x0380): Compression method (0-5, matching -m level)
              (the technote describes these as "bits 8-10" using 1-based indexing)
Bits 10-14  (0x7C00): Minimum dictionary size (N → 128 KB × 2^N)
```

For example, `-m3` non-solid with 4 MB dictionary (N=5):
```
version=0, solid=0, method=3, dict_bits=5
value = (0) | (0<<6) | (3<<7) | (5<<10) = 0x1580
```

---

## Python implementation note

> **Important:** The RAR compression algorithm is proprietary. This Python
> implementation uses **store mode (method=0) for all compression levels** so
> that archives are always valid and fully extractable by the reference `unrar`
> binary. The compression level flag is accepted on the command line but
> currently has no effect on the stored data.

---

## Dictionary size (`-md<n>`)

The `-md<n>` switch sets the compression dictionary size (e.g. `-md64m` for 64 MB). Larger dictionaries generally improve compression on large files at the cost of memory. The dictionary size is encoded in bits 10-14 of the compression info field.

Default dictionary sizes per level:

| Level | Approx. dictionary |
|-------|--------------------|
| `-m1` | 128 KB  |
| `-m2` | 256 KB  |
| `-m3` | 4 MB    |
| `-m4` | 32 MB   |
| `-m5` | 256 MB  |
