package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fileEntry represents a file entry in a RAR archive (for listing/processing).
type fileEntry struct {
	Name        string
	PackedSize  uint64
	UnpackedSize uint64
	Attributes  uint64
	Mtime       time.Time
	DataCRC32   uint32
	HasCRC32    bool
	CompInfo    uint64
	HostOS      uint64
	IsDir       bool
	FileFlags   uint64
	// position in file
	DataOffset  int64 // file position where data starts
	HeaderOffset int64 // file position of the header start
}

// archiveInfo contains top-level info about an archive
type archiveInfo struct {
	ArchFlags   uint64
	HasComment  bool
	Comment     string
	IsLocked    bool
}

// compMethod extracts compression method from compression info vint
func compMethod(compInfo uint64) int {
	return int((compInfo >> 7) & 0x7)
}

// compVersion extracts algorithm version from compression info
func compVersion(compInfo uint64) int {
	return int(compInfo & 0x3f)
}

// ---- Archive Reader ----

type rarReader struct {
	f      *os.File
	size   int64
	info   archiveInfo
	offset int64
}

func openRarReader(path string) (*rarReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	rr := &rarReader{f: f, size: stat.Size()}
	if err := rr.readSignature(); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rr, nil
}

func (rr *rarReader) Close() { rr.f.Close() }

func (rr *rarReader) readSignature() error {
	sig := make([]byte, 8)
	if _, err := io.ReadFull(rr.f, sig); err != nil {
		return fmt.Errorf("cannot read signature: %w", err)
	}
	if !bytes.Equal(sig, rar5Signature) {
		return fmt.Errorf("not a RAR archive or unsupported format")
	}
	rr.offset = 8
	return nil
}

// blockHeader represents a parsed RAR 5.0 block header (short block).
type blockHeader struct {
	CRC32       uint32
	HeaderSize  uint64
	HeaderType  uint64
	Flags       uint64
	ExtraSize   uint64
	DataSize    uint64
	// raw body bytes (from type onwards), not including CRC32 and header_size
	Body []byte
	// extra area bytes
	Extra []byte
	// position of block header in file
	StartOffset int64
	// position right after the header (before data area)
	DataOffset int64
}

// readBlockHeader reads the next block header from the current position.
func (rr *rarReader) readBlockHeader() (*blockHeader, error) {
	startOffset := rr.offset
	bh := &blockHeader{StartOffset: startOffset}

	// Read CRC32
	crc32val, err := readUint32LE(rr.f)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("reading block CRC32: %w", err)
	}
	bh.CRC32 = crc32val
	rr.offset += 4

	// Read header size (vint)
	headerSize, n, err := readVint(rr.f)
	if err != nil {
		return nil, fmt.Errorf("reading header size: %w", err)
	}
	bh.HeaderSize = headerSize
	rr.offset += int64(n)
	headerSizeBytes := encodeVint(headerSize)

	if headerSize == 0 || headerSize > 2*1024*1024 {
		return nil, fmt.Errorf("invalid header size: %d", headerSize)
	}

	// Read header body
	body := make([]byte, headerSize)
	if _, err := io.ReadFull(rr.f, body); err != nil {
		return nil, fmt.Errorf("reading header body: %w", err)
	}
	rr.offset += int64(headerSize)

	// Verify CRC32
	crcData := append(headerSizeBytes, body...)
	expectedCRC := computeCRC32(crcData)
	if expectedCRC != crc32val {
		return nil, fmt.Errorf("header CRC32 mismatch: got %08x, expected %08x", crc32val, expectedCRC)
	}

	// Parse body
	pos := 0
	headerType, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading header type: %w", err)
	}
	bh.HeaderType = headerType
	pos += n

	flags, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading header flags: %w", err)
	}
	bh.Flags = flags
	pos += n

	if flags&hflExtra != 0 {
		extraSize, n, err := readVintFromBytes(body[pos:])
		if err != nil {
			return nil, fmt.Errorf("reading extra size: %w", err)
		}
		bh.ExtraSize = extraSize
		pos += n
	}

	if flags&hflData != 0 {
		dataSize, n, err := readVintFromBytes(body[pos:])
		if err != nil {
			return nil, fmt.Errorf("reading data size: %w", err)
		}
		bh.DataSize = dataSize
		pos += n
	}

	bh.Body = body[pos : uint64(pos)+headerSize-uint64(pos)-bh.ExtraSize]
	if bh.ExtraSize > 0 {
		extraStart := uint64(len(body)) - bh.ExtraSize
		if extraStart > uint64(len(body)) {
			return nil, fmt.Errorf("invalid extra area size")
		}
		bh.Extra = body[extraStart:]
	}

	bh.DataOffset = rr.offset
	return bh, nil
}

