"""Command-line interface for the RAR 5.0 archiver."""

import sys
from .archive import Archive


def main():
    args = sys.argv[1:]

    if not args:
        _print_usage()
        return

    command = args[0].lower()

    method = 3        # default compression level
    recurse = False
    positional = []

    i = 1
    while i < len(args):
        arg = args[i]
        if len(arg) == 3 and arg.startswith('-m') and arg[2].isdigit():
            method = int(arg[2])
        elif arg == '-r':
            recurse = True
        elif arg in ('-y', '-o+', '-o'):
            pass   # overwrite / yes-to-all — default behavior already allows this
        elif arg.startswith('-p'):
            pass   # password encryption — not implemented
        elif not arg.startswith('-'):
            positional.append(arg)
        i += 1

    arc = Archive()

    if command == 'a':
        if len(positional) < 2:
            print("Usage: rar a [options] <archive.rar> <file1> ...")
            sys.exit(1)
        archive_path = positional[0]
        file_paths = positional[1:]
        arc.add(archive_path, file_paths, method=method, recurse=recurse)
        print(f"Created {archive_path}")

    elif command in ('e', 'x'):
        if not positional:
            print(f"Usage: rar {command} [options] <archive.rar> [dest/]")
            sys.exit(1)
        archive_path = positional[0]
        dest = positional[1] if len(positional) > 1 else '.'
        arc.extract(archive_path, dest, include_paths=(command == 'x'))

    elif command == 'l':
        if not positional:
            print("Usage: rar l <archive.rar>")
            sys.exit(1)
        archive_path = positional[0]
        entries = arc.list_contents(archive_path)
        print(f"{'Name':<40} {'Size':>10} {'Packed':>10}  Method")
        print('-' * 72)
        for e in entries:
            mname = 'store' if e['method'] == 0 else f"m{e['method']}"
            print(f"{e['name']:<40} {e['unpacked_size']:>10} {e['packed_size']:>10}  {mname}")

    elif command == 't':
        if not positional:
            print("Usage: rar t <archive.rar>")
            sys.exit(1)
        archive_path = positional[0]
        ok = arc.test(archive_path)
        if ok:
            print("All OK")
        else:
            sys.exit(3)

    elif command == 'd':
        if len(positional) < 2:
            print("Usage: rar d <archive.rar> <pattern1> ...")
            sys.exit(1)
        archive_path = positional[0]
        patterns = positional[1:]
        arc.delete(archive_path, patterns)
        print(f"Updated {archive_path}")

    else:
        print(f"Unknown command: {command!r}")
        _print_usage()
        sys.exit(1)


def _print_usage():
    print("Usage: rar <command> [options] <archive.rar> [files...]")
    print()
    print("Commands:")
    print("  a   Add files to archive")
    print("  e   Extract without paths")
    print("  x   Extract with paths")
    print("  l   List archive contents")
    print("  t   Test archive integrity")
    print("  d   Delete files from archive")
    print()
    print("Options:")
    print("  -m0..5   Compression method (0=store, 3=normal default, 5=best)")
    print("  -r       Recurse into subdirectories")
    print("  -y       Answer yes to all prompts")
