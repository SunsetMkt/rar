package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const version = "RAR 1.0   Copyright (c) 2026\nImplementation of RAR 5.0 archive format\n"

// Exit codes matching RAR specification
const (
	exitSuccess      = 0
	exitWarning      = 1
	exitFatal        = 2
	exitCRCError     = 3
	exitLocked       = 4
	exitWriteError   = 5
	exitOpenError    = 6
	exitCmdLineError = 7
	exitNoMemory     = 8
	exitCreateError  = 9
	exitNoFiles      = 10
	exitBadPassword  = 11
	exitReadError    = 12
	exitBadArchive   = 13
	exitUserStop     = 255
)

// options holds parsed command-line options
type options struct {
	// commands
	command string

	// archive and file parameters
	archivePath string
	files       []string

	// switches
	compLevel     int      // -m0...-m5 (default 3)
	recursive     int      // 0=off, 1=-r, 2=-r0 (wildcards only), -1=-r- (force off)
	solid         bool     // -s
	solidOff      bool     // -s=-
	overwrite     int      // 0=ask, 1=always (-o+), -1=skip (-o-)
	password      string   // -p<pwd>
	hdrPassword   string   // -hp<pwd>
	assumeYes     bool     // -y
	test          bool     // -t (test after archiving)
	keepTime      bool     // -tk
	setLatestTime bool     // -tl
	excludes      []string // -x<f>
	includes      []string // -n<f>
	extractPath   string   // -op<path>
	workDir       string   // -w<p>
	noComment     bool     // -c-
	noProgress    bool     // -id contains 'p' or -inul
	noMessages    bool     // -inul or -idq
	volumeSize    []int64  // -v<size>
	autoRename    bool     // -or
	lock          bool     // -k  (included in command list too)
	freshOnly     bool     // -u (update mode in add)
	saveOwner     bool     // -ow
	saveStreams    bool     // -os
	epMode        int      // 0=default, 1=-ep, 2=-ep1, 3=-ep2, 4=-ep3
	pathPrefix    string   // -ap<path>
	commentFile   string   // -z<file>
	listFile      []string // @listfile
	sfxModule     string   // -sfx[name]
	rrPct         int      // recovery record percentage
	move          bool     // for m command
	noRecurse     bool     // -r-
	renameList    []string // rn command: old new pairs
	verboseList   bool     // -v list switch
	showVersion   bool     // from -iver or --version
	noConfig      bool     // -cfg-
	stdinRead     bool     // -si
	stdoutWrite   bool     // -so
	sizeLimit     int64    // -sl<size>
	sizeMinimum   int64    // -sm<size>
	deleteAfter   bool     // -df
	wipeAfter     bool     // -dw
	noEmptyDirs   bool     // -ed
	keepBroken    bool     // -kb
	archiveAttr   bool     // -ao (only archive attribute)
	syncContents  bool     // -as (synchronize archive)
	logFile       string   // -ilog[name]
	sendErrors    bool     // -ierr
	noRecovery    bool     // for -rr with N=0
	toLower       bool     // -cl
	toUpper       bool     // -cu
	findString    string   // i command string
	findFlags     string   // i command flags (c, h, t, etc.)

	// display/logging options
	displayBare bool // -idc (no info messages)
	displayDots bool // -idd (show dots)
}

func defaultOptions() options {
	return options{
		compLevel: 3,
		overwrite: 0, // ask
	}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	// Special case: just '-?' or '--help'
	first := os.Args[1]
	if first == "-?" || first == "-h" || first == "--help" {
		printHelp()
		os.Exit(0)
	}
	if first == "-iver" || first == "--version" {
		fmt.Print(version)
		os.Exit(0)
	}

	// Parse args with defaults
	opts := defaultOptions()

	// Load configuration file (unless -cfg- is in argv first)
	hasCfgOff := false
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, "-cfg-") {
			hasCfgOff = true
			break
		}
	}

	if !hasCfgOff {
		// Load config switches
		configSwitches := loadConfig()
		envSwitches := loadEnvSwitches()
		// Apply config first, then env (command line switches will override later)
		applyExtraSwitches(&opts, configSwitches)
		applyExtraSwitches(&opts, envSwitches)
	}

	var err error
	opts, err = parseArgsInto(opts, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", err)
		os.Exit(exitCmdLineError)
	}

	if opts.showVersion {
		fmt.Print(version)
		os.Exit(0)
	}

	if opts.command == "" {
		printUsage()
		os.Exit(0)
	}

	os.Exit(dispatch(opts))
}

// loadConfig reads the .rarrc configuration file.
func loadConfig() []string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".rarrc"),
		"/etc/rarrc",
	}
	if xdgHome := os.Getenv("XDG_CONFIG_HOME"); xdgHome != "" {
		candidates = append(candidates, filepath.Join(xdgHome, "rar", "rarrc"))
	} else if home != "" {
		candidates = append(candidates, filepath.Join(home, ".config", "rar", "rarrc"))
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parseConfigFile(string(data))
	}
	return nil
}

// parseConfigFile parses a RAR configuration file and returns switches.
func parseConfigFile(content string) []string {
	var switches []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "switches=") {
			val := line[len("switches="):]
			switches = append(switches, splitSwitches(val)...)
		}
	}
	return switches
}

// loadEnvSwitches reads the RARINISWITCHES environment variable.
func loadEnvSwitches() []string {
	val := os.Getenv("RARINISWITCHES")
	if val == "" {
		return nil
	}
	return splitSwitches(val)
}

