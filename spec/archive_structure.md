# RAR 5.0 Archive Structure

Derived from `technote/technote.md`.

---

## File layout

```
┌──────────────────────────────────────┐
│  Self-extracting module (optional)   │  any size, precedes signature
├──────────────────────────────────────┤
│  RAR 5.0 Signature  (8 bytes)        │  52 61 72 21 1A 07 01 00
├──────────────────────────────────────┤
│  Archive Encryption Header (opt.)    │  only in header-encrypted archives
├──────────────────────────────────────┤
│  Main Archive Header                 │  header type 1
├──────────────────────────────────────┤
│  Archive Comment Service Header(opt) │  service header, name "CMT"
├──────────────────────────────────────┤
│  File Header 1                       │  header type 2
│  [Service headers for file 1]        │  NTFS ACL, streams, etc.
│  ...                                 │
│  File Header N                       │
│  [Service headers for file N]        │
├──────────────────────────────────────┤
│  Recovery Record (optional)          │  service header, name "RR"
├──────────────────────────────────────┤
│  Quick Open Header (optional)        │  service header, name "QO"
├──────────────────────────────────────┤
│  End of Archive Header               │  header type 5
└──────────────────────────────────────┘
```

---

## Data types

| Type     | Size     | Encoding |
|----------|----------|----------|
| `byte`   | 1 byte   | unsigned |
| `uint16` | 2 bytes  | unsigned, little-endian |
| `uint32` | 4 bytes  | unsigned, little-endian |
| `uint64` | 8 bytes  | unsigned, little-endian |
| `vint`   | 1–10 bytes | variable-length integer (see below) |

### vint encoding

Each byte: bits 0-6 = data, bit 7 = continuation flag (1 = more bytes follow).

```
Value 5     → 0x05               (1 byte)
Value 128   → 0x80 0x01          (2 bytes: 128 = 0b10000000)
Value 300   → 0xAC 0x02          (2 bytes: 300 = 0x12C, lower 7 = 0x2C|0x80, upper = 0x02)
```

Maximum value: 64-bit integer (10 bytes maximum).

---

## General block format

Every block in a RAR 5.0 archive uses this structure:

```
┌────────────────┬──────────┬───────────────────────────────────────────────────┐
│ Field          │ Size     │ Description                                        │
├────────────────┼──────────┼───────────────────────────────────────────────────┤
│ Header CRC32   │ uint32   │ CRC32 of: header_size_vint + everything below      │
│ Header size    │ vint     │ Bytes from Header type through end of extra area   │
│ Header type    │ vint     │ 1=main 2=file 3=service 4=encrypt 5=end            │
│ Header flags   │ vint     │ See common flags below                             │
│ Extra area size│ vint     │ Only if header flags & 0x0001                      │
│ Data size      │ vint     │ Only if header flags & 0x0002                      │
│ [type-specific]│ …        │ Fields specific to the block type                  │
│ Extra area     │ bytes    │ Only if header flags & 0x0001                      │
└────────────────┴──────────┴───────────────────────────────────────────────────┘
Data area (header flags & 0x0002) – NOT included in Header CRC32 or Header size
```

### Common header flags

| Bit    | Hex    | Meaning |
|--------|--------|---------|
| 0      | 0x0001 | Extra area is present at the end of the header |
| 1      | 0x0002 | Data area follows the header |
| 2      | 0x0004 | Skip block when updating if type is unknown |
| 3      | 0x0008 | Data area continues from previous volume |
| 4      | 0x0010 | Data area continues in next volume |
| 5      | 0x0020 | Block depends on preceding file block |
| 6      | 0x0040 | Preserve child block if host block is modified |

---

## Main archive header (type 1)

```
Header CRC32   uint32
Header size    vint
Header type    vint  = 1
Header flags   vint
[Extra area size vint]   if flags & 0x0001
Archive flags  vint      0x0001=volume 0x0002=vol_num 0x0004=solid 0x0008=recovery 0x0010=locked
[Volume number vint]     if archive flags & 0x0002
[Extra area    bytes]    if flags & 0x0001
```

