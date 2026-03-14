# RAR 5.0 Commands

## Command Summary

| Command | Description |
|---------|-------------|
| `a`     | Add files to archive |
| `e`     | Extract files (without directory paths) |
| `x`     | Extract files (preserving directory paths) |
| `l`     | List archive contents |
| `t`     | Test archive integrity |
| `d`     | Delete files from archive |

---

## Command Details

### `a` — Add Files to Archive

```
rar a [options] <archive.rar> <file1> [file2 ...]
```

Creates a new archive or appends to an existing one. If the archive does not exist, it is created. Files matching the given paths or glob patterns are added.

**Examples:**
```bash
rar a archive.rar file.txt
rar a -m5 archive.rar src/
rar a -r archive.rar /project/
```

---

### `e` — Extract Without Paths

```
rar e [options] <archive.rar> [dest/]
```

Extracts all files from the archive to the destination directory, ignoring any directory structure stored in the archive. All files are placed flat in the destination.

**Examples:**
```bash
rar e archive.rar
rar e archive.rar /tmp/output/
```

---

### `x` — Extract With Paths

```
rar x [options] <archive.rar> [dest/]
```

Extracts all files from the archive, recreating the stored directory structure under the destination path.

**Examples:**
```bash
rar x archive.rar
rar x archive.rar /tmp/output/
```

---

### `l` — List Contents

```
rar l <archive.rar>
```

Lists all files stored in the archive with their sizes, packed sizes, and compression method.

**Output columns:** Name, Size, Packed, Method

---

### `t` — Test Integrity

```
rar t <archive.rar>
```

Verifies CRC32 checksums for all stored files. Prints `OK: <name>` for passing files and `CRC error: <name>` for failures. Returns exit code 0 if all OK, nonzero on error.

---

### `d` — Delete Files

```
rar d <archive.rar> <pattern1> [pattern2 ...]
```

Removes files matching the given filename patterns (supports wildcards) from the archive. A new archive is written without the matching entries.

**Examples:**
```bash
rar d archive.rar *.tmp
rar d archive.rar old_file.txt
```
