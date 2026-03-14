# RAR 5.0 Command × Option Matrix

This table shows which flags apply to each command.

| Flag / Option | `a` (add) | `e` (extract) | `x` (extract w/paths) | `l` (list) | `t` (test) | `d` (delete) |
|---------------|:---------:|:--------------:|:---------------------:|:----------:|:----------:|:------------:|
| `-m0..5`      | ✓         |                |                       |            |            |              |
| `-r`          | ✓         | ✓              | ✓                     |            |            | ✓            |
| `-y`          | ✓         | ✓              | ✓                     |            |            | ✓            |
| `-o+`         |           | ✓              | ✓                     |            |            |              |
| `-p<pwd>`     | ✓         | ✓              | ✓                     |            |            |              |

---

## Command × Block Type Matrix

Which block types each command reads or writes:

| Block Type          | `a` write | `e`/`x` read | `l` read | `t` read | `d` read/write |
|---------------------|:---------:|:------------:|:--------:|:--------:|:--------------:|
| Signature (8 bytes) | ✓         | ✓            | ✓        | ✓        | ✓              |
| BLOCK_MAIN (1)      | ✓         | ✓            | ✓        | ✓        | ✓              |
| BLOCK_FILE (2)      | ✓         | ✓            | ✓        | ✓        | ✓              |
| BLOCK_SERVICE (3)   | —         | skip         | skip     | skip     | passthrough    |
| BLOCK_ENCRYPTION(4) | —         | skip         | skip     | skip     | passthrough    |
| BLOCK_END (5)       | ✓         | ✓            | ✓        | ✓        | ✓              |

---

## Compression Method × Feature Matrix

| Feature              | `-m0` Store | `-m1`–`-m5` Compress |
|----------------------|:-----------:|:--------------------:|
| LZ77 matching        | —           | ✓                    |
| Huffman coding       | —           | ✓                    |
| CRC32 stored         | ✓           | ✓                    |
| mtime stored         | ✓           | ✓                    |
| data_size in header  | ✓           | ✓                    |
| Block header present | —           | ✓                    |
| Decompressible by unrar | ✓        | ✓                    |

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0    | Success |
| 1    | Warning (non-fatal errors) |
| 2    | Fatal error |
| 3    | CRC error |
| 4    | Lock error |
| 5    | Write error |
| 6    | Open error |
| 7    | User error |
| 8    | Memory error |
| 255  | User break |