// skipData skips over the data area of a block.
func (rr *rarReader) skipData(bh *blockHeader) error {
	if bh.DataSize == 0 {
		return nil
	}
	_, err := rr.f.Seek(int64(bh.DataSize), io.SeekCurrent)
	if err != nil {
		return err
	}
	rr.offset += int64(bh.DataSize)
	return nil
}

// parseFileHeader parses a file header block body into a fileEntry.
func parseFileHeader(bh *blockHeader) (*fileEntry, error) {
	fe := &fileEntry{}
	body := bh.Body
	pos := 0

	// File flags
	fileFlags, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading file flags: %w", err)
	}
	fe.FileFlags = fileFlags
	fe.IsDir = fileFlags&fileFlagDirectory != 0
	pos += n

	// Unpacked size
	unpSize, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading unpacked size: %w", err)
	}
	fe.UnpackedSize = unpSize
	pos += n

	// Attributes
	attrs, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading attributes: %w", err)
	}
	fe.Attributes = attrs
	pos += n

	// Optional mtime (uint32 Unix time) if file flag 0x0002 set
	if fileFlags&fileFlagUtime != 0 {
		if pos+4 > len(body) {
			return nil, fmt.Errorf("insufficient data for mtime")
		}
		mtime32 := uint32(body[pos]) | uint32(body[pos+1])<<8 | uint32(body[pos+2])<<16 | uint32(body[pos+3])<<24
		fe.Mtime = time.Unix(int64(mtime32), 0)
		pos += 4
	}

	// Optional CRC32 if file flag 0x0004 set
	if fileFlags&fileFlagCRC32 != 0 {
		if pos+4 > len(body) {
			return nil, fmt.Errorf("insufficient data for CRC32")
		}
		fe.DataCRC32 = uint32(body[pos]) | uint32(body[pos+1])<<8 | uint32(body[pos+2])<<16 | uint32(body[pos+3])<<24
		fe.HasCRC32 = true
		pos += 4
	}

	// Compression info
	compInfo, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading compression info: %w", err)
	}
	fe.CompInfo = compInfo
	pos += n

	// Host OS
	hostOS, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading host OS: %w", err)
	}
	fe.HostOS = hostOS
	pos += n

	// Name length
	nameLen, n, err := readVintFromBytes(body[pos:])
	if err != nil {
		return nil, fmt.Errorf("reading name length: %w", err)
	}
	pos += n

	if pos+int(nameLen) > len(body) {
		return nil, fmt.Errorf("name exceeds body size")
	}
	fe.Name = string(body[pos : pos+int(nameLen)])
	pos += int(nameLen)

	fe.PackedSize = bh.DataSize
	fe.DataOffset = bh.DataOffset

	// Parse extra area for high-precision time
	if len(bh.Extra) > 0 {
		parseFileExtra(bh.Extra, fe)
	}

	return fe, nil
}

// parseFileExtra parses the extra area of a file header.
func parseFileExtra(extra []byte, fe *fileEntry) {
	pos := 0
	for pos < len(extra) {
		// Record size
		recSize, n, err := readVintFromBytes(extra[pos:])
		if err != nil {
			return
		}
		pos += n
		if pos+int(recSize) > len(extra) {
			return
		}
		recData := extra[pos : pos+int(recSize)]
		pos += int(recSize)

		if len(recData) == 0 {
			continue
		}

		// Record type
		recType, n, err := readVintFromBytes(recData)
		if err != nil {
			continue
		}
		recData = recData[n:]

		switch recType {
		case fhExtraHTime:
			parseHTimeRecord(recData, fe)
		}
	}
}

// serviceHeader represents a parsed service header.
type serviceHeader struct {
	name string
}

