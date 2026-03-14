package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

// RAR 5.0 signature
var rar5Signature = []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00}

// RAR 4.x (and earlier) signature prefix
var rar4Signature = []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00}

// Compression algorithm versions
const (
	compVer5 = 0 // RAR 5 algorithm
	compVer7 = 1 // RAR 7 algorithm (not supported)
)

// Header types
const (
	headerTypeMain       = 1
	headerTypeFile       = 2
	headerTypeService    = 3
	headerTypeEncryption = 4
	headerTypeEnd        = 5
)

// Common header flags
const (
	hflExtra        = 0x0001
	hflData         = 0x0002
	hflSkipUnknown  = 0x0004
	hflSplitBefore  = 0x0008
	hflSplitAfter   = 0x0010
	hflChild        = 0x0020
	hflInherited    = 0x0040
)

// Archive header flags (MHFL_*)
const (
	archFlagVolume    = 0x0001
	archFlagVolNumber = 0x0002
	archFlagSolid     = 0x0004
	archFlagProtect   = 0x0008
	archFlagLock      = 0x0010
)

// File header flags (FHFL_*)
const (
	fileFlagDirectory   = 0x0001
	fileFlagUtime       = 0x0002
	fileFlagCRC32       = 0x0004
	fileFlagUnpUnknown  = 0x0008
)

// End of archive flags
const (
	endFlagNextVolume = 0x0001
)

// Host OS values
const (
	hostOSWindows = 0
	hostOSUnix    = 1
)

// Compression info constants
// Bits 0-5: algorithm version (0 or 1)
// Bit 6: solid flag
// Bits 7-9 (mask 0x380): method 0-5 (0=store)
// Bits 10-14 (mask 0x7c00): dictionary size (128KB * 2^N)
const (
	compMethodShift = 7
	compDictShift   = 10
)

// Main archive extra types
const (
	mhExtraLocator  = 0x01
	mhExtraMetadata = 0x02
)

// Locator extra flags
const (
	mhExtraLocatorQList = 0x01
	mhExtraLocatorRR    = 0x02
)

// File/service header extra types
const (
	fhExtraCrypt   = 0x01
	fhExtraHash    = 0x02
	fhExtraHTime   = 0x03
	fhExtraVersion = 0x04
	fhExtraRedir   = 0x05
	fhExtraUOwner  = 0x06
	fhExtraSubData = 0x07
)

// File time record flags
const (
	fhHTimeUnixTime = 0x01 // Unix time_t format
	fhHTimeMtime    = 0x02 // mtime present
	fhHTimeCtime    = 0x04 // ctime present
	fhHTimeAtime    = 0x08 // atime present
	fhHTimeUnixNS   = 0x10 // nanoseconds present
)

// Unix owner record flags
const (
	fhUOwnerUName  = 0x01
	fhUOwnerGName  = 0x02
	fhUOwnerNumUID = 0x04
	fhUOwnerNumGID = 0x08
)

// Service header names
const (
	serviceNameCmt = "CMT"
	serviceNameQO  = "QO"
)

// ---- Variable-length integer (vint) ----

// encodeVint encodes a uint64 as a RAR 5.0 variable-length integer.
func encodeVint(v uint64) []byte {
	if v == 0 {
		return []byte{0x00}
	}
	var buf [10]byte
	i := 0
	for v > 0 {
		b := byte(v & 0x7f)
		v >>= 7
		if v > 0 {
			b |= 0x80
		}
		buf[i] = b
		i++
	}
	return buf[:i]
}

// encodeVintPadded encodes a uint64 as a padded vint with a fixed number of bytes.
// Extra bytes are filled with 0x80 (continuation + zero).
func encodeVintPadded(v uint64, n int) []byte {
	b := encodeVint(v)
	if len(b) >= n {
		return b
	}
	// pad with continuation bytes
	padded := make([]byte, n)
	copy(padded, b)
	// Set continuation bits on all but last
	for i := 0; i < n-1; i++ {
		if i < len(b)-1 {
			// already set
		} else if i == len(b)-1 {
			// last real byte: set continuation bit
			padded[i] |= 0x80
		} else {
			// padding byte: 0x80 = continuation + zero
			padded[i] = 0x80
		}
	}
	// last byte: no continuation
	padded[n-1] = padded[n-1] &^ 0x80
	return padded
}

// readVint reads a variable-length integer from a reader.
func readVint(r io.Reader) (uint64, int, error) {
	var result uint64
	var shift uint
	bytesRead := 0
	for {
		var b [1]byte
		_, err := io.ReadFull(r, b[:])
		if err != nil {
			return 0, bytesRead, err
		}
		bytesRead++
		result |= uint64(b[0]&0x7f) << shift
		if b[0]&0x80 == 0 {
			break
		}
		shift += 7
		if shift >= 63 {
			return 0, bytesRead, fmt.Errorf("vint too long")
		}
	}
	return result, bytesRead, nil
}

// readVintFromBytes reads a vint from a byte slice and returns value, bytes consumed.
func readVintFromBytes(data []byte) (uint64, int, error) {
	var result uint64
	var shift uint
	for i, b := range data {
		result |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, i + 1, nil
		}
		shift += 7
		if shift >= 63 {
			return 0, 0, fmt.Errorf("vint too long")
		}
	}
	return 0, 0, io.ErrUnexpectedEOF
}

// writeVint writes a vint to w.
func writeVint(w io.Writer, v uint64) error {
	_, err := w.Write(encodeVint(v))
	return err
}

// ---- CRC32 ----

// computeCRC32 computes CRC32/IEEE of data.
func computeCRC32(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

// ---- Little-endian read/write helpers ----

func readUint16LE(r io.Reader) (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b[:]), nil
}

func readUint32LE(r io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}

func readUint64LE(r io.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

func writeUint32LE(w io.Writer, v uint32) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	_, err := w.Write(b[:])
	return err
}

func writeUint64LE(w io.Writer, v uint64) error {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	_, err := w.Write(b[:])
	return err
}

func uint32LEBytes(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

func uint64LEBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

// ---- Header building helpers ----

// buildHeader builds a complete RAR 5.0 block header.
// headerType: block type vint value
// commonFlags: common header flags
// typeSpecificData: bytes after the common flags (and after optional extra/data size fields)
// extraArea: optional extra area bytes (nil if not used)
// Returns the complete header (CRC32 + header_size + body + extra area).
// Data area is written separately and is NOT included here.
func buildHeader(headerType uint64, commonFlags uint64, typeSpecificData []byte, extraArea []byte, dataSize uint64) []byte {
	// Build the header body (from type onwards)
	var body []byte

	body = append(body, encodeVint(headerType)...)
	body = append(body, encodeVint(commonFlags)...)

	if commonFlags&hflExtra != 0 {
		body = append(body, encodeVint(uint64(len(extraArea)))...)
	}
	if commonFlags&hflData != 0 {
		body = append(body, encodeVint(dataSize)...)
	}

	body = append(body, typeSpecificData...)

	if extraArea != nil {
		body = append(body, extraArea...)
	}

	// Build header_size vint + body
	headerSizeBytes := encodeVint(uint64(len(body)))
	payload := append(headerSizeBytes, body...)

	// Compute CRC32 over header_size + body
	crc := computeCRC32(payload)

	// Final header
	var result []byte
	result = append(result, uint32LEBytes(crc)...)
	result = append(result, payload...)

	return result
}