// splitSwitches splits a string of switches into individual switch strings.
func splitSwitches(s string) []string {
	var result []string
	for _, part := range strings.Fields(s) {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// applyExtraSwitches applies a list of switch strings (with - prefix) to opts.
func applyExtraSwitches(opts *options, switches []string) {
	for _, sw := range switches {
		sw = strings.TrimPrefix(sw, "-")
		sw = strings.TrimPrefix(sw, "/")
		if sw != "" {
			parseSwitch(opts, sw) //nolint:errcheck
		}
	}
}

// parseArgsInto parses the command-line arguments after the program name into an existing options.
func parseArgsInto(opts options, args []string) (options, error) {
	stopSwitches := false
	i := 0

	if len(args) == 0 {
		return opts, nil
	}

	// First argument is the command (if not starting with -)
	first := args[0]
	if !strings.HasPrefix(first, "-") || stopSwitches {
		cmd := strings.ToLower(first)

		// Handle special 'i' command format: i[c|h|t][=<string>] or i<string>
		if strings.HasPrefix(cmd, "i") && !isSimpleCommand(cmd) {
			rest := cmd[1:] // everything after 'i'
			opts.command = "i"
			// Parse modifiers before '=' or end
			if idx := strings.IndexByte(rest, '='); idx >= 0 {
				opts.findFlags = rest[:idx]
				opts.findString = first[2+idx:] // preserve original case
			} else {
				// No '=', rest is either flags or the whole search string
				// Flags are a subset of {c, h, t}; check each character
				allFlags := true
				for _, ch := range rest {
					if ch != 'c' && ch != 'h' && ch != 't' {
						allFlags = false
						break
					}
				}
				if allFlags && rest != "" {
					opts.findFlags = rest
				} else {
					opts.findString = first[1:] // whole rest is search string
				}
			}
		} else {
			opts.command = cmd
		}
		i = 1
	}

	// Parse remaining arguments
	positional := 0 // 0=archive, 1+=files
	for i < len(args) {
		arg := args[i]
		i++

		if arg == "--" {
			stopSwitches = true
			continue
		}

		if !stopSwitches && strings.HasPrefix(arg, "-") && len(arg) > 1 {
			if err := parseSwitch(&opts, arg[1:]); err != nil {
				return opts, err
			}
			continue
		}

		if strings.HasPrefix(arg, "@") {
			// List file
			opts.listFile = append(opts.listFile, arg[1:])
			continue
		}

		// Positional argument
		if positional == 0 {
			opts.archivePath = ensureRarExtension(arg)
			positional++
		} else {
			opts.files = append(opts.files, arg)
		}
	}

	// Add files from list files
	for _, lf := range opts.listFile {
		names, err := readListFile(lf)
		if err != nil {
			return opts, fmt.Errorf("cannot read list file %s: %w", lf, err)
		}
		opts.files = append(opts.files, names...)
	}

	return opts, nil
}

// isSimpleCommand returns true if the command is a known simple (non-i-search) command.
func isSimpleCommand(cmd string) bool {
	switch cmd {
	case "a", "c", "ch", "cw", "d", "e", "f", "k",
		"l", "lt", "lta", "lb",
		"m", "mf", "p", "r", "rc", "rn", "rr", "rv",
		"s", "s-", "t", "u",
		"v", "vt", "vta", "vb", "x":
		return true
	}
	return false
}

// parseSwitch parses a single switch (without the leading '-').
func parseSwitch(opts *options, sw string) error {
	swLower := strings.ToLower(sw)

	switch {
	case swLower == "?" || swLower == "h":
		printHelp()
		os.Exit(0)

	case swLower == "iver":
		opts.showVersion = true

	case swLower == "cfg-":
		opts.noConfig = true

	case swLower == "y":
		opts.assumeYes = true

	case swLower == "r":
		opts.recursive = 1
	case swLower == "r-":
		opts.recursive = -1
		opts.noRecurse = true
	case swLower == "r0":
		opts.recursive = 2

	case swLower == "s" || swLower == "s+":
		opts.solid = true
	case swLower == "s-" || strings.HasPrefix(swLower, "s=-"):
		opts.solidOff = true
		opts.solid = false
	case strings.HasPrefix(swLower, "s="):
		opts.solid = true // simplified: just enable solid

	case swLower == "t":
		opts.test = true

	case swLower == "tk":
		opts.keepTime = true
	case swLower == "tl":
		opts.setLatestTime = true

	case strings.HasPrefix(sw, "m") || strings.HasPrefix(sw, "M"):
		lvl, err := parseCompLevel(sw)
		if err != nil {
			return err
		}
		opts.compLevel = lvl

	case swLower == "o" || swLower == "o-":
		// No or skip
		if swLower == "o-" {
			opts.overwrite = -1
		} else {
			opts.overwrite = 0
		}
	case swLower == "o+":
		opts.overwrite = 1

	case strings.HasPrefix(swLower, "p"):
		rest := sw[1:]
		if rest == "" {
			// Read password from stdin
			fmt.Print("Enter password: ")
			pwd, err := readLine(os.Stdin)
			if err != nil {
				return fmt.Errorf("cannot read password: %w", err)
			}
			opts.password = pwd
		} else {
			opts.password = rest
		}

	case strings.HasPrefix(swLower, "hp"):
		rest := sw[2:]
		if rest == "" {
			fmt.Print("Enter password: ")
			pwd, err := readLine(os.Stdin)
			if err != nil {
				return fmt.Errorf("cannot read password: %w", err)
			}
			opts.hdrPassword = pwd
		} else {
			opts.hdrPassword = rest
		}

	case strings.HasPrefix(swLower, "x"):
		rest := sw[1:]
		if rest == "" {
			opts.excludes = append(opts.excludes, "*")
		} else if strings.HasPrefix(rest, "@") {
			names, err := readListFile(rest[1:])
			if err != nil {
				return err
			}
			opts.excludes = append(opts.excludes, names...)
		} else {
			opts.excludes = append(opts.excludes, rest)
		}

	case strings.HasPrefix(swLower, "n"):
		rest := sw[1:]
		if rest == "" {
			break
		}
		if strings.HasPrefix(rest, "@") {
			names, err := readListFile(rest[1:])
			if err != nil {
				return err
			}
			opts.includes = append(opts.includes, names...)
		} else {
			opts.includes = append(opts.includes, rest)
		}

	case swLower == "ep":
		opts.epMode = 1
	case swLower == "ep1":
		opts.epMode = 2
	case swLower == "ep2":
		opts.epMode = 3
	case swLower == "ep3":
		opts.epMode = 4

	case strings.HasPrefix(swLower, "op"):
		opts.extractPath = sw[2:]

	case strings.HasPrefix(swLower, "w"):
		opts.workDir = sw[1:]

	case swLower == "c-":
		opts.noComment = true

	case swLower == "or":
		opts.autoRename = true

	case swLower == "ow":
		opts.saveOwner = true

	case swLower == "os":
		opts.saveStreams = true

	case swLower == "inul":
		opts.noMessages = true
		opts.noProgress = true

	case strings.HasPrefix(swLower, "id"):
		rest := strings.ToLower(sw[2:])
		if strings.Contains(rest, "p") {
			opts.noProgress = true
		}
		if strings.Contains(rest, "q") {
			opts.noMessages = true
		}

	case strings.HasPrefix(swLower, "sfx"):
		rest := sw[3:]
		opts.sfxModule = rest

	case strings.HasPrefix(swLower, "v") && len(sw) > 1 && (sw[1] >= '0' && sw[1] <= '9'):
		sz, err := parseSize(sw[1:])
		if err != nil {
			return fmt.Errorf("invalid volume size: %s", sw)
		}
		opts.volumeSize = append(opts.volumeSize, sz)

	case swLower == "v":
		// Volume autodetect or verbose list (handled by command)
		opts.verboseList = true

	case strings.HasPrefix(swLower, "vd"):
		// Erase disk - ignore

	case strings.HasPrefix(swLower, "rr"):
		rest := sw[2:]
		if rest == "" {
			opts.rrPct = 3 // default 3%
		} else {
			n, err := strconv.Atoi(strings.TrimRight(rest, "%"))
			if err == nil {
				opts.rrPct = n
			}
		}

	case strings.HasPrefix(swLower, "rv"):
		// Recovery volumes - not implemented
		_ = sw

	case strings.HasPrefix(swLower, "z"):
		opts.commentFile = sw[1:]

	case strings.HasPrefix(swLower, "sl"):
		sz, err := parseSize(sw[2:])
		if err != nil {
			return fmt.Errorf("invalid size: %s", sw)
		}
		opts.sizeLimit = sz

	case strings.HasPrefix(swLower, "sm"):
		sz, err := parseSize(sw[2:])
		if err != nil {
			return fmt.Errorf("invalid size: %s", sw)
		}
		opts.sizeMinimum = sz

	case swLower == "si" || strings.HasPrefix(swLower, "si"):
		opts.stdinRead = true

	case swLower == "so":
		opts.stdoutWrite = true

	case swLower == "u":
		opts.freshOnly = true

	case swLower == "f":
		// freshen - handled in command dispatch

	case swLower == "df":
		opts.deleteAfter = true

	case swLower == "dw":
		opts.wipeAfter = true

	case swLower == "ed":
		opts.noEmptyDirs = true

	case swLower == "kb":
		opts.keepBroken = true

	case swLower == "ao":
		opts.archiveAttr = true

	case swLower == "as":
		opts.syncContents = true

	case swLower == "cl":
		opts.toLower = true

	case swLower == "cu":
		opts.toUpper = true

	case strings.HasPrefix(swLower, "ap"):
		opts.pathPrefix = sw[2:]

	case strings.HasPrefix(swLower, "ta") || strings.HasPrefix(swLower, "tb") ||
		strings.HasPrefix(swLower, "tn") || strings.HasPrefix(swLower, "to") ||
		strings.HasPrefix(swLower, "ts"):
		// Time filters - accept but simplified

	case strings.HasPrefix(swLower, "ri"):
		// Priority - ignore

	case swLower == "k":
		opts.lock = true

	case swLower == "vp":
		// Pause before each volume - ignore

	case swLower == "ds":
		// Don't sort files in solid archive - ignore

	case strings.HasPrefix(swLower, "sc"):
		// Character set - ignore

	case swLower == "ms" || strings.HasPrefix(swLower, "ms"):
		// Store files by mask - ignore

	case strings.HasPrefix(swLower, "ma"):
		// Archive format version - ignore

	case strings.HasPrefix(swLower, "ac"):
		// Clear archive attribute - ignore

	case strings.HasPrefix(swLower, "ag"):
		// Generate archive name by mask - ignore

	case strings.HasPrefix(swLower, "ai"):
		// Ignore file attributes - ignore

	case strings.HasPrefix(swLower, "am"):
		// File exist message - ignore

	case strings.HasPrefix(swLower, "dh"):
		// Open shared files - ignore

	case strings.HasPrefix(swLower, "dp"):
		// Disable percent indicator - ignore

	case strings.HasPrefix(swLower, "dr"):
		// Delete to recycle bin - ignore

	case strings.HasPrefix(swLower, "e<"):
		// Set file attributes - ignore

	case strings.HasPrefix(swLower, "en"):
		// Don't add end-of-archive block - ignore

	case swLower == "ierr":
		opts.sendErrors = true

	case strings.HasPrefix(swLower, "ilog"):
		opts.logFile = sw[4:]

	case strings.HasPrefix(swLower, "isi"):
		// Stdin read - ignore

	case swLower == "mes":
		// Ignore encrypted file errors - ignore

	case strings.HasPrefix(swLower, "mc"):
		// Compression parameters - accept but simplified

	case strings.HasPrefix(swLower, "md"):
		// Dictionary size - accept but simplified

	case strings.HasPrefix(swLower, "me"):
		// Encryption method - accept but simplified

	case swLower == "mlp":
		// Large memory pages - ignore

	case strings.HasPrefix(swLower, "mt"):
		// Thread count - ignore

	case strings.HasPrefix(swLower, "ol"):
		// Save symbolic links - ignore

	case strings.HasPrefix(swLower, "om"):
		// Mark of the Web - ignore

	case swLower == "oni":
		// Allow incompatible names - ignore

	case strings.HasPrefix(swLower, "oi"):
		// Identical file detection - ignore

	case strings.HasPrefix(swLower, "qo"):
		// Quick open record - ignore

	case strings.HasPrefix(swLower, "ver"):
		// Version control - ignore

	case strings.HasPrefix(swLower, "oc"):
		// NTFS Compressed - ignore

	case strings.HasPrefix(swLower, "oh"):
		// Hard links - ignore

	case strings.HasPrefix(swLower, "ieml"):
		// Email - ignore

	case strings.HasPrefix(swLower, "ioff"):
		// Turn off computer - ignore

	case strings.HasPrefix(swLower, "isnd"):
		// Sound notifications - ignore

	case strings.HasPrefix(swLower, "log"):
		// Log - ignore

	case strings.HasPrefix(swLower, "ri"):
		// Priority - ignore

	default:
		// Unknown switch - warn but don't error
		if !opts.noMessages {
			fmt.Fprintf(os.Stderr, "Warning: unknown switch: -%s\n", sw)
		}
	}

	return nil
}

// parseCompLevel parses -m0...-m5 switch.
func parseCompLevel(sw string) (int, error) {
	rest := strings.ToLower(sw)
	if rest == "m" || rest == "m5" {
		return 5, nil
	}
	if len(rest) >= 2 && rest[0] == 'm' {
		n, err := strconv.Atoi(rest[1:])
		if err == nil && n >= 0 && n <= 5 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("invalid compression level: -%s", sw)
}

// parseSize parses a size string like "100k", "1m", "1.5g", etc.
func parseSize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSpace(s)
	lastChar := strings.ToLower(string(s[len(s)-1]))
	var multiplier float64 = 1 // default: no multiplication

	numStr := s
	switch lastChar {
	case "b":
		multiplier = 1
		numStr = s[:len(s)-1]
	case "k":
		multiplier = 1024
		numStr = s[:len(s)-1]
	case "m":
		multiplier = 1024 * 1024
		numStr = s[:len(s)-1]
	case "g":
		multiplier = 1024 * 1024 * 1024
		numStr = s[:len(s)-1]
	case "t":
		multiplier = 1024 * 1024 * 1024 * 1024
		numStr = s[:len(s)-1]
	default:
		// Plain number: use as-is (multiplier=1)
	}

	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", s)
	}
	return int64(f * multiplier), nil
}

// ensureRarExtension adds .rar extension if not present.
func ensureRarExtension(path string) string {
	if path == "" {
		return path
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".rar" || ext == ".exe" || ext == ".sfx" {
		return path
	}
	return path + ".rar"
}

// readListFile reads a list file and returns file names.
func readListFile(path string) ([]string, error) {
	var f *os.File
	var err error
	if path == "" {
		f = os.Stdin
	} else {
		f, err = os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
	}
	var names []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, ";") {
			names = append(names, line)
		}
	}
	return names, scanner.Err()
}

