# RAR CLI Flags

Derived from `rar/man.txt` (RAR 7.20 console version).

---

## Compression

| Flag | Description |
|------|-------------|
| `-m0` | Store – no compression |
| `-m1` | Fastest compression |
| `-m2` | Fast compression |
| `-m3` | Normal compression (default) |
| `-m4` | Good compression |
| `-m5` | Best (maximum) compression |

**Compatible commands:** `a`, `u`, `f`

---

## Path handling

| Flag  | Description |
|-------|-------------|
| `-ep`  | Exclude paths – store only the file name, strip all directory components |
| `-ep1` | Exclude base directory – strip the topmost directory from the stored path |
| `-ep2` | Use full absolute paths including drive letter |
| `-ep3` | Expand to full paths relative to the current drive root |

**Compatible commands:** `a`, `u`, `f`, `e`, `x`

---

## Recursion

| Flag | Description |
|------|-------------|
| `-r`  | Recurse into sub-directories when adding or extracting |
| `-r0` | Like `-r` but applies only to wildcards; bare directory names are not recursed |

**Compatible commands:** `a`, `d`, `e`, `f`, `i`, `l`, `p`, `t`, `u`, `v`, `x`

---

## Overwrite control

| Flag  | Description |
|-------|-------------|
| `-o+` | Overwrite existing files during extraction (default) |
| `-o-` | Do **not** overwrite existing files |
| `-or`  | Auto-rename extracted file if name already exists |

**Compatible commands:** `e`, `x`

---

## Password

| Flag        | Description |
|-------------|-------------|
| `-p[pass]`  | Encrypt/decrypt with password `pass`. If `pass` is omitted, RAR prompts. |
| `-hp[pass]` | Like `-p` but also encrypts file names in the archive |

**Compatible commands:** `a`, `e`, `x`, `t`, `p`

> **Note:** Password encryption is not implemented in this Python version.

---

## Archive structure

| Flag  | Description |
|-------|-------------|
| `-s`  | Create a solid archive – better compression but slower random access |
| `-s-` | Disable solid archiving |
| `-sv`  | Create independent solid volumes |
| `-sv-` | Create dependent solid volumes |

**Compatible commands:** `a`

---

## Volumes (multi-part archives)

| Flag         | Description |
|--------------|-------------|
| `-v[size]`   | Create volumes of the given size. Suffixes: `k`=KB, `m`=MB, `g`=GB. E.g. `-v100m` |
| `-vn`        | Use old style volume naming (`volname.r00`, `.r01`, …) |
| `-vsm`       | Enable smart volume sizes |

**Compatible commands:** `a`

> **Note:** Volume creation is not implemented in this Python version.

---

## Filtering

| Flag          | Description |
|---------------|-------------|
| `-x[mask]`    | Exclude files matching wildcard mask. Multiple `-x` flags allowed. |
| `-x@[list]`   | Exclude files listed in file `list` |
| `-n[mask]`    | Include **only** files matching mask. Multiple `-n` flags allowed. |
| `-n@[list]`   | Include only files listed in file `list` |

**Compatible commands:** `a`, `d`, `e`, `f`, `l`, `p`, `t`, `u`, `v`, `x`

---

## File selection

| Flag     | Description |
|----------|-------------|
| `-ta<date>` | Process files modified **after** `date` (YYYYMMDDHHMMSS format) |
| `-tb<date>` | Process files modified **before** `date` |
| `-tn<age>`  | Process files **newer** than `age` |
| `-to<age>`  | Process files **older** than `age` |
| `-tl`       | Set archive time to latest file time |
| `-ts`       | Save file times with full precision |

**Compatible commands:** `a`, `u`, `f`, `e`, `x`, `l`, `v`, `t`

---

## Behaviour / interaction

| Flag | Description |
|------|-------------|
| `-y`  | Yes to all questions; suppress interactive prompts |
| `-w[path]` | Use `path` as the working (temp) directory |

**Compatible commands:** all

---

## Output path

| Flag       | Description |
|------------|-------------|
| `-op<path>` | Set destination path for extraction (alternative to trailing `path/` argument) |

**Compatible commands:** `e`, `x`

---

## Informational / logging

| Flag          | Description |
|---------------|-------------|
| `-ilog[file]` | Write error messages to log file |
| `-inul`       | Suppress all messages |
| `-idn`        | Disable archive name output |
| `-idp`        | Disable progress indicator |

**Compatible commands:** all

---

## Self-extracting

| Flag       | Description |
|------------|-------------|
| `-sfx[module]` | Create self-extracting archive using the given SFX module |

**Compatible commands:** `a`, `s`

---

## Miscellaneous

| Flag        | Description |
|-------------|-------------|
| `-cl`       | Convert file names to lower case |
| `-cu`       | Convert file names to upper case |
| `-cfg-`     | Ignore configuration file and environment variable |
| `-z[file]`  | Read archive comment from `file` (or stdin if omitted) |
| `-rr[n]`    | Add recovery record protecting `n`% of data |
| `-rv[n]`    | Create `n` recovery volumes |
| `-sc<charset>[l|c]` | Specify character set for filenames or comments |
| `-sl<size>` | Process files smaller than `size` bytes |
| `-sm<size>` | Process files larger than `size` bytes |
