# RAR 5.0 Archive Structure

## File Layout

```
[Signature: 8 bytes]
[Archive Header block]
[File Header block 1]
  ...
[File Header block N]
[End-of-Archive block]
```

---

## Signature

```
52 61 72 21 1a 07 01 00
R  a  r  !  .  .  .  .
```

The first 8 bytes of every RAR 5.0 archive.

---

## Block Format

Every block follows this structure:

```
[CRC32: 4 bytes LE]        — CRC32 of (vint(header_size) + header_data)
[header_size: vint]        — byte length of header_data
[header_data]
  [block_type: vint]
  [header_flags: vint]
  [extra_size: vint]       — only if HFL_EXTRA set
  [data_size: vint]        — only if HFL_DATA set
  [block-specific fields]
[data area: data_size bytes]  — only if HFL_DATA set
```

---

## vint Encoding

Variable-length integers, little-endian 7-bit groups:

- Each byte contributes 7 data bits (bits 0–6).
- Bit 7 = 1 means more bytes follow; bit 7 = 0 means this is the last byte.

**Example:**  
Value `300` = `0x12C` → encoded as `AC 02` (i.e., `0xAC 0x02`)

```python
def encode_vint(value):
    result = bytearray()
    while True:
        byte = value & 0x7f
        value >>= 7
        if value:
            byte |= 0x80
        result.append(byte)
        if not value:
            break
    return bytes(result)
```

---

## Block Types

| Type | Name | Description |
|------|------|-------------|
| 1    | BLOCK_MAIN | Archive header (global archive flags) |
| 2    | BLOCK_FILE | File entry (header + compressed data) |
| 3    | BLOCK_SERVICE | Service record (ignored by basic readers) |
| 4    | BLOCK_ENCRYPTION | Encryption header |
| 5    | BLOCK_END | End of archive marker |

---

## Archive Header (type 1)

```
header_flags = 0          (no extra, no data)
archive_flags: vint       (0 = no special flags)
```

---

## File Header (type 2)

```
header_flags = HFL_DATA (0x0002)

block_specific fields (in order):
  file_flags:      vint   (FHFL_UNIX_TIME | FHFL_CRC32 = 0x06)
  unpacked_size:   vint
  attributes:      vint   (Unix st_mode value; regular file with 644 = 0o100644 = 33188)
  mtime:           uint32 LE   [if FHFL_UNIX_TIME]
  file_crc32:      uint32 LE   [if FHFL_CRC32]
  comp_info:       vint   (version=0 | method<<7 | dict_index<<10)
  host_os:         vint   (1 = Unix)
  name_len:        vint
  name:            UTF-8 bytes
```

---

## End-of-Archive Block (type 5)

```
header_flags = 0
eof_flags: vint   (0 = no additional flags)
```

---

## Compressed Data Area

For files stored with method > 0, the data area contains one or more RAR5 compression blocks:

```
[BlockFlags: 1 byte]
[CheckSum:   1 byte]
[BlockSize:  1–3 bytes LE]
[compressed bit stream: BlockSize bytes]
```

The bit stream is MSB-first within each byte and contains:
1. Base table (20 × 4-bit Huffman lengths)
2. Meta-encoded 430 Huffman lengths (using the base table)
3. Compressed symbols (LD/DD/LDD codes + extra bits)
