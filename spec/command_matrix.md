# RAR Command / Flag / Compression Level Compatibility Matrix

## Legend

| Symbol | Meaning |
|--------|---------|
| ✓      | Supported / applicable |
| –      | Not applicable |
| (stub) | Accepted by parser but not yet implemented |

---

## Commands × Flags

| Flag            | `a` | `e` | `x` | `l` | `v` | `t` | `d` | `u` | `f` | `p` | `c` | `k` | Notes |
|-----------------|-----|-----|-----|-----|-----|-----|-----|-----|-----|-----|-----|-----|-------|
| `-m0`…`-m5`     | ✓   | –   | –   | –   | –   | –   | –   | ✓   | ✓   | –   | –   | –   | Compression level; all use store internally |
| `-r`            | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | –   | –   | Recurse subdirectories |
| `-s`            | ✓   | –   | –   | –   | –   | –   | –   | –   | –   | –   | –   | –   | Solid archive |
| `-y`            | ✓   | ✓   | ✓   | –   | –   | ✓   | ✓   | ✓   | ✓   | –   | –   | –   | Yes to all prompts |
| `-o+`           | –   | ✓   | ✓   | –   | –   | –   | –   | –   | –   | –   | –   | –   | Overwrite on extract |
| `-o-`           | –   | ✓   | ✓   | –   | –   | –   | –   | –   | –   | –   | –   | –   | Don't overwrite |
| `-ep`           | ✓   | ✓   | ✓   | –   | –   | –   | –   | ✓   | ✓   | –   | –   | –   | Exclude paths |
| `-ep1`          | ✓   | –   | –   | –   | –   | –   | –   | ✓   | ✓   | –   | –   | –   | Exclude base dir |
| `-p[pass]`      | ✓   | ✓   | ✓   | –   | –   | ✓   | –   | ✓   | ✓   | ✓   | –   | –   | Password (stub) |
| `-x[pat]`       | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | –   | –   | Exclude pattern |
| `-n[pat]`       | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | –   | –   | Include-only pattern |
| `-v[size]`      | ✓   | –   | –   | –   | –   | –   | –   | –   | –   | –   | –   | –   | Volumes (stub) |
| `-z[file]`      | –   | –   | –   | –   | –   | –   | –   | –   | –   | –   | ✓   | –   | Comment from file |
| `-op<path>`     | –   | ✓   | ✓   | –   | –   | –   | –   | –   | –   | –   | –   | –   | Output path |
| `-ilog`         | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | ✓   | Log errors (stub) |

---

## Commands × Compression Levels

| Command | `-m0` | `-m1` | `-m2` | `-m3` | `-m4` | `-m5` | Default |
|---------|-------|-------|-------|-------|-------|-------|---------|
| `a`     | ✓     | ✓     | ✓     | ✓     | ✓     | ✓     | `-m3`   |
| `u`     | ✓     | ✓     | ✓     | ✓     | ✓     | ✓     | `-m3`   |
| `f`     | ✓     | ✓     | ✓     | ✓     | ✓     | ✓     | `-m3`   |
| `e`     | –     | –     | –     | –     | –     | –     | –       |
| `x`     | –     | –     | –     | –     | –     | –     | –       |
| `l`     | –     | –     | –     | –     | –     | –     | –       |
| `v`     | –     | –     | –     | –     | –     | –     | –       |
| `t`     | –     | –     | –     | –     | –     | –     | –       |
| `d`     | –     | –     | –     | –     | –     | –     | –       |
| `p`     | –     | –     | –     | –     | –     | –     | –       |
| `c`     | –     | –     | –     | –     | –     | –     | –       |
| `k`     | –     | –     | –     | –     | –     | –     | –       |

---

## Implementation status

| Feature                              | Status |
|--------------------------------------|--------|
| `a` – add files (store mode)         | ✓ Implemented |
| `a` – add files with compression     | (stub) Always uses store mode |
| `e` – extract without paths          | ✓ Implemented |
| `x` – extract with paths             | ✓ Implemented |
| `l` – list contents                  | ✓ Implemented |
| `v` – verbose list                   | ✓ Implemented |
| `t` – test integrity                 | ✓ Implemented |
| `d` – delete files                   | ✓ Implemented |
| `u` – update files                   | ✓ Implemented |
| `f` – freshen files                  | ✓ Implemented |
| `p` – print to stdout                | ✓ Implemented |
| `c` – archive comment                | (stub) |
| `k` – lock archive                   | ✓ Implemented |
| `-s` solid archives                  | ✓ Flag set; compression uses store |
| `-r` recurse                         | ✓ Implemented |
| `-p` password encryption             | (stub) Not implemented |
| `-v` volume creation                 | (stub) Not implemented |
| RAR 5.0 read (store mode)            | ✓ Implemented |
| RAR 5.0 read (compressed)            | ✗ Requires proprietary algorithm |
| CRC32 verification                   | ✓ Implemented |
| mtime preservation                   | ✓ Implemented |
