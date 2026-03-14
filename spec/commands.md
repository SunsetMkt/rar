# RAR CLI Commands

Derived from `rar/man.txt` (RAR 7.20 console version).

---

## `a` – Add files to archive

**Syntax:** `rar a [switches] <archive> [files...]`

Adds files to a new or existing RAR archive. If no file names are given, all files (`*.*`) are assumed. If a directory name is given (without trailing separator and without wildcards), all contents including sub-directories are added even without `-r`.

**Arguments:**
- `archive` – target archive path (`.rar` appended if no extension)
- `files`   – files/directories/wildcards to add

**Examples:**
```
rar a help.rar *.hlp
rar a -r archive.rar src/
rar a -m5 -s best.rar documents/
```

**Expected behaviour:**
- Creates the archive if it does not exist.
- Replaces existing entries with the same name.
- Reports each file being added.

---

## `c` – Add archive comment

**Syntax:** `rar c [switches] <archive>`

Adds or replaces the archive comment (max 256 KB). If `-z<file>` is specified, the comment is read from that file; otherwise RAR prompts interactively.

**Examples:**
```
rar c distrib.rar
rar c -zinfo.txt distrib.rar
```

---

## `ch` – Change archive parameters

**Syntax:** `rar ch [switches] <archive>`

Modifies archive parameters without re-compressing data (e.g. `-tl` to set archive time to latest file).

---

## `cw` – Write archive comment to file

**Syntax:** `rar cw [switches] <archive> [outfile]`

Writes the archive comment to a file (or stdout if no file given). Output encoding controlled by `-sc`.

---

## `d` – Delete files from archive

**Syntax:** `rar d [switches] <archive> [files/masks...]`

Removes matching entries from the archive. If all files are removed, the empty archive is also removed.

**Examples:**
```
rar d backup.rar *.tmp
```

---

## `e` – Extract files without paths

**Syntax:** `rar e [switches] <archive> [files...] [dest/]`

Extracts all (or matched) files to the destination directory, **stripping** the stored path component so all files land in the same directory.

**Examples:**
```
rar e archive.rar /tmp/out/
rar e archive.rar *.txt /tmp/out/
```

---

## `f` – Freshen files in archive

**Syntax:** `rar f [switches] <archive> [files...]`

Like `u` but does **not** add new files – only updates entries that are already in the archive and are older than the corresponding file on disk.

---

## `i` – Find string in archives

**Syntax:** `rar i[i|c|h|t]=<string> <archive>`

Searches archive contents for a string. Modifiers: `c` case-sensitive, `h` hex string, `t` name only.

---

## `k` – Lock archive

**Syntax:** `rar k <archive>`

Sets the locked flag, preventing any modifications by RAR.

---

## `l` – List archive contents (brief)

**Syntax:** `rar l [switches] <archive>`

Prints a table with file name, size, date/time, and attributes for each archived file.

**Example output:**
```
Archive: example.rar

Name                                       Size               Date  Attributes
------------------------------------------------------------------------------------
hello.txt                                    12  2024-01-15 10:30:00           .
------------------------------------------------------------------------------------
Total                                        12
1 file(s)
```

---

## `p` – Print file to stdout

**Syntax:** `rar p [switches] <archive> [files...]`

Outputs the raw (decompressed) contents of matched files to stdout. Useful for piping.

---

## `r` – Repair damaged archive

**Syntax:** `rar r [switches] <archive>`

Attempts to reconstruct a damaged archive using the recovery record if present.

---

## `s` – Convert to self-extracting

**Syntax:** `rar s [switches] <archive>`

Prepends an SFX module to create a self-extracting executable.

---

## `t` – Test archive files

**Syntax:** `rar t [switches] <archive> [files...]`

Decompresses each file in memory and verifies its CRC32. Reports OK or error per file.

**Example output:**
```
OK        hello.txt

All OK (1 file(s))
```

---

## `u` – Update files in archive

**Syntax:** `rar u [switches] <archive> [files...]`

Adds new files and replaces existing entries that are older than the disk version. Unlike `f`, it **does** add new files.

---

## `v` – Verbose list

**Syntax:** `rar v [switches] <archive>`

Like `l` but adds packed size, compression ratio, and CRC32 columns.

---

## `x` – Extract files with full paths

**Syntax:** `rar x [switches] <archive> [files...] [dest/]`

Extracts files **preserving** the directory structure stored in the archive.

**Examples:**
```
rar x archive.rar /tmp/out/
rar x archive.rar src/*.c /tmp/out/
```