// readLine reads a line from a reader.
func readLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	return "", scanner.Err()
}

// dispatch dispatches to the appropriate command handler.
func dispatch(opts options) int {
	if opts.archivePath == "" && opts.command != "" &&
		opts.command != "-?" && opts.command != "?" {
		fmt.Fprintln(os.Stderr, "Archive name is required")
		return exitCmdLineError
	}

	switch opts.command {
	case "a":
		return cmdAdd(opts, false, false)
	case "u":
		return cmdAdd(opts, true, false)
	case "f":
		return cmdAdd(opts, false, true)
	case "m":
		return cmdMove(opts)
	case "mf":
		opts.move = true
		return cmdAdd(opts, false, true)
	case "d":
		return cmdDelete(opts)
	case "e":
		return cmdExtract(opts, false)
	case "x":
		return cmdExtract(opts, true)
	case "t":
		return cmdTest(opts)
	case "l", "lt", "lta", "lb":
		return cmdList(opts, false)
	case "v", "vt", "vta", "vb":
		return cmdList(opts, true)
	case "p":
		return cmdPrint(opts)
	case "c":
		return cmdComment(opts, false)
	case "cw":
		return cmdComment(opts, true)
	case "ch":
		return cmdChangeAttr(opts)
	case "k":
		return cmdLock(opts)
	case "rn":
		return cmdRename(opts)
	case "i":
		return cmdFind(opts)
	case "r":
		return cmdRepair(opts)
	case "rc":
		return cmdReconstruct(opts)
	case "rr":
		return cmdRecoveryRecord(opts)
	case "rv":
		return cmdRecoveryVolumes(opts)
	case "s", "s-":
		return cmdSFX(opts)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", opts.command)
		printUsage()
		return exitCmdLineError
	}
}

// ---- Commands ----