// parseServiceHeader parses a service header block to extract the service name.
func parseServiceHeader(bh *blockHeader) *serviceHeader {
	if len(bh.Body) == 0 {
		return nil
	}
	data := bh.Body

	// Service header has same fields as file header:
	// fileFlags, unpackedSize, attributes, dataCRC32, compInfo, hostOS, nameLength, name, ...
	pos := 0

	// fileFlags (vint)
	_, n, err := readVintFromBytes(data[pos:])
	if err != nil {
		return nil
	}
	pos += n

	// unpackedSize (vint)
	_, n, err = readVintFromBytes(data[pos:])
	if err != nil {
		return nil
	}
	pos += n

	// attributes (vint)
	_, n, err = readVintFromBytes(data[pos:])
	if err != nil {
		return nil
	}
	pos += n

	// dataCRC32 (4 bytes)
	if pos+4 > len(data) {
		return nil
	}
	pos += 4

	// compInfo (vint)
	_, n, err = readVintFromBytes(data[pos:])
	if err != nil {
		return nil
	}
	pos += n

	// hostOS (vint)
	_, n, err = readVintFromBytes(data[pos:])
	if err != nil {
		return nil
	}
	pos += n

	// nameLen (vint)
	nameLen, n, err := readVintFromBytes(data[pos:])
	if err != nil {
		return nil
	}
	pos += n

	if pos+int(nameLen) > len(data) {
		return nil
	}
	name := string(data[pos : pos+int(nameLen)])
	return &serviceHeader{name: name}
}

// parseHTimeRecord parses the file time extra record.
func parseHTimeRecord(data []byte, fe *fileEntry) {
	if len(data) < 1 {
		return
	}
	flags, n, err := readVintFromBytes(data)
	if err != nil {
		return
	}
	data = data[n:]

	unixTime := flags&fhHTimeUnixTime != 0

	if flags&fhHTimeMtime != 0 {
		if unixTime {
			if len(data) >= 4 {
				t := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
				fe.Mtime = time.Unix(int64(t), 0)
				data = data[4:]
			}
		} else {
			// Windows FILETIME (8 bytes)
			if len(data) >= 8 {
				ft := uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
					uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56
				fe.Mtime = winFileTimeToTime(ft)
				data = data[8:]
			}
		}
	}
	// We skip ctime and atime for now
	_ = data
}

// winFileTimeToTime converts a Windows FILETIME (100-ns intervals since Jan 1, 1601) to time.Time.
func winFileTimeToTime(ft uint64) time.Time {
	// FILETIME is 100-nanosecond intervals since January 1, 1601
	const epoch = int64(116444736000000000) // FILETIME for Unix epoch (1970-01-01)
	ns := (int64(ft) - epoch) * 100        // convert to nanoseconds since Unix epoch
	return time.Unix(0, ns)
}

// timeToWinFileTime converts time.Time to Windows FILETIME.
func timeToWinFileTime(t time.Time) uint64 {
	const epoch = int64(116444736000000000)
	ns := t.UnixNano()
	return uint64(ns/100 + epoch)
}

// readArchive reads all entries from a RAR archive.
func readArchive(path string) (*rarReader, []fileEntry, error) {
	rr, err := openRarReader(path)
	if err != nil {
		return nil, nil, err
	}

	var entries []fileEntry

	for {
		bh, err := rr.readBlockHeader()
		if err == io.EOF {
			break
		}
		if err != nil {
			rr.Close()
			return nil, nil, fmt.Errorf("reading archive: %w", err)
		}

		switch bh.HeaderType {
		case headerTypeMain:
			parseMainHeader(bh, &rr.info)
			rr.skipData(bh)
		case headerTypeFile:
			fe, err := parseFileHeader(bh)
			if err != nil {
				rr.skipData(bh)
				continue
			}
			entries = append(entries, *fe)
			rr.skipData(bh)
		case headerTypeService:
			// Parse service header (CMT = comment, QO = quick open, etc.)
			svc := parseServiceHeader(bh)
			if svc != nil && svc.name == serviceNameCmt && bh.DataSize > 0 {
				data := make([]byte, bh.DataSize)
				if _, err := io.ReadFull(rr.f, data); err == nil {
					rr.info.HasComment = true
					rr.info.Comment = string(data)
				}
				// Data has been read (or partially attempted), no skipData needed
			} else {
				rr.skipData(bh)
			}
		case headerTypeEnd:
			rr.skipData(bh)
			break
		default:
			rr.skipData(bh)
		}

		if bh.HeaderType == headerTypeEnd {
			break
		}
	}

	return rr, entries, nil
}

// parseMainHeader extracts archive info from the main header.
func parseMainHeader(bh *blockHeader, info *archiveInfo) {
	if len(bh.Body) == 0 {
		return
	}
	archFlags, _, err := readVintFromBytes(bh.Body)
	if err != nil {
		return
	}
	info.ArchFlags = archFlags
	info.IsLocked = archFlags&archFlagLock != 0
}

// ---- Archive Writer ----

type rarWriter struct {
	f          *os.File
	tmpPath    string
	finalPath  string
	compLevel  int
	solid      bool
	archFlags  uint64
	comment    string
}

