# RAR 5.0 Compression Levels

## Method Overview

| Method | Flag | Name    | Description |
|--------|------|---------|-------------|
| 0      | `-m0`| Store   | No compression; data is stored verbatim |
| 1      | `-m1`| Fastest | Minimum compression effort; very fast |
| 2      | `-m2`| Fast    | Light compression |
| 3      | `-m3`| Normal  | Balanced speed/ratio (default) |
| 4      | `-m4`| Good    | Better ratio at some speed cost |
| 5      | `-m5`| Best    | Maximum compression effort |

---

## `comp_info` Encoding

Each file header stores a `comp_info` vint that encodes the compression parameters:

```
bits [5:0]   — Algorithm version. Always 50 (decimal) for RAR5.
bit  [6]     — Solid flag. 1 if this file is part of a solid stream.
bits [9:7]   — Method (0 = store, 1–5 = compression levels).
bits [14:10] — Dictionary size index.
```

**Formula:**
```python
comp_info = version | (solid << 6) | (method << 7) | (dict_index << 10)
```

where `version = 0` (stored on disk as `0` for RAR5; the internal constant `VER_PACK5 = 50`
is not stored — unrar adds 50 internally when reading `0` from the file).

---

## Dictionary Size Index

| Method | Dict Index | Dictionary Size |
|--------|-----------|-----------------|
| -m1    | 0         | 128 KB          |
| -m2    | 1         | 256 KB          |
| -m3    | 2         | 512 KB          |
| -m4    | 3         | 1 MB            |
| -m5    | 5         | 4 MB            |

---

## Huffman Table Structure

The compressor uses four Huffman tables per block:

| Table | Symbols | Purpose |
|-------|---------|---------|
| LD    | 306     | Literals (0–255) + EOB (256) + length slots (262–305) |
| DD    | 64      | Distance slots |
| LDD   | 16      | Low 4 bits of large distances |
| RD    | 44      | Repeat/match codes |

**Total symbols encoded in base table:** 430 (`NC + DCB + LDC + RC`)

The 430 bit-lengths are themselves encoded using a base (meta) Huffman table with 20 symbols (`BC = 20`), whose 4-bit lengths are written literally at the start of each block.

---

## Block Header Format

Each compressed block is prefixed with 2–5 bytes:

```
[BlockFlags: 1 byte] [CheckSum: 1 byte] [BlockSize: 1–3 bytes LE]
```

`CheckSum = 0x5A ^ BlockFlags ^ BlockSize ^ (BlockSize >> 8) ^ (BlockSize >> 16)`

The compressed bit stream follows immediately.
