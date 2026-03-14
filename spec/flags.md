# RAR 5.0 Flags and Switches

## Command-Line Flags

| Flag        | Description |
|-------------|-------------|
| `-m0`       | Store only (no compression) |
| `-m1`       | Fastest compression |
| `-m2`       | Fast compression |
| `-m3`       | Normal compression (default) |
| `-m4`       | Good compression |
| `-m5`       | Best compression |
| `-r`        | Recurse into subdirectories |
| `-y`        | Answer "yes" to all prompts |
| `-o+`       | Overwrite existing files on extract |
| `-p<pwd>`   | Encrypt archive with password (not implemented) |

---

## Internal Header Flags

### Block-Level Header Flags (`HFL_*`)

| Constant          | Value  | Meaning |
|-------------------|--------|---------|
| `HFL_EXTRA`       | 0x0001 | Block contains an extra area |
| `HFL_DATA`        | 0x0002 | Block contains a data area |
| `HFL_SKIP_IF_UNKNOWN` | 0x0004 | Skip block if type is unknown |

### Archive Header Flags (`MHFL_*`)

| Constant      | Value  | Meaning |
|---------------|--------|---------|
| `MHFL_VOLUME` | 0x0001 | Archive is part of a multi-volume set |
| `MHFL_SOLID`  | 0x0004 | Solid archive (shared compression dictionary) |
| `MHFL_PROTECT`| 0x0008 | Recovery record present |
| `MHFL_LOCK`   | 0x0010 | Archive is locked (no modifications allowed) |

### File Header Flags (`FHFL_*`)

| Constant           | Value  | Meaning |
|--------------------|--------|---------|
| `FHFL_DIRECTORY`   | 0x0001 | Entry is a directory |
| `FHFL_UNIX_TIME`   | 0x0002 | Unix modification time (uint32) is present |
| `FHFL_CRC32`       | 0x0004 | CRC32 of uncompressed data is present |
| `FHFL_UNKNOWN_SIZE`| 0x0008 | Unpacked size is unknown |

### End-of-Archive Flags (`EHFL_*`)

| Constant          | Value  | Meaning |
|-------------------|--------|---------|
| `EHFL_NEXT_VOLUME`| 0x0001 | More volumes follow |

---

## Compression Block Flags

| Bit(s) | Field | Meaning |
|--------|-------|---------|
| 0–2    | `last_byte_bits - 1` | Number of valid bits in the last byte of the compressed stream, minus 1 |
| 3–4    | `ByteCount - 1` | Number of bytes used to encode BlockSize, minus 1 |
| 5      | (reserved) | Unused, must be 0 |
| 6      | `LastBlockInFile` | Set to 1 for the final (or only) compression block in a file |
| 7      | `TablePresent` | Set to 1 when Huffman tables are included in this block |