func newRarWriter(path string, compLevel int, solid bool) (*rarWriter, error) {
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}
	w := &rarWriter{
		f:         f,
		tmpPath:   tmpPath,
		finalPath: path,
		compLevel: compLevel,
		solid:     solid,
	}
	// Write signature
	if _, err := f.Write(rar5Signature); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	return w, nil
}

// writeMainHeader writes the main archive header.
func (w *rarWriter) writeMainHeader() error {
	archFlags := w.archFlags
	if w.solid {
		archFlags |= archFlagSolid
	}

	// Build type-specific data: archive flags
	var tsdata []byte
	tsdata = append(tsdata, encodeVint(archFlags)...)

	// Build locator extra record
	var locator []byte
	locatorFlags := uint64(0)
	locatorData := encodeVint(uint64(fhExtraHTime)) // placeholder type
	_ = locatorData

	// Locator record: type=1, flags=0 (no quick open, no recovery)
	var locRec []byte
	locRec = append(locRec, encodeVint(mhExtraLocator)...) // type
	locRec = append(locRec, encodeVint(locatorFlags)...)   // flags
	// size of record data (from type onwards)
	sizeVint := encodeVint(uint64(len(locRec)))
	locator = append(locator, sizeVint...)
	locator = append(locator, locRec...)

	flags := uint64(hflExtra | hflSkipUnknown)
	hdr := buildHeader(headerTypeMain, flags, tsdata, locator, 0)
	_, err := w.f.Write(hdr)
	return err
}

// buildFileTimeExtraRecord builds a file time extra area record.
// Uses Windows FILETIME format (8-byte, 100-ns intervals since 1601).
func buildFileTimeExtraRecord(mtime time.Time) []byte {
	ft := timeToWinFileTime(mtime)
	ftFlags := uint64(fhHTimeMtime) // mtime present, not unix format

	var rec []byte
	rec = append(rec, encodeVint(fhExtraHTime)...) // type
	rec = append(rec, encodeVint(ftFlags)...)       // flags
	rec = append(rec, uint64LEBytes(ft)...)         // 8-byte Windows FILETIME

	// size of record (from type onwards)
	sizeVint := encodeVint(uint64(len(rec)))
	var result []byte
	result = append(result, sizeVint...)
	result = append(result, rec...)
	return result
}

// AddFile adds a file to the archive.
func (w *rarWriter) AddFile(arcName string, data []byte, mtime time.Time, attrs uint32, isDir bool) error {
	fileFlags := uint64(fileFlagCRC32)
	if isDir {
		fileFlags |= fileFlagDirectory
	}

	// Compute CRC32 of the file data
	dataCRC32 := computeCRC32(data)

	// Compression info: version=0, not solid, method=0 (store), dict=0
	// For now we always store regardless of compLevel (produces valid archives)
	compInfo := uint64(0) // method 0 (store), version 0

	// Build extra area
	var extra []byte
	if !mtime.IsZero() {
		extra = buildFileTimeExtraRecord(mtime)
	}

	// Convert path separators to forward slash
	arcName = filepath.ToSlash(arcName)

	// Build type-specific data
	var tsdata []byte
	tsdata = append(tsdata, encodeVint(fileFlags)...)
	tsdata = append(tsdata, encodeVint(uint64(len(data)))...) // unpacked size
	tsdata = append(tsdata, encodeVint(uint64(attrs))...)     // attributes
	// mtime not in main header (flag 0x0002 not set)
	tsdata = append(tsdata, uint32LEBytes(dataCRC32)...) // CRC32 (flag 0x0004 set)
	tsdata = append(tsdata, encodeVintPadded(compInfo, 2)...) // compression info (2 bytes padded)
	tsdata = append(tsdata, encodeVint(hostOSUnix)...)   // host OS (Unix)
	nameBytes := []byte(arcName)
	tsdata = append(tsdata, encodeVint(uint64(len(nameBytes)))...) // name length
	tsdata = append(tsdata, nameBytes...)                           // name

	// Build flags
	commonFlags := uint64(hflData) // data area present
	if len(extra) > 0 {
		commonFlags |= hflExtra
	}

	hdr := buildHeader(headerTypeFile, commonFlags, tsdata, extra, uint64(len(data)))
	if _, err := w.f.Write(hdr); err != nil {
		return err
	}

	// Write data area
	if _, err := w.f.Write(data); err != nil {
		return err
	}
	return nil
}