// cmdAdd implements the 'a', 'u', 'f' commands.
func cmdAdd(opts options, updateOnly bool, freshenOnly bool) int {
	if opts.archivePath == "" {
		fmt.Fprintln(os.Stderr, "Archive name is required")
		return exitCmdLineError
	}

	// Apply -u and -f switches
	if opts.freshOnly {
		updateOnly = true
	}

	// Collect files to add
	filesToAdd, err := collectFiles(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error collecting files: %v\n", err)
		return exitFatal
	}

	// Filter empty dirs if -ed
	if opts.noEmptyDirs {
		var filtered []fileInfo
		for _, f := range filesToAdd {
			if !f.isDir {
				filtered = append(filtered, f)
			}
		}
		filesToAdd = filtered
	}

	if len(filesToAdd) == 0 {
		if !opts.noMessages {
			fmt.Fprintln(os.Stderr, "No files to add")
		}
		return exitNoFiles
	}

	// Check if archive exists
	archiveExists := fileExists(opts.archivePath)

	// Read existing archive for update/freshen or sync
	var existingEntries []fileEntry
	var oldRR *rarReader
	if archiveExists {
		oldRR, existingEntries, err = readArchive(opts.archivePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading archive: %v\n", err)
			return exitFatal
		}
		defer oldRR.Close()

		if oldRR.info.IsLocked {
			fmt.Fprintln(os.Stderr, "Archive is locked")
			return exitLocked
		}
	}

	// Create writer
	w, err := newRarWriter(opts.archivePath, opts.compLevel, opts.solid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create archive: %v\n", err)
		return exitCreateError
	}
	if archiveExists && oldRR != nil {
		w.archFlags = oldRR.info.ArchFlags
	}

	if err := w.writeMainHeader(); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing header: %v\n", err)
		return exitWriteError
	}

	// If updating/freshening existing archive, first copy entries that won't be replaced
	// For -as (synchronize): only copy entries whose arcNames are also being added
	if archiveExists && oldRR != nil {
		// Build set of arcNames being added
		addSet := map[string]bool{}
		for _, fa := range filesToAdd {
			addSet[fa.arcName] = true
		}

		for _, fe := range existingEntries {
			if !addSet[fe.Name] {
				// When -as is set: skip entries not in the new file list (they get removed)
				if opts.syncContents {
					continue
				}
				data, err := readFileData(oldRR, &fe)
				if err != nil {
					if !opts.noMessages {
						fmt.Fprintf(os.Stderr, "Warning: cannot copy %s: %v\n", fe.Name, err)
					}
					continue
				}
				if err := w.AddFile(fe.Name, data, fe.Mtime, uint32(fe.Attributes), fe.IsDir); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing entry %s: %v\n", fe.Name, err)
				}
			}
		}
	}

	exitCode := exitSuccess
	addedCount := 0
	addedPaths := []string{}

	for _, fa := range filesToAdd {
		// For freshen: only update files that exist in archive and disk file is newer
		if freshenOnly && archiveExists {
			found := false
			for _, fe := range existingEntries {
				if fe.Name == fa.arcName {
					found = true
					if !fa.mtime.After(fe.Mtime) {
						goto skip
					}
					break
				}
			}
			if !found {
				goto skip
			}
		}

		// For update: only add if not in archive, or disk file is newer
		if updateOnly && archiveExists {
			for _, fe := range existingEntries {
				if fe.Name == fa.arcName {
					// Already in archive; only add if disk file is newer
					if !fa.mtime.After(fe.Mtime) {
						goto skip
					}
					break
				}
			}
		}

		{
			var data []byte
			var readErr error

			if !fa.isDir {
				data, readErr = os.ReadFile(fa.path)
				if readErr != nil {
					fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", fa.path, readErr)
					exitCode = exitWarning
					goto skip
				}

				// Apply size filters
				if opts.sizeLimit > 0 && int64(len(data)) >= opts.sizeLimit {
					goto skip
				}
				if opts.sizeMinimum > 0 && int64(len(data)) <= opts.sizeMinimum {
					goto skip
				}
			}

			// Apply case conversion
			arcName := fa.arcName
			if opts.toLower {
				arcName = strings.ToLower(arcName)
			} else if opts.toUpper {
				arcName = strings.ToUpper(arcName)
			}

			// Apply path prefix (-ap)
			if opts.pathPrefix != "" {
				arcName = filepath.ToSlash(filepath.Join(opts.pathPrefix, arcName))
			}

			if !opts.noMessages {
				if fa.isDir {
					fmt.Printf("Adding    %-40s (dir)\n", arcName)
				} else {
					fmt.Printf("Adding    %-40s  %6d%%\n", arcName, 0)
				}
			}

			if addErr := w.AddFile(arcName, data, fa.mtime, uint32(fa.attrs), fa.isDir); addErr != nil {
				fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", arcName, addErr)
				exitCode = exitWriteError
				goto skip
			}
			addedCount++
			addedPaths = append(addedPaths, fa.path)
		}
	skip:
	}

	if err := w.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error finalizing archive: %v\n", err)
		return exitWriteError
	}

	// Delete or wipe original files after archiving (-df, -dw)
	if (opts.deleteAfter || opts.wipeAfter || opts.move) && exitCode == exitSuccess {
		for _, path := range addedPaths {
			if opts.wipeAfter {
				// Overwrite file with zeros before deleting
				info, err := os.Stat(path)
				if err == nil && !info.IsDir() {
					f, err := os.OpenFile(path, os.O_WRONLY, 0)
					if err == nil {
						zeros := make([]byte, info.Size())
						f.Write(zeros)
						f.Close()
					}
				}
			}
			os.Remove(path)
		}
	}

	if addedCount == 0 && len(filesToAdd) > 0 && !freshenOnly && !updateOnly {
		if !opts.noMessages {
			fmt.Println("No files added")
		}
	}

	// Test archive if requested
	if opts.test && exitCode == exitSuccess {
		testOpts := opts
		testOpts.files = nil
		testCode := cmdTest(testOpts)
		if testCode != exitSuccess {
			return testCode
		}
	}

	return exitCode
}

// fileInfo holds info about a file to add.
type fileInfo struct {
	path    string
	arcName string
	mtime   time.Time
	attrs   uint32
	isDir   bool
}

// collectFiles gathers the list of files to add to archive.
func collectFiles(opts options) ([]fileInfo, error) {
	var result []fileInfo
	seen := map[string]bool{}

	baseFiles := opts.files
	if len(baseFiles) == 0 {
		// No files specified: add all files in current directory
		// Per man.txt: "If neither files nor listfiles are specified, *.* is implied"
		fis, err := expandPattern("*", opts)
		if err != nil {
			return nil, err
		}
		return fis, nil
	}

	for _, pattern := range baseFiles {
		fis, err := expandPattern(pattern, opts)
		if err != nil {
			if !opts.noMessages {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			}
			continue
		}
		for _, fi := range fis {
			if !seen[fi.arcName] {
				seen[fi.arcName] = true
				result = append(result, fi)
			}
		}
	}

	return result, nil
}