**Extra record types for main header:**

| Type | Name     | Description |
|------|----------|-------------|
| 0x01 | Locator  | Offsets of quick-open and recovery blocks for fast access |
| 0x02 | Metadata | Original archive name and creation time |

---

## File header (type 2) and service header (type 3)

```
Header CRC32        uint32
Header size         vint
Header type         vint  = 2 (file) or 3 (service)
Header flags        vint
[Extra area size    vint]   if flags & 0x0001
[Data size          vint]   packed file size; if flags & 0x0002
File flags          vint    see below
Unpacked size       vint    uncompressed size
Attributes          vint    OS file attributes
[mtime              uint32] Unix timestamp; if file flags & 0x0002
[Data CRC32         uint32] CRC32 of unpacked data; if file flags & 0x0004
Compression info    vint    see below
Host OS             vint    0=Windows 1=Unix
Name length         vint
Name                bytes   UTF-8, no trailing zero
[Extra area         bytes]  if flags & 0x0001
--- data area follows (packed file data) ---
```

### File flags

| Bit | Hex    | Meaning |
|-----|--------|---------|
| 0   | 0x0001 | Directory entry |
| 1   | 0x0002 | mtime present in header (Unix uint32) |
| 2   | 0x0004 | Data CRC32 present in header |
| 3   | 0x0008 | Unpacked size unknown |

### Compression info field layout

```
Bits  0-5   version of compression algorithm (0 or 1)
Bit   6     solid flag (continue dictionary from previous file)
Bits  7-9   compression method: 0=store, 1=fastest … 5=best
Bits 10-14  min dictionary size: N → 128 KB × 2^N  (N=0 → 128 KB, N=5 → 4 MB …)
Bits 15-19  (version 1 only) dictionary fine-grain multiplier
Bit  20     (version 1 only) algorithm is version 0 despite version 1 size encoding
```

### Extra area record format (inside extra area)

```
Size   vint   bytes from Type to end of record Data
Type   vint   record type
Data   bytes  type-dependent
```

### File time extra record (type 0x03)

```
Size    vint
Type    vint  = 0x03
Flags   vint  0x0001=Unix format  0x0002=mtime present
              0x0004=ctime present  0x0008=atime present  0x0010=nanoseconds
[mtime  uint32 or uint64]  present if flags & 0x0002
[ctime  uint32 or uint64]  present if flags & 0x0004
[atime  uint32 or uint64]  present if flags & 0x0008
[mtime_ns uint32]          present if flags & (0x0001|0x0002|0x0010)
[ctime_ns uint32]          present if flags & (0x0001|0x0004|0x0010)
[atime_ns uint32]          present if flags & (0x0001|0x0008|0x0010)
```

- If `TIME_FLAG_UNIX` (0x0001) is set: times are Unix `time_t` (uint32 or uint64 with ns flag)
- Otherwise: times are Windows FILETIME (uint64, 100 ns intervals since 1601-01-01)

---

## End of archive header (type 5)

```
Header CRC32          uint32
Header size           vint
Header type           vint  = 5
Header flags          vint
End of archive flags  vint  0x0001=not last volume
```

---

## Service headers (type 3)

| Name field | Purpose |
|------------|---------|
| `CMT`      | Archive comment (UTF-8 text, uncompressed) |
| `QO`       | Quick open data cache (copies of file headers for fast listing) |
| `ACL`      | NTFS access control list |
| `STM`      | NTFS alternate data stream |
| `RR`       | Recovery record |

---

## CRC32 computation

- Algorithm: standard CRC-32 (ISO 3309 / ITU-T V.42)
- Input: header_size_vint bytes **+** all header content bytes (type through end of extra area)
- The four-byte CRC32 field itself is **excluded**
- The data area (packed file bytes) is **excluded**
- Python: `binascii.crc32(data) & 0xFFFFFFFF`