// writeEndHeader writes the end of archive header.
func (w *rarWriter) writeEndHeader() error {
	var tsdata []byte
	tsdata = append(tsdata, encodeVint(0)...) // end flags = 0

	flags := uint64(hflSkipUnknown)
	hdr := buildHeader(headerTypeEnd, flags, tsdata, nil, 0)
	_, err := w.f.Write(hdr)
	return err
}

// writeCommentHeader writes a CMT service header with the given comment text.
func (w *rarWriter) writeCommentHeader() error {
	if w.comment == "" {
		return nil
	}

	// Service headers are like file headers with a special name.
	// Name = "CMT", data = comment bytes
	commentData := []byte(w.comment)
	fileFlags := uint64(fileFlagCRC32)
	dataCRC32 := computeCRC32(commentData)
	compInfo := uint64(0) // stored

	nameBytes := []byte(serviceNameCmt)
	var tsdata []byte
	tsdata = append(tsdata, encodeVint(fileFlags)...)
	tsdata = append(tsdata, encodeVint(uint64(len(commentData)))...) // unpacked size
	tsdata = append(tsdata, encodeVint(0)...)                        // attributes
	tsdata = append(tsdata, uint32LEBytes(dataCRC32)...)             // CRC32
	tsdata = append(tsdata, encodeVintPadded(compInfo, 2)...)        // compression info
	tsdata = append(tsdata, encodeVint(hostOSUnix)...)               // host OS
	tsdata = append(tsdata, encodeVint(uint64(len(nameBytes)))...)    // name length
	tsdata = append(tsdata, nameBytes...)                             // name

	commonFlags := uint64(hflData | hflSkipUnknown)
	hdr := buildHeader(headerTypeService, commonFlags, tsdata, nil, uint64(len(commentData)))
	if _, err := w.f.Write(hdr); err != nil {
		return err
	}
	_, err := w.f.Write(commentData)
	return err
}

// Close finalizes and closes the archive.
func (w *rarWriter) Close() error {
	if w.comment != "" {
		if err := w.writeCommentHeader(); err != nil {
			w.f.Close()
			os.Remove(w.tmpPath)
			return err
		}
	}
	if err := w.writeEndHeader(); err != nil {
		w.f.Close()
		os.Remove(w.tmpPath)
		return err
	}
	if err := w.f.Sync(); err != nil {
		w.f.Close()
		os.Remove(w.tmpPath)
		return err
	}
	if err := w.f.Close(); err != nil {
		os.Remove(w.tmpPath)
		return err
	}
	return os.Rename(w.tmpPath, w.finalPath)
}

// readFileData reads the data for a file entry from the archive.
func readFileData(rr *rarReader, fe *fileEntry) ([]byte, error) {
	if _, err := rr.f.Seek(fe.DataOffset, io.SeekStart); err != nil {
		return nil, err
	}
	method := compMethod(fe.CompInfo)
	if method != 0 {
		return nil, fmt.Errorf("compressed data (method %d) not supported for extraction", method)
	}
	data := make([]byte, fe.PackedSize)
	if _, err := io.ReadFull(rr.f, data); err != nil {
		return nil, err
	}
	return data, nil
}

// normalizeExtractPath cleans a path for extraction to prevent path traversal.
func normalizeExtractPath(name string) string {
	// Replace backslashes with forward slashes
	name = strings.ReplaceAll(name, "\\", "/")
	// Clean the path
	name = filepath.Clean(name)
	// Remove leading slashes and drive letters
	for strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		name = name[1:]
	}
	// Remove ../ components
	parts := strings.Split(name, "/")
	var cleanParts []string
	for _, p := range parts {
		if p != ".." && p != "." {
			cleanParts = append(cleanParts, p)
		}
	}
	return strings.Join(cleanParts, string(os.PathSeparator))
}

// matchesPattern checks if name matches a wildcard pattern.
func matchesPattern(name, pattern string) bool {
	name = strings.ToLower(filepath.Base(name))
	pattern = strings.ToLower(pattern)
	matched, _ := filepath.Match(pattern, name)
	if matched {
		return true
	}
	// Also try full path match
	matched2, _ := filepath.Match(pattern, strings.ToLower(name))
	return matched2
}

// entryMatchesPatterns checks if a file entry matches any of the given patterns.
// Empty patterns = match all.
func entryMatchesPatterns(fe *fileEntry, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if matchesPattern(fe.Name, p) {
			return true
		}
		// Also match full path
		matched, _ := filepath.Match(p, fe.Name)
		if matched {
			return true
		}
	}
	return false
}
