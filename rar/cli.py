"""Argument parsing for the Python RAR CLI."""

import argparse
import sys


def build_parser():
    parser = argparse.ArgumentParser(
        prog='rar',
        description='Python RAR 5.0 archiver',
        add_help=True,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Commands:
  a   Add files to archive
  e   Extract files without paths
  x   Extract files with full paths
  l   List archive contents
  v   Verbosely list archive contents
  t   Test archive integrity
  d   Delete files from archive
  u   Update files in archive
  f   Freshen files in archive (update existing only)
  p   Print file to stdout
  c   Add archive comment
  k   Lock archive

Examples:
  rar a archive.rar file.txt          Add file.txt to archive.rar
  rar a -r archive.rar dir/           Add directory recursively
  rar x archive.rar /tmp/extract/     Extract with full paths
  rar l archive.rar                   List contents
  rar t archive.rar                   Test integrity
""",
    )

    # Positional: command
    parser.add_argument(
        'command',
        metavar='COMMAND',
        help='Command (a, e, x, l, v, t, d, u, f, p, c, k)',
    )

    # Positional: archive name
    parser.add_argument(
        'archive',
        metavar='ARCHIVE',
        help='Archive file path',
    )

    # Positional: files / destination
    parser.add_argument(
        'files',
        metavar='FILE',
        nargs='*',
        help='Files to add/extract/list, or destination directory',
    )

    # ── Compression ─────────────────────────────────────────────────────────
    m_group = parser.add_mutually_exclusive_group()
    m_group.add_argument('-m0', dest='compression_level', action='store_const', const=0,
                         help='Store (no compression)')
    m_group.add_argument('-m1', dest='compression_level', action='store_const', const=1,
                         help='Fastest compression')
    m_group.add_argument('-m2', dest='compression_level', action='store_const', const=2,
                         help='Fast compression')
    m_group.add_argument('-m3', dest='compression_level', action='store_const', const=3,
                         help='Normal compression (default)')
    m_group.add_argument('-m4', dest='compression_level', action='store_const', const=4,
                         help='Good compression')
    m_group.add_argument('-m5', dest='compression_level', action='store_const', const=5,
                         help='Best compression')

    # ── Behaviour flags ──────────────────────────────────────────────────────
    parser.add_argument('-r', dest='recurse', action='store_true',
                        help='Recurse into subdirectories')

    parser.add_argument('-s', dest='solid', action='store_true',
                        help='Create solid archive')

    parser.add_argument('-y', dest='yes_all', action='store_true',
                        help='Assume yes on all queries')

    # ── Overwrite ────────────────────────────────────────────────────────────
    ow_group = parser.add_mutually_exclusive_group()
    ow_group.add_argument('-o+', dest='overwrite', action='store_true', default=True,
                          help='Overwrite existing files on extraction (default)')
    ow_group.add_argument('-o-', dest='overwrite', action='store_false',
                          help='Do not overwrite existing files on extraction')

    # ── Path handling ────────────────────────────────────────────────────────
    parser.add_argument('-ep', dest='ep', action='store_true',
                        help='Exclude paths from names')
    parser.add_argument('-ep1', dest='ep1', action='store_true',
                        help='Exclude base directory from names')

    # ── Password (stub) ──────────────────────────────────────────────────────
    parser.add_argument('-p', dest='password', metavar='PASSWORD', nargs='?',
                        const='', help='Set password (not implemented)')

    # ── Filtering ────────────────────────────────────────────────────────────
    parser.add_argument('-x', dest='exclude_patterns', metavar='PATTERN',
                        action='append', default=[],
                        help='Exclude files matching pattern')
    parser.add_argument('-n', dest='include_patterns', metavar='PATTERN',
                        action='append', default=[],
                        help='Include only files matching pattern')

    # ── Comment ──────────────────────────────────────────────────────────────
    parser.add_argument('-z', dest='comment_file', metavar='FILE',
                        help='Read archive comment from file')

    # ── Volume size (stub) ───────────────────────────────────────────────────
    parser.add_argument('-v', dest='volume_size', metavar='SIZE',
                        help='Create volumes of SIZE bytes (stub)')

    # ── Output path ──────────────────────────────────────────────────────────
    parser.add_argument('-op', dest='output_path', metavar='PATH',
                        help='Output path for extracted files')

    return parser


def parse_args(argv=None):
    """
    Parse command line arguments, handling RAR-style switches like -o+/-o-.

    Returns an argparse.Namespace.
    """
    if argv is None:
        argv = sys.argv[1:]

    # Pre-process argv: convert -o+ / -o- to --o+ / --o- compatible forms
    # argparse doesn't support '+' in flag names, so we map manually.
    processed = []
    for arg in argv:
        if arg == '-o+':
            processed.append('-o+')
        elif arg == '-o-':
            processed.append('-o-')
        else:
            processed.append(arg)

    parser = build_parser()

    # Handle -o+/-o- manually before passing to argparse
    overwrite = True
    clean = []
    for arg in processed:
        if arg == '-o+':
            overwrite = True
        elif arg == '-o-':
            overwrite = False
        else:
            clean.append(arg)

    namespace = parser.parse_args(clean)
    namespace.overwrite = overwrite
    if namespace.compression_level is None:
        namespace.compression_level = 3  # default: normal

    return namespace
