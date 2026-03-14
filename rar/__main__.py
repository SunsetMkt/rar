"""Entry point: python -m rar <command> ..."""

import os
import sys


def main():
    from .cli     import parse_args
    from .archive import (cmd_add, cmd_delete, cmd_update, cmd_extract,
                          cmd_extract_full, cmd_list, cmd_test, cmd_print,
                          cmd_comment, cmd_lock)

    args = parse_args()
    cmd  = args.command.lower()

    opts = {
        'recurse'          : args.recurse,
        'compression_level': args.compression_level,
        'solid'            : args.solid,
        'yes_all'          : args.yes_all,
        'overwrite'        : args.overwrite,
        'ep'               : args.ep,
        'ep1'              : args.ep1,
        'include_patterns' : args.include_patterns or [],
        'exclude_patterns' : args.exclude_patterns or [],
        'password'         : args.password,
    }

    archive = args.archive
    files   = args.files

    # Determine destination directory for extraction commands
    def _dest_dir():
        """Last element of files if it looks like a dir arg, else cwd."""
        if files and (files[-1].endswith('/') or files[-1].endswith(os.sep)
                      or os.path.isdir(files[-1])):
            return files[-1]
        if args.output_path:
            return args.output_path
        return os.getcwd()

    def _file_patterns():
        """Patterns for filtering (all files args that aren't the dest dir)."""
        if not files:
            return []
        dest = _dest_dir()
        return [f for f in files if f != dest]

    rc = 0

    if cmd == 'a':
        rc = cmd_add(archive, files, opts)

    elif cmd == 'e':
        dest     = _dest_dir()
        patterns = _file_patterns()
        rc = cmd_extract(archive, dest, patterns, opts)

    elif cmd == 'x':
        dest     = _dest_dir()
        patterns = _file_patterns()
        rc = cmd_extract_full(archive, dest, patterns, opts)

    elif cmd in ('l', 'v'):
        rc = cmd_list(archive, verbose=(cmd == 'v'))

    elif cmd == 't':
        rc = cmd_test(archive, files, opts)

    elif cmd == 'd':
        if not files:
            print("rar: d command requires file patterns to delete.", file=sys.stderr)
            rc = 1
        else:
            rc = cmd_delete(archive, files, opts)

    elif cmd == 'u':
        rc = cmd_update(archive, files, opts, freshen_only=False)

    elif cmd == 'f':
        rc = cmd_update(archive, files, opts, freshen_only=True)

    elif cmd == 'p':
        rc = cmd_print(archive, files, opts)

    elif cmd == 'c':
        comment = ''
        if args.comment_file:
            try:
                with open(args.comment_file) as f:
                    comment = f.read()
            except OSError as e:
                print(f"rar: cannot read comment file: {e}", file=sys.stderr)
                rc = 1
        else:
            print("Enter archive comment (end with Ctrl-D or empty line):")
            lines = []
            try:
                while True:
                    line = input()
                    lines.append(line)
            except EOFError:
                pass
            comment = '\n'.join(lines)
        if rc == 0:
            rc = cmd_comment(archive, comment)

    elif cmd == 'k':
        rc = cmd_lock(archive)

    else:
        print(f"rar: unknown command '{cmd}'. "
              "Use one of: a e x l v t d u f p c k", file=sys.stderr)
        rc = 1

    sys.exit(rc)


if __name__ == '__main__':
    main()