// expandPattern expands a file pattern (possibly with wildcards) to a list of files.
func expandPattern(pattern string, opts options) ([]fileInfo, error) {
	var result []fileInfo

	// Check if it's a directory (no wildcards)
	if !strings.ContainsAny(pattern, "*?") {
		info, err := os.Stat(pattern)
		if err != nil {
			return nil, fmt.Errorf("cannot find %s: %w", pattern, err)
		}

		if info.IsDir() {
			// If -r- is set, add directory entry only
			if opts.noRecurse {
				fi := fileInfo{
					path:    pattern,
					arcName: cleanArcName(pattern, opts),
					mtime:   info.ModTime(),
					attrs:   fileAttr(info),
					isDir:   true,
				}
				result = append(result, fi)
				return result, nil
			}
			// Recurse into directory (default for directories)
			return walkDir(pattern, pattern, opts)
		}

		// Single file
		fi := fileInfo{
			path:    pattern,
			arcName: cleanArcName(pattern, opts),
			mtime:   info.ModTime(),
			attrs:   fileAttr(info),
		}
		result = append(result, fi)
		return result, nil
	}

	// Wildcard pattern
	dir := filepath.Dir(pattern)
	base := filepath.Base(pattern)

	matches, err := filepath.Glob(filepath.Join(dir, base))
	if err != nil {
		return nil, err
	}

	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		if isExcluded(match, opts.excludes) {
			continue
		}
		if len(opts.includes) > 0 && !isIncluded(match, opts.includes) {
			continue
		}

		fi := fileInfo{
			path:    match,
			arcName: cleanArcName(match, opts),
			mtime:   info.ModTime(),
			attrs:   fileAttr(info),
			isDir:   info.IsDir(),
		}
		result = append(result, fi)

		// Recurse into directories
		if info.IsDir() && opts.recursive >= 1 {
			subs, err := walkDir(match, match, opts)
			if err == nil {
				result = append(result, subs...)
			}
		}
	}

	// If -r is set, also search subdirectories for the pattern
	if opts.recursive >= 1 {
		// Walk current directory looking for files matching 'base'
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if path == dir {
				return nil
			}
			if !info.IsDir() {
				matched, _ := filepath.Match(base, filepath.Base(path))
				if matched && !seen(path, result) {
					if !isExcluded(path, opts.excludes) {
						fi := fileInfo{
							path:    path,
							arcName: cleanArcName(path, opts),
							mtime:   info.ModTime(),
							attrs:   fileAttr(info),
						}
						result = append(result, fi)
					}
				}
			}
			return nil
		})
		_ = err
	}

	return result, nil
}

func seen(path string, list []fileInfo) bool {
	for _, fi := range list {
		if fi.path == path {
			return true
		}
	}
	return false
}

// walkDir recursively walks a directory and returns file infos.
func walkDir(root, baseDir string, opts options) ([]fileInfo, error) {
	var result []fileInfo

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if path == root && info.IsDir() {
			return nil
		}
		if isExcluded(path, opts.excludes) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(opts.includes) > 0 && !info.IsDir() && !isIncluded(path, opts.includes) {
			return nil
		}
		if opts.sizeLimit > 0 && !info.IsDir() && info.Size() >= opts.sizeLimit {
			return nil
		}
		if opts.sizeMinimum > 0 && !info.IsDir() && info.Size() <= opts.sizeMinimum {
			return nil
		}

		arcName := cleanArcName(path, opts)
		fi := fileInfo{
			path:    path,
			arcName: arcName,
			mtime:   info.ModTime(),
			attrs:   fileAttr(info),
			isDir:   info.IsDir(),
		}
		result = append(result, fi)
		return nil
	})
	return result, err
}

// cleanArcName converts a file path to an archive entry name.
func cleanArcName(path string, opts options) string {
	// Normalize separators
	name := filepath.ToSlash(path)

	switch opts.epMode {
	case 1: // -ep: no paths
		name = filepath.Base(name)
	case 2: // -ep1: remove current dir prefix
		cwd, _ := os.Getwd()
		cwdSlash := filepath.ToSlash(cwd) + "/"
		if strings.HasPrefix(name, cwdSlash) {
			name = name[len(cwdSlash):]
		} else if strings.HasPrefix(name, "./") {
			name = name[2:]
		}
	case 3: // -ep2: full path without drive letter
		if len(name) > 2 && name[1] == ':' {
			name = name[2:]
		}
		name = strings.TrimPrefix(name, "/")
	}

	// Remove leading ./
	name = strings.TrimPrefix(name, "./")

	return name
}

// fileAttr returns file attributes for archiving.
func fileAttr(info os.FileInfo) uint32 {
	if info.IsDir() {
		return 0x10 // directory
	}
	mode := info.Mode()
	return uint32(mode)
}

// isExcluded checks if a path matches any exclude pattern.
func isExcluded(path string, excludes []string) bool {
	name := filepath.Base(path)
	for _, exc := range excludes {
		matched, _ := filepath.Match(exc, name)
		if matched {
			return true
		}
		// Match full path
		matched2, _ := filepath.Match(exc, filepath.ToSlash(path))
		if matched2 {
			return true
		}
	}
	return false
}

// isIncluded checks if a path matches any include pattern.
func isIncluded(path string, includes []string) bool {
	name := filepath.Base(path)
	for _, inc := range includes {
		matched, _ := filepath.Match(inc, name)
		if matched {
			return true
		}
	}
	return false
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// cmdMove implements the 'm' command (move files to archive and delete originals).
func cmdMove(opts options) int {
	opts.move = true
	opts.deleteAfter = true
	return cmdAdd(opts, false, false)
}

// cmdDelete implements the 'd' command.
func cmdDelete(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading archive: %v\n", err)
		return exitBadArchive
	}

	if rr.info.IsLocked {
		fmt.Fprintln(os.Stderr, "Archive is locked")
		rr.Close()
		return exitLocked
	}

	// Determine which entries to keep
	var keep []fileEntry
	for _, fe := range entries {
		if !entryMatchesPatterns(&fe, opts.files) {
			keep = append(keep, fe)
		} else {
			if !opts.noMessages {
				fmt.Printf("Deleting  %s\n", fe.Name)
			}
		}
	}

	// Write new archive
	tmpPath := opts.archivePath + ".tmp"
	w, err := newRarWriter(opts.archivePath, opts.compLevel, false)
	if err != nil {
		rr.Close()
		fmt.Fprintf(os.Stderr, "Cannot create temp archive: %v\n", err)
		return exitCreateError
	}

	if err := w.writeMainHeader(); err != nil {
		rr.Close()
		return exitWriteError
	}

	for _, fe := range keep {
		fe2 := fe
		data, err := readFileData(rr, &fe2)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", fe.Name, err)
			continue
		}
		if err := w.AddFile(fe.Name, data, fe.Mtime, uint32(fe.Attributes), fe.IsDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", fe.Name, err)
		}
	}

	rr.Close()
	_ = tmpPath

	if err := w.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error finalizing archive: %v\n", err)
		return exitWriteError
	}

	return exitSuccess
}

// cmdExtract implements the 'e' and 'x' commands.
func cmdExtract(opts options, withPaths bool) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}
	defer rr.Close()

	// Determine extraction destination
	destDir := opts.extractPath
	if destDir == "" && len(opts.files) > 0 {
		// Check if last argument ends with path separator or is a directory
		last := opts.files[len(opts.files)-1]
		if strings.HasSuffix(last, "/") || strings.HasSuffix(last, string(os.PathSeparator)) {
			destDir = strings.TrimRight(last, "/"+string(os.PathSeparator))
			opts.files = opts.files[:len(opts.files)-1]
		}
	}
	if destDir == "" {
		destDir = "."
	}

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create directory %s: %v\n", destDir, err)
		return exitCreateError
	}

	exitCode := exitSuccess
	extractedCount := 0

	for _, fe := range entries {
		if !entryMatchesPatterns(&fe, opts.files) {
			continue
		}

		if fe.IsDir {
			if withPaths {
				dirPath := filepath.Join(destDir, fe.Name)
				os.MkdirAll(dirPath, 0755)
			}
			continue
		}

		// Determine output path
		var outPath string
		if withPaths {
			// Extract with full paths
			name := filepath.FromSlash(fe.Name)
			// Remove absolute path markers
			name = normalizeExtractPath(name)
			outPath = filepath.Join(destDir, name)
		} else {
			// Extract without paths (just filename)
			outPath = filepath.Join(destDir, filepath.Base(filepath.FromSlash(fe.Name)))
		}

		// Handle -or (auto-rename)
		if opts.autoRename {
			outPath = autoRenameIfExists(outPath)
		}

		// Handle overwrite mode
		if fileExists(outPath) && !opts.autoRename {
			switch opts.overwrite {
			case -1: // skip
				if !opts.noMessages {
					fmt.Printf("Skipping  %s\n", fe.Name)
				}
				continue
			case 0: // ask
				if !opts.assumeYes {
					fmt.Printf("\n%s already exists. Overwrite it? [Y/n]: ", outPath)
					answer, _ := readLine(os.Stdin)
					if strings.ToLower(strings.TrimSpace(answer)) == "n" {
						continue
					}
				}
			case 1: // always overwrite
				// fall through
			}
		}

		// Read file data
		data, err := readFileData(rr, &fe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error extracting %s: %v\n", fe.Name, err)
			if !strings.Contains(err.Error(), "not supported") {
				exitCode = exitFatal
			} else {
				exitCode = exitWarning
			}
			continue
		}

		// Verify CRC32
		if fe.HasCRC32 {
			gotCRC := computeCRC32(data)
			if gotCRC != fe.DataCRC32 {
				fmt.Fprintf(os.Stderr, "CRC error in %s\n", fe.Name)
				exitCode = exitCRCError
				continue
			}
		}

		if !opts.noMessages {
			fmt.Printf("Extracting  %-40s  OK\n", fe.Name)
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Cannot create directory for %s: %v\n", fe.Name, err)
			exitCode = exitCreateError
			continue
		}

		// Write file
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Cannot write %s: %v\n", outPath, err)
			exitCode = exitWriteError
			continue
		}

		// Restore modification time
		if !fe.Mtime.IsZero() {
			os.Chtimes(outPath, fe.Mtime, fe.Mtime)
		}

		extractedCount++
	}

	if extractedCount == 0 && len(opts.files) > 0 {
		exitCode = exitNoFiles
	}

	return exitCode
}

// autoRenameIfExists generates a unique filename if the target exists.
func autoRenameIfExists(path string) string {
	if !fileExists(path) {
		return path
	}
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	for i := 1; i < 10000; i++ {
		newPath := fmt.Sprintf("%s(%d)%s", base, i, ext)
		if !fileExists(newPath) {
			return newPath
		}
	}
	return path
}

// cmdTest implements the 't' command.
func cmdTest(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}
	defer rr.Close()

	exitCode := exitSuccess
	okCount := 0
	errCount := 0

	for _, fe := range entries {
		if !entryMatchesPatterns(&fe, opts.files) {
			continue
		}
		if fe.IsDir {
			continue
		}

		data, err := readFileData(rr, &fe)
		if err != nil {
			if !opts.noMessages {
				fmt.Printf("Testing   %-40s  ", fe.Name)
				fmt.Printf("FAILED: %v\n", err)
			}
			errCount++
			exitCode = exitFatal
			continue
		}

		if fe.HasCRC32 {
			gotCRC := computeCRC32(data)
			if gotCRC != fe.DataCRC32 {
				if !opts.noMessages {
					fmt.Printf("Testing   %-40s  CRC FAILED\n", fe.Name)
				}
				errCount++
				exitCode = exitCRCError
				continue
			}
		}

		if !opts.noMessages {
			fmt.Printf("Testing   %-40s  OK\n", fe.Name)
		}
		okCount++
	}

	if !opts.noMessages {
		if errCount > 0 {
			fmt.Printf("\nErrors found: %d\n", errCount)
		} else {
			fmt.Printf("\nAll OK\n")
		}
	}

	return exitCode
}

// cmdList implements the 'l', 'v', 'lb', 'vb', etc. commands.
func cmdList(opts options, verbose bool) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}
	defer rr.Close()

	// Determine list mode
	isBare := strings.HasSuffix(opts.command, "b")
	isVerbose := verbose || strings.HasPrefix(opts.command, "v")
	showTech := strings.Contains(opts.command, "t")

	if isBare {
		// Bare listing: just filenames
		for _, fe := range entries {
			if !entryMatchesPatterns(&fe, opts.files) {
				continue
			}
			fmt.Println(fe.Name)
		}
		return exitSuccess
	}

	// Header
	printBanner()
	fmt.Printf("\nArchive: %s\n", opts.archivePath)

	archFmt := "RAR 5"
	if rr.info.ArchFlags&archFlagSolid != 0 {
		archFmt += " solid"
	}
	if rr.info.IsLocked {
		archFmt += " locked"
	}
	fmt.Printf("Details: %s\n", archFmt)

	if rr.info.HasComment && rr.info.Comment != "" {
		fmt.Printf("\nArchive comment:\n%s\n", rr.info.Comment)
	}
	fmt.Println()

	var totalPacked, totalUnpacked uint64
	count := 0

	if showTech {
		// Technical (lt/vt) mode: multiline detailed info per file
		for _, fe := range entries {
			if !entryMatchesPatterns(&fe, opts.files) {
				continue
			}
			attrStr := formatAttrs(fe.Attributes, fe.IsDir, fe.HostOS)
			timeStr := formatTime(fe.Mtime)
			fmt.Printf("  Name: %s\n", fe.Name)
			fmt.Printf("  Type: %s\n", fileTypeName(fe.IsDir))
			fmt.Printf("  Size: %d\n", fe.UnpackedSize)
			fmt.Printf(" Ratio: %s\n", formatRatio(fe.PackedSize, fe.UnpackedSize))
			fmt.Printf(" Mtime: %s\n", timeStr)
			fmt.Printf(" Attr:  %s\n", attrStr)
			fmt.Printf("    OS: %s\n", hostOSName(fe.HostOS))
			if fe.HasCRC32 {
				fmt.Printf("   CRC: %08X\n", fe.DataCRC32)
			}
			fmt.Printf("  Pack: method=%d ver=%d\n", compMethod(fe.CompInfo), compVersion(fe.CompInfo))
			fmt.Println()
			totalUnpacked += fe.UnpackedSize
			totalPacked += fe.PackedSize
			count++
		}
		fmt.Printf("Total %d file(s), %d bytes\n", count, totalUnpacked)
	} else if isVerbose {
		fmt.Println(" Attributes        Size   Packed   Ratio  Date      Time  Name")
		fmt.Println("----------- ---------- --------   -----  ---------- -----  ----")

		for _, fe := range entries {
			if !entryMatchesPatterns(&fe, opts.files) {
				continue
			}

			attrStr := formatAttrs(fe.Attributes, fe.IsDir, fe.HostOS)
			timeStr := formatTime(fe.Mtime)

			fmt.Printf("%-11s %10d %8d   %s  %s  %s\n",
				attrStr, fe.UnpackedSize, fe.PackedSize, formatRatio(fe.PackedSize, fe.UnpackedSize), timeStr, fe.Name)

			totalUnpacked += fe.UnpackedSize
			totalPacked += fe.PackedSize
			count++
		}

		fmt.Println("----------- ---------- --------   -----  ---------- -----  ----")
		fmt.Printf("            %10d %8d   %s                   %d\n",
			totalUnpacked, totalPacked, formatRatio(totalPacked, totalUnpacked), count)
	} else {
		fmt.Println(" Attributes       Size     Date    Time   Name")
		fmt.Println("----------- ----------  ---------- -----  ----")

		for _, fe := range entries {
			if !entryMatchesPatterns(&fe, opts.files) {
				continue
			}

			attrStr := formatAttrs(fe.Attributes, fe.IsDir, fe.HostOS)
			timeStr := formatTime(fe.Mtime)

			fmt.Printf("%-11s %10d  %s  %s\n",
				attrStr, fe.UnpackedSize, timeStr, fe.Name)

			totalUnpacked += fe.UnpackedSize
			totalPacked += fe.PackedSize
			count++
		}

		fmt.Println("----------- ----------  ---------- -----  ----")
		fmt.Printf("            %10d                    %d\n", totalUnpacked, count)
	}

	return exitSuccess
}

// cmdPrint implements the 'p' command - print file contents to stdout.
func cmdPrint(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}
	defer rr.Close()

	for _, fe := range entries {
		if !entryMatchesPatterns(&fe, opts.files) {
			continue
		}
		if fe.IsDir {
			continue
		}

		data, err := readFileData(rr, &fe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", fe.Name, err)
			return exitFatal
		}

		os.Stdout.Write(data)
	}
	return exitSuccess
}

// cmdComment implements 'c' and 'cw' commands.
func cmdComment(opts options, writeToFile bool) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}

	if rr.info.IsLocked {
		fmt.Fprintln(os.Stderr, "Archive is locked")
		rr.Close()
		return exitLocked
	}

	if writeToFile {
		// Write existing comment to file
		if opts.files != nil && len(opts.files) > 0 {
			if err := os.WriteFile(opts.files[0], []byte(rr.info.Comment), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Cannot write comment: %v\n", err)
				rr.Close()
				return exitWriteError
			}
		}
		rr.Close()
		return exitSuccess
	}

	// Read comment from file or stdin
	var comment string
	if opts.commentFile != "" {
		data, err := os.ReadFile(opts.commentFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot read comment file: %v\n", err)
			rr.Close()
			return exitReadError
		}
		comment = string(data)
	} else {
		fmt.Print("Enter comment (Ctrl+D/Ctrl+Z to end):\n")
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			rr.Close()
			return exitReadError
		}
		comment = string(data)
	}

	// Rewrite archive with comment
	w, err := newRarWriter(opts.archivePath, opts.compLevel, rr.info.ArchFlags&archFlagSolid != 0)
	if err != nil {
		rr.Close()
		return exitCreateError
	}
	w.archFlags = rr.info.ArchFlags
	w.comment = comment

	if err := w.writeMainHeader(); err != nil {
		rr.Close()
		return exitWriteError
	}

	for _, fe := range entries {
		fe2 := fe
		data, err := readFileData(rr, &fe2)
		if err != nil {
			continue
		}
		w.AddFile(fe.Name, data, fe.Mtime, uint32(fe.Attributes), fe.IsDir)
	}

	rr.Close()

	if err := w.Close(); err != nil {
		return exitWriteError
	}

	return exitSuccess
}

// cmdChangeAttr implements 'ch' command (change archive parameters).
func cmdChangeAttr(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}

	if rr.info.IsLocked {
		fmt.Fprintln(os.Stderr, "Archive is locked")
		rr.Close()
		return exitLocked
	}

	// Apply requested attribute changes
	newFlags := rr.info.ArchFlags
	if opts.solid {
		newFlags |= archFlagSolid
	} else if opts.solidOff {
		newFlags &^= archFlagSolid
	}

	w, err := newRarWriter(opts.archivePath, opts.compLevel, newFlags&archFlagSolid != 0)
	if err != nil {
		rr.Close()
		return exitCreateError
	}
	w.archFlags = newFlags
	// Carry over comment unless -c- specified
	if !opts.noComment {
		w.comment = rr.info.Comment
	}
	// Override comment if -z<file> specified
	if opts.commentFile != "" {
		data, err := os.ReadFile(opts.commentFile)
		if err == nil {
			w.comment = string(data)
		}
	}

	if err := w.writeMainHeader(); err != nil {
		rr.Close()
		return exitWriteError
	}

	for _, fe := range entries {
		fe2 := fe
		data, err := readFileData(rr, &fe2)
		if err != nil {
			continue
		}
		w.AddFile(fe.Name, data, fe.Mtime, uint32(fe.Attributes), fe.IsDir)
	}

	rr.Close()

	if err := w.Close(); err != nil {
		return exitWriteError
	}

	return exitSuccess
}

// cmdLock implements the 'k' command.
func cmdLock(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}

	if rr.info.IsLocked {
		fmt.Println("Archive is already locked")
		rr.Close()
		return exitSuccess
	}

	w, err := newRarWriter(opts.archivePath, opts.compLevel, rr.info.ArchFlags&archFlagSolid != 0)
	if err != nil {
		rr.Close()
		return exitCreateError
	}
	w.archFlags = rr.info.ArchFlags | archFlagLock

	if err := w.writeMainHeader(); err != nil {
		rr.Close()
		return exitWriteError
	}

	for _, fe := range entries {
		fe2 := fe
		data, err := readFileData(rr, &fe2)
		if err != nil {
			continue
		}
		w.AddFile(fe.Name, data, fe.Mtime, uint32(fe.Attributes), fe.IsDir)
	}

	rr.Close()

	if err := w.Close(); err != nil {
		return exitWriteError
	}

	if !opts.noMessages {
		fmt.Println("Archive locked")
	}
	return exitSuccess
}

// cmdRename implements the 'rn' command.
func cmdRename(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	// files should be pairs of old/new names
	if len(opts.files)%2 != 0 {
		fmt.Fprintln(os.Stderr, "rn command requires pairs of old/new names")
		return exitCmdLineError
	}

	renames := map[string]string{}
	for i := 0; i < len(opts.files); i += 2 {
		renames[opts.files[i]] = opts.files[i+1]
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}

	if rr.info.IsLocked {
		fmt.Fprintln(os.Stderr, "Archive is locked")
		rr.Close()
		return exitLocked
	}

	w, err := newRarWriter(opts.archivePath, opts.compLevel, rr.info.ArchFlags&archFlagSolid != 0)
	if err != nil {
		rr.Close()
		return exitCreateError
	}
	w.archFlags = rr.info.ArchFlags

	if err := w.writeMainHeader(); err != nil {
		rr.Close()
		return exitWriteError
	}

	for _, fe := range entries {
		fe2 := fe
		newName, ok := renames[fe.Name]
		if ok {
			fe2.Name = newName
			if !opts.noMessages {
				fmt.Printf("Renaming %s -> %s\n", fe.Name, newName)
			}
		}
		data, err := readFileData(rr, &fe)
		if err != nil {
			continue
		}
		w.AddFile(fe2.Name, data, fe2.Mtime, uint32(fe2.Attributes), fe2.IsDir)
	}

	rr.Close()

	if err := w.Close(); err != nil {
		return exitWriteError
	}

	return exitSuccess
}

// cmdFind implements the 'i' command (find string in archive).
func cmdFind(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}

	// Parse search string from command or files
	searchStr := opts.findString
	if searchStr == "" && len(opts.files) > 0 {
		searchStr = opts.files[0]
	}
	if searchStr == "" {
		fmt.Fprintln(os.Stderr, "No search string specified")
		return exitCmdLineError
	}

	// Determine search flags from findFlags field
	caseSensitive := strings.Contains(opts.findFlags, "c")
	hexMode := strings.Contains(opts.findFlags, "h")

	var searchBytes []byte
	if hexMode {
		// Parse hex string
		hexStr := strings.ReplaceAll(searchStr, " ", "")
		for i := 0; i < len(hexStr)-1; i += 2 {
			b, err := strconv.ParseUint(hexStr[i:i+2], 16, 8)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid hex string: %s\n", hexStr[i:i+2])
				return exitCmdLineError
			}
			searchBytes = append(searchBytes, byte(b))
		}
	} else {
		searchBytes = []byte(searchStr)
	}

	rr, entries, err := readArchive(opts.archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open archive: %v\n", err)
		return exitBadArchive
	}
	defer rr.Close()

	foundCount := 0

	for _, fe := range entries {
		if fe.IsDir {
			continue
		}

		data, err := readFileData(rr, &fe)
		if err != nil {
			continue
		}

		var found bool
		if caseSensitive || hexMode {
			found = bytes.Contains(data, searchBytes)
		} else {
			found = bytes.Contains(bytes.ToLower(data), bytes.ToLower(searchBytes))
		}

		if found {
			fmt.Printf("%s : %s\n", opts.archivePath, fe.Name)
			foundCount++
		}
	}

	if foundCount == 0 {
		return exitNoFiles
	}
	return exitSuccess
}

// cmdRepair implements the 'r' command.
func cmdRepair(opts options) int {
	fmt.Fprintln(os.Stderr, "Repair: not implemented")
	return exitFatal
}

// cmdReconstruct implements the 'rc' command.
func cmdReconstruct(opts options) int {
	fmt.Fprintln(os.Stderr, "Reconstruct volumes: not implemented")
	return exitFatal
}

// cmdRecoveryRecord implements the 'rr[N]' command.
func cmdRecoveryRecord(opts options) int {
	if opts.archivePath == "" {
		return exitCmdLineError
	}
	if !opts.noMessages {
		fmt.Printf("Recovery record: not implemented for %s\n", opts.archivePath)
	}
	return exitSuccess
}

// cmdRecoveryVolumes implements the 'rv[N]' command.
func cmdRecoveryVolumes(opts options) int {
	fmt.Fprintln(os.Stderr, "Recovery volumes: not implemented")
	return exitFatal
}

// cmdSFX implements the 's' and 's-' commands.
func cmdSFX(opts options) int {
	fmt.Fprintln(os.Stderr, "SFX conversion: not implemented")
	return exitFatal
}

// ---- Output formatting helpers ----

func printBanner() {
	fmt.Printf("\nRAR 1.0   Copyright (c) 2026 RAR Implementation\n")
	fmt.Printf("Implementation based on RAR 5.0 format\n")
}

func formatAttrs(attrs uint64, isDir bool, hostOS uint64) string {
	if hostOS == hostOSWindows {
		// Windows attributes
		result := "..A...."
		if isDir {
			result = "..Ax..."
		}
		if attrs&0x01 != 0 { // read-only
			result = strings.Replace(result, "A", "R", 1)
		}
		if attrs&0x20 != 0 { // archive
			// already A
		}
		if isDir {
			return "     D  "
		}
		return result
	}

	// Unix attributes
	mode := attrs
	result := make([]byte, 10)
	result[0] = '-'
	if isDir {
		result[0] = 'd'
	}
	// owner
	if mode&0400 != 0 {
		result[1] = 'r'
	} else {
		result[1] = '-'
	}
	if mode&0200 != 0 {
		result[2] = 'w'
	} else {
		result[2] = '-'
	}
	if mode&0100 != 0 {
		result[3] = 'x'
	} else {
		result[3] = '-'
	}
	// group
	if mode&040 != 0 {
		result[4] = 'r'
	} else {
		result[4] = '-'
	}
	if mode&020 != 0 {
		result[5] = 'w'
	} else {
		result[5] = '-'
	}
	if mode&010 != 0 {
		result[6] = 'x'
	} else {
		result[6] = '-'
	}
	// other
	if mode&04 != 0 {
		result[7] = 'r'
	} else {
		result[7] = '-'
	}
	if mode&02 != 0 {
		result[8] = 'w'
	} else {
		result[8] = '-'
	}
	if mode&01 != 0 {
		result[9] = 'x'
	} else {
		result[9] = '-'
	}

	return string(result)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "                 "
	}
	return t.Local().Format("2006-01-02 15:04")
}

func formatRatio(packed, unpacked uint64) string {
	if unpacked == 0 {
		return "  0%"
	}
	return fmt.Sprintf("%3d%%", 100*packed/unpacked)
}

func fileTypeName(isDir bool) string {
	if isDir {
		return "directory"
	}
	return "file"
}

func hostOSName(os uint64) string {
	switch os {
	case 0:
		return "Windows"
	case 1:
		return "Unix"
	default:
		return fmt.Sprintf("Unknown(%d)", os)
	}
}

// ---- Help and usage ----

func printUsage() {
	fmt.Print(`
Usage:     rar <command> [-<switch 1> -<switch N>] <archive> [<@listfiles...>]
           [<files...>] [<path_to_extract\>]

Commands:
  a             Add files to archive
  c             Add archive comment
  ch            Change archive parameters
  cw            Write archive comment to file
  d             Delete files from archive
  e             Extract files without archived paths
  f             Freshen files in archive
  i[par]=<str>  Find string in archives
  k             Lock archive
  l[t[a],b]     List archive contents [technical[all],bare]
  m[f]          Move to archive [files only]
  p             Print file to stdout
  r             Repair archive
  rc            Reconstruct missing volumes
  rn            Rename archived files
  rr[N]         Add data recovery record
  rv[N]         Create recovery volumes
  s[name|-]     Convert archive to or from SFX
  t             Test archive files
  u             Update files in archive
  v[t[a],b]     Verbosely list archive contents [technical[all],bare]
  x             Extract files with full path

Switches:
  -             Stop switches scanning
  -cfg-         Disable read of configuration
  -m<n>         Set compression level (0-store...3-default...5-best)
  -o[+|-]       Set the overwrite mode
  -op<path>     Set the output path for extracted files
  -or           Rename files automatically
  -ow           Save or restore file owner and group
  -p[pwd]       Set password
  -r[-|0]       Recurse subdirectories
  -rr[N]        Add data recovery record
  -rv[N]        Create recovery volumes
  -s[=<par>]    Create solid archive
  -t            Test files after archiving
  -ta[f]<date>  Process files modified after specified date
  -tb[f]<date>  Process files modified before specified date
  -tk           Keep original archive time
  -tl           Set archive time to newest file
  -tn[m,c,a,o]<time>  Process files newer than specified time
  -to[m,c,a,o]<time>  Process files older than specified time
  -ts[m,c,a,p][+,-,1]  Save or restore file time
  -u            Update files
  -v<size>[k,m,g]  Create volumes of specified size
  -vd           Erase disk before creating volume
  -ver[n]       File version control
  -vp           Pause before each volume
  -w<p>         Assign work directory
  -x[f]         Exclude specified file or directory
  -y            Assume Yes on all queries
  -z[f]         Read archive comment from file
`)
}

func printHelp() {
	printBanner()
	fmt.Println()
	printUsage()
}
