package main

import (
	"fmt"
	"sort"
)

// ---- RAR5 compression/decompression constants ----

const (
	r5NC  = 306 // literal/length alphabet size
	r5DCB = 64  // base distance alphabet size
	r5LDC = 16  // low distance alphabet size
	r5RC  = 44  // repeat match length alphabet size
	r5BC  = 20  // pre-code alphabet size

	r5HuffTableSize = r5NC + r5DCB + r5LDC + r5RC // 430

	r5MaxCodeLen = 15 // maximum Huffman code length
	r5MaxMatch   = 0x1001
	r5MinMatch   = 3

	r5QuickBits      = 9 // quick decode bits for large tables (NC)
	r5QuickBitsSmall = 6 // quick decode bits for small tables
)

// ---- Bit reader (MSB first) ----

type bitReader struct {
	data    []byte
	bytePos int
	bitPos  int // bits already consumed from current byte (0-7)
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

// getbits returns the next 16 bits (MSB first) without advancing.
func (br *bitReader) getbits() uint16 {
	var v uint32
	if br.bytePos < len(br.data) {
		v |= uint32(br.data[br.bytePos]) << 24
	}
	if br.bytePos+1 < len(br.data) {
		v |= uint32(br.data[br.bytePos+1]) << 16
	}
	if br.bytePos+2 < len(br.data) {
		v |= uint32(br.data[br.bytePos+2]) << 8
	}
	v <<= uint(br.bitPos)
	return uint16(v >> 16)
}

// getbits32 returns the next 32 bits (MSB first) without advancing.
func (br *bitReader) getbits32() uint32 {
	var v uint64
	if br.bytePos < len(br.data) {
		v |= uint64(br.data[br.bytePos]) << 56
	}
	if br.bytePos+1 < len(br.data) {
		v |= uint64(br.data[br.bytePos+1]) << 48
	}
	if br.bytePos+2 < len(br.data) {
		v |= uint64(br.data[br.bytePos+2]) << 40
	}
	if br.bytePos+3 < len(br.data) {
		v |= uint64(br.data[br.bytePos+3]) << 32
	}
	if br.bytePos+4 < len(br.data) {
		v |= uint64(br.data[br.bytePos+4]) << 24
	}
	v <<= uint(br.bitPos)
	return uint32(v >> 32)
}

// addbits advances the bit position by n.
func (br *bitReader) addbits(n int) {
	n += br.bitPos
	br.bytePos += n >> 3
	br.bitPos = n & 7
}

// alignByte advances to the next byte boundary.
func (br *bitReader) alignByte() {
	if br.bitPos > 0 {
		br.addbits(8 - br.bitPos)
	}
}

// ---- Decode table ----

type decodeTable struct {
	maxNum    int
	decodeLen [16]uint32
	decodePos [16]uint32
	decodeNum []uint16
	quickBits int
	quickLen  []byte
	quickNum  []uint16
}

func makeDecodeTable(lengths []byte, quickBits int) *decodeTable {
	dt := &decodeTable{
		maxNum:    len(lengths),
		quickBits: quickBits,
	}
	dt.decodeNum = make([]uint16, len(lengths))

	var lenCount [16]int
	for _, l := range lengths {
		lenCount[l&0xf]++
	}
	lenCount[0] = 0

	dt.decodePos[0] = 0
	dt.decodeLen[0] = 0
	upperLimit := uint32(0)
	for i := 1; i < 16; i++ {
		upperLimit += uint32(lenCount[i])
		leftAligned := upperLimit << uint(16-i)
		upperLimit *= 2
		dt.decodeLen[i] = leftAligned
		dt.decodePos[i] = dt.decodePos[i-1] + uint32(lenCount[i-1])
	}

	copyDecodePos := dt.decodePos
	for i, l := range lengths {
		if cl := l & 0xf; cl != 0 {
			lastPos := copyDecodePos[cl]
			dt.decodeNum[lastPos] = uint16(i)
			copyDecodePos[cl]++
		}
	}

	quickDataSize := 1 << uint(dt.quickBits)
	dt.quickLen = make([]byte, quickDataSize)
	dt.quickNum = make([]uint16, quickDataSize)

	curBitLength := 1
	for code := 0; code < quickDataSize; code++ {
		bitField := uint32(code) << uint(16-dt.quickBits)
		// Advance until curBitLength matches the code length; cap at 15 to stay in bounds.
		for curBitLength < 15 && bitField >= dt.decodeLen[curBitLength] {
			curBitLength++
		}
		dt.quickLen[code] = byte(curBitLength)
		dist := (bitField - dt.decodeLen[curBitLength-1]) >> uint(16-curBitLength)
		pos := dt.decodePos[curBitLength] + dist
		if int(pos) < len(lengths) {
			dt.quickNum[code] = dt.decodeNum[pos]
		}
	}

	return dt
}

func decodeNumber(br *bitReader, dt *decodeTable) int {
	bitField := uint32(br.getbits()) & 0xfffe
	if bitField < dt.decodeLen[dt.quickBits] {
		code := bitField >> uint(16-dt.quickBits)
		if int(code) < len(dt.quickLen) {
			br.addbits(int(dt.quickLen[code]))
			return int(dt.quickNum[code])
		}
	}
	bits := 15
	for i := dt.quickBits + 1; i < 15; i++ {
		if bitField < dt.decodeLen[i] {
			bits = i
			break
		}
	}
	br.addbits(bits)
	dist := (bitField - dt.decodeLen[bits-1]) >> uint(16-bits)
	pos := int(dt.decodePos[bits]) + int(dist)
	if pos >= dt.maxNum {
		pos = 0
	}
	return int(dt.decodeNum[pos])
}

// ---- RAR5 block header ----

type r5BlockHeader struct {
	blockSize    int
	blockBitSize int // valid bits in last byte of block
	lastBlock    bool
	tablePresent bool
	blockStart   int // byte offset within the compressed stream
}

func readR5BlockHeader(br *bitReader) (r5BlockHeader, error) {
	br.alignByte()
	var bh r5BlockHeader

	blockFlags := byte(br.getbits() >> 8)
	br.addbits(8)

	byteCount := int((blockFlags>>3)&3) + 1
	if byteCount == 4 {
		return bh, fmt.Errorf("invalid block header: byteCount=4")
	}

	bh.blockBitSize = int(blockFlags&7) + 1
	bh.lastBlock = blockFlags&0x40 != 0
	bh.tablePresent = blockFlags&0x80 != 0

	savedCheckSum := byte(br.getbits() >> 8)
	br.addbits(8)

	blockSize := 0
	for i := 0; i < byteCount; i++ {
		blockSize |= int(br.getbits()>>8) << (i * 8)
		br.addbits(8)
	}
	bh.blockSize = blockSize

	checkSum := byte(0x5a ^ blockFlags ^ byte(blockSize) ^ byte(blockSize>>8) ^ byte(blockSize>>16))
	if checkSum != savedCheckSum {
		return bh, fmt.Errorf("block header checksum mismatch: got %02x, expected %02x", checkSum, savedCheckSum)
	}

	bh.blockStart = br.bytePos
	return bh, nil
}

// isBlockEnd returns true when we have consumed all the data in this block.
func isBlockEnd(br *bitReader, bh r5BlockHeader) bool {
	blockEndByte := bh.blockStart + bh.blockSize - 1
	return br.bytePos > blockEndByte ||
		(br.bytePos == blockEndByte && br.bitPos >= bh.blockBitSize)
}

// ---- RAR5 Huffman tables ----

type r5Tables struct {
	LD  *decodeTable // literals/lengths
	DD  *decodeTable // distances
	LDD *decodeTable // low distances
	RD  *decodeTable // repeat distances
	BD  *decodeTable // pre-code for table encoding
}

func readR5Tables(br *bitReader, bh r5BlockHeader, dcb int) (r5Tables, error) {
	var tables r5Tables
	if !bh.tablePresent {
		return tables, nil
	}

	// Read pre-code lengths (BC entries, 4 bits each, with special run-of-zeros encoding)
	bcLen := make([]byte, r5BC)
	for i := 0; i < r5BC; {
		l := byte(br.getbits() >> 12)
		br.addbits(4)
		if l == 15 {
			zeroCount := byte(br.getbits() >> 12)
			br.addbits(4)
			if zeroCount == 0 {
				bcLen[i] = 15
				i++
			} else {
				zeroCount += 2
				for zeroCount > 0 && i < r5BC {
					bcLen[i] = 0
					i++
					zeroCount--
				}
			}
		} else {
			bcLen[i] = l
			i++
		}
	}
	tables.BD = makeDecodeTable(bcLen, r5QuickBitsSmall)

	// Decode the main table using pre-codes
	tableSize := r5NC + dcb + r5LDC + r5RC
	table := make([]byte, tableSize)
	for i := 0; i < tableSize; {
		number := decodeNumber(br, tables.BD)
		if number < 16 {
			table[i] = byte(number)
			i++
		} else if number < 18 {
			var n int
			if number == 16 {
				n = int(br.getbits()>>13) + 3 // 3 bits → 3..10
				br.addbits(3)
			} else {
				n = int(br.getbits()>>9) + 11 // 7 bits → 11..138
				br.addbits(7)
			}
			if i == 0 {
				return tables, fmt.Errorf("invalid Huffman table: repeat at start")
			}
			for n > 0 && i < tableSize {
				table[i] = table[i-1]
				i++
				n--
			}
		} else {
			var n int
			if number == 18 {
				n = int(br.getbits()>>13) + 3 // 3 bits → 3..10
				br.addbits(3)
			} else {
				n = int(br.getbits()>>9) + 11 // 7 bits → 11..138
				br.addbits(7)
			}
			for n > 0 && i < tableSize {
				table[i] = 0
				i++
				n--
			}
		}
	}

	tables.LD = makeDecodeTable(table[0:r5NC], r5QuickBits)
	tables.DD = makeDecodeTable(table[r5NC:r5NC+dcb], r5QuickBitsSmall)
	tables.LDD = makeDecodeTable(table[r5NC+dcb:r5NC+dcb+r5LDC], r5QuickBitsSmall)
	tables.RD = makeDecodeTable(table[r5NC+dcb+r5LDC:], r5QuickBitsSmall)
	return tables, nil
}

// slotToLength converts a length slot to an actual match length.
func slotToLength(br *bitReader, slot int) int {
	var lBits, length int
	if slot < 8 {
		lBits = 0
		length = 2 + slot
	} else {
		lBits = slot/4 - 1
		length = 2 + (4|(slot&3))<<uint(lBits)
	}
	if lBits > 0 {
		length += int(br.getbits()) >> uint(16-lBits)
		br.addbits(lBits)
	}
	return length
}

// ---- RAR5 decompressor ----

// decompress5 decompresses RAR5 packed data.
// windowSize is the dictionary size (power of two, from compInfo); 0 = default (128KB).
func decompress5(compressed []byte, unpackedSize int, windowSize int) ([]byte, error) {
	if unpackedSize == 0 {
		return nil, nil
	}
	if windowSize <= 0 {
		windowSize = 0x20000 // default 128KB
	}
	// Cap to avoid excessive memory
	if windowSize > 0x8000000 { // 128MB cap
		windowSize = 0x8000000
	}
	// Round up to power of two
	wsz := 1
	for wsz < windowSize {
		wsz <<= 1
	}
	windowSize = wsz
	mask := windowSize - 1

	window := make([]byte, windowSize)
	output := make([]byte, 0, unpackedSize)
	unptr := 0

	br := newBitReader(compressed)

	bh, err := readR5BlockHeader(br)
	if err != nil {
		return nil, fmt.Errorf("decompress5: %w", err)
	}
	tables, err := readR5Tables(br, bh, r5DCB)
	if err != nil {
		return nil, fmt.Errorf("decompress5: %w", err)
	}

	var oldDist [4]int
	lastLength := 0

	for len(output) < unpackedSize {
		if isBlockEnd(br, bh) {
			if bh.lastBlock {
				break
			}
			bh, err = readR5BlockHeader(br)
			if err != nil {
				return nil, fmt.Errorf("decompress5: %w", err)
			}
			tables, err = readR5Tables(br, bh, r5DCB)
			if err != nil {
				return nil, fmt.Errorf("decompress5: %w", err)
			}
		}

		mainSlot := decodeNumber(br, tables.LD)

		if mainSlot < 256 {
			// Literal byte
			b := byte(mainSlot)
			window[unptr] = b
			output = append(output, b)
			unptr = (unptr + 1) & mask
			continue
		}

		if mainSlot >= 262 {
			// LZ match
			length := slotToLength(br, mainSlot-262)

			distSlot := decodeNumber(br, tables.DD)
			var dist int
			if distSlot < 4 {
				dist = 1 + distSlot
			} else {
				dbits := distSlot/2 - 1
				dist = 1 + (2|(distSlot&1))<<uint(dbits)
				if dbits >= 4 {
					if dbits > 4 {
						highBits := int(br.getbits32()) >> uint(36-dbits)
						br.addbits(dbits - 4)
						dist += highBits << 4
					}
					lowDist := decodeNumber(br, tables.LDD)
					dist += lowDist
				} else {
					extra := int(br.getbits()) >> uint(16-dbits)
					br.addbits(dbits)
					dist += extra
				}
			}

			// Adjust length for large distances
			if dist > 0x100 {
				length++
				if dist > 0x2000 {
					length++
					if dist > 0x40000 {
						length++
					}
				}
			}

			oldDist[3] = oldDist[2]
			oldDist[2] = oldDist[1]
			oldDist[1] = oldDist[0]
			oldDist[0] = dist
			lastLength = length

			for i := 0; i < length && len(output) < unpackedSize; i++ {
				srcPtr := (unptr - dist + windowSize) & mask
				b := window[srcPtr]
				window[unptr] = b
				output = append(output, b)
				unptr = (unptr + 1) & mask
			}
			continue
		}

		if mainSlot == 256 {
			// Filter: not supported in this implementation
			return nil, fmt.Errorf("decompress5: RAR5 filters not supported")
		}

		if mainSlot == 257 {
			// Repeat last match
			if lastLength != 0 {
				dist := oldDist[0]
				for i := 0; i < lastLength && len(output) < unpackedSize; i++ {
					srcPtr := (unptr - dist + windowSize) & mask
					b := window[srcPtr]
					window[unptr] = b
					output = append(output, b)
					unptr = (unptr + 1) & mask
				}
			}
			continue
		}

		if mainSlot < 262 {
			// Repeat old distance (slots 258-261 → oldDist[0..3])
			distNum := mainSlot - 258
			dist := oldDist[distNum]
			copy(oldDist[1:distNum+1], oldDist[0:distNum])
			oldDist[0] = dist

			lengthSlot := decodeNumber(br, tables.RD)
			length := slotToLength(br, lengthSlot)
			lastLength = length

			for i := 0; i < length && len(output) < unpackedSize; i++ {
				srcPtr := (unptr - dist + windowSize) & mask
				b := window[srcPtr]
				window[unptr] = b
				output = append(output, b)
				unptr = (unptr + 1) & mask
			}
			continue
		}
	}

	if len(output) > unpackedSize {
		output = output[:unpackedSize]
	}
	return output, nil
}

// ---- Bit writer (MSB first) ----

type bitWriter struct {
	buf     []byte
	pending uint64
	bitLen  int
}

// writeBits writes the bottom n bits of value, MSB first.
func (bw *bitWriter) writeBits(value uint64, n int) {
	if n == 0 {
		return
	}
	mask := uint64((1 << uint(n)) - 1)
	bw.pending = (bw.pending << uint(n)) | (value & mask)
	bw.bitLen += n
	for bw.bitLen >= 8 {
		bw.bitLen -= 8
		bw.buf = append(bw.buf, byte(bw.pending>>uint(bw.bitLen)))
	}
}

// bytes returns the bytes written so far (without flushing).
func (bw *bitWriter) bytes() []byte {
	return bw.buf
}

// flush writes any remaining bits (zero-padded) and returns (data, validBitsInLastByte).
func (bw *bitWriter) flush() ([]byte, int) {
	result := make([]byte, len(bw.buf))
	copy(result, bw.buf)
	lastBits := 8
	if bw.bitLen > 0 {
		lastBits = bw.bitLen
		result = append(result, byte(bw.pending<<uint(8-bw.bitLen)))
	}
	bw.buf = nil
	bw.pending = 0
	bw.bitLen = 0
	return result, lastBits
}

// ---- Huffman code builder ----

// buildHuffmanLengths computes optimal Huffman code lengths for the given
// symbol frequencies. maxBits limits the maximum code length (≤15).
func buildHuffmanLengths(freqs []int, maxBits int) []int {
	n := len(freqs)
	lengths := make([]int, n)

	// Collect symbols with non-zero frequency
	type symFreq struct{ sym, freq int }
	syms := make([]symFreq, 0, n)
	for i, f := range freqs {
		if f > 0 {
			syms = append(syms, symFreq{i, f})
		}
	}
	if len(syms) == 0 {
		return lengths
	}
	if len(syms) == 1 {
		lengths[syms[0].sym] = 1
		return lengths
	}

	// Build Huffman tree using sorted list (merge-pairs approach).
	// Nodes 0..len(syms)-1 are leaves; rest are internal.
	numSyms := len(syms)
	totalNodes := 2*numSyms - 1
	nodeFreq := make([]int, totalNodes)
	nodeLeft := make([]int, totalNodes)
	nodeRight := make([]int, totalNodes)
	for i := range nodeLeft {
		nodeLeft[i] = -1
		nodeRight[i] = -1
	}
	for i, s := range syms {
		nodeFreq[i] = s.freq
	}

	// Priority queue: sorted slice of (freq, nodeIdx)
	type entry struct{ freq, idx int }
	pq := make([]entry, numSyms)
	for i := range syms {
		pq[i] = entry{syms[i].freq, i}
	}
	sort.Slice(pq, func(a, b int) bool { return pq[a].freq < pq[b].freq })

	insertSorted := func(e entry) {
		// Binary search for insertion point
		lo, hi := 0, len(pq)
		for lo < hi {
			mid := (lo + hi) / 2
			if pq[mid].freq <= e.freq {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		pq = append(pq, entry{})
		copy(pq[lo+1:], pq[lo:])
		pq[lo] = e
	}

	nextNode := numSyms
	for len(pq) > 1 {
		a, b := pq[0], pq[1]
		pq = pq[2:]
		parentIdx := nextNode
		nextNode++
		nodeFreq[parentIdx] = a.freq + b.freq
		nodeLeft[parentIdx] = a.idx
		nodeRight[parentIdx] = b.idx
		insertSorted(entry{nodeFreq[parentIdx], parentIdx})
	}

	root := pq[0].idx

	// Assign depths via DFS
	var assignDepths func(idx, depth int)
	assignDepths = func(idx, depth int) {
		if nodeLeft[idx] == -1 {
			// leaf — find which symbol this is
			if idx < numSyms {
				lengths[syms[idx].sym] = depth
			}
		} else {
			assignDepths(nodeLeft[idx], depth+1)
			assignDepths(nodeRight[idx], depth+1)
		}
	}
	assignDepths(root, 0)

	// Limit code lengths to maxBits
	limitHuffmanLengths(lengths, maxBits)
	return lengths
}

// limitHuffmanLengths caps all lengths at maxBits and then fixes the Kraft
// inequality by merging longest codes until valid.
func limitHuffmanLengths(lengths []int, maxBits int) {
	// Cap individual lengths
	for i, l := range lengths {
		if l > maxBits {
			lengths[i] = maxBits
		}
	}

	// Fix Kraft inequality (sum 2^(maxBits-li) <= 2^maxBits)
	// Strategy: if overflow, find the two longest codes, merge them
	// (increase their lengths would not help; instead reduce some)
	// Simple approach: sort by (length desc, sym asc) and adjust.
	for {
		kraft := 0
		for _, l := range lengths {
			if l > 0 {
				kraft += 1 << uint(maxBits-l)
			}
		}
		maxK := 1 << uint(maxBits)
		if kraft <= maxK {
			break
		}
		// Find a length-maxBits code and remove it (set to 0), then add it back
		// at a shorter length — but that makes things worse.
		// Instead: find the longest two codes and make one shorter (by 1).
		// This reduces the overflow. Eventually terminates.
		for i, l := range lengths {
			if l == maxBits {
				lengths[i]-- // shorten one code
				break
			}
		}
	}

	// If we have fewer codes than needed to express all 2^maxBits codewords
	// (i.e. the table is under-full), that's OK for canonical Huffman.
}

// makeCanonicalCodes generates canonical Huffman codes from code lengths.
// Returns codes[i] = the code bits for symbol i (MSB aligned within length[i] bits).
func makeCanonicalCodes(lengths []int) []uint32 {
	n := len(lengths)
	codes := make([]uint32, n)

	// Count symbols at each length
	var cnt [16]int
	for _, l := range lengths {
		if l > 0 {
			cnt[l]++
		}
	}

	// Starting code for each length (left-aligned canonical assignment)
	var startCode [16]uint32
	code := uint32(0)
	for i := 1; i <= 15; i++ {
		startCode[i] = code
		code = (code + uint32(cnt[i])) << 1
	}

	// Assign codes in order of (length, symbol index)
	type symLen struct{ sym, length int }
	syms := make([]symLen, 0, n)
	for i, l := range lengths {
		if l > 0 {
			syms = append(syms, symLen{i, l})
		}
	}
	sort.Slice(syms, func(a, b int) bool {
		if syms[a].length != syms[b].length {
			return syms[a].length < syms[b].length
		}
		return syms[a].sym < syms[b].sym
	})

	for _, s := range syms {
		codes[s.sym] = startCode[s.length]
		startCode[s.length]++
	}
	return codes
}

// ---- RLE encoding for Huffman tables ----

type rleEntry struct {
	sym      int // 0-15: literal length; 16/17: repeat prev; 18/19: zero run
	extra    int // extra value
	extraLen int // number of extra bits
}

// rleEncodeTable RLE-encodes a table of Huffman code lengths (values 0-15).
func rleEncodeTable(table []byte) []rleEntry {
	var result []rleEntry
	n := len(table)
	for i := 0; i < n; {
		v := table[i]
		if v == 0 {
			// Count zero run
			j := i + 1
			for j < n && table[j] == 0 {
				j++
			}
			count := j - i
			i = j
			for count > 0 {
				if count >= 11 {
					run := count
					if run > 138 {
						run = 138
					}
					result = append(result, rleEntry{19, run - 11, 7})
					count -= run
				} else if count >= 3 {
					run := count
					if run > 10 {
						run = 10
					}
					result = append(result, rleEntry{18, run - 3, 3})
					count -= run
				} else {
					result = append(result, rleEntry{0, 0, 0})
					count--
				}
			}
		} else {
			// Emit one literal, then check for repeats
			result = append(result, rleEntry{int(v), 0, 0})
			i++
			// Count consecutive same values
			j := i
			for j < n && table[j] == v {
				j++
			}
			count := j - i
			for count > 0 {
				if count >= 11 {
					run := count
					if run > 138 {
						run = 138
					}
					result = append(result, rleEntry{17, run - 11, 7})
					count -= run
					i += run
				} else if count >= 3 {
					run := count
					if run > 10 {
						run = 10
					}
					result = append(result, rleEntry{16, run - 3, 3})
					count -= run
					i += run
				} else {
					result = append(result, rleEntry{int(v), 0, 0})
					count--
					i++
				}
			}
		}
	}
	return result
}

// writeBCTable writes the 20 pre-code lengths (4 bits each, with run-of-zeros).
func writeBCTable(bw *bitWriter, bcLens []int) {
	for i := 0; i < r5BC; {
		l := bcLens[i]
		if l == 0 {
			// Count consecutive zeros
			j := i + 1
			for j < r5BC && bcLens[j] == 0 {
				j++
			}
			count := j - i
			if count >= 3 {
				for count > 0 {
					if count <= 2 {
						bw.writeBits(0, 4)
						count--
						i++
					} else {
						run := count
						if run > 17 { // ZeroCount max = 15 → count = 17
							run = 17
						}
						bw.writeBits(15, 4)       // marker
						bw.writeBits(uint64(run-2), 4) // ZeroCount
						count -= run
						i += run
					}
				}
			} else {
				bw.writeBits(0, 4)
				i++
			}
		} else {
			if l == 15 {
				bw.writeBits(15, 4)
				bw.writeBits(0, 4) // ZeroCount=0 = "literal 15"
			} else {
				bw.writeBits(uint64(l), 4)
			}
			i++
		}
	}
}

// ---- LZ77 hash table ----

const (
	lzHashBits = 16
	lzHashSize = 1 << lzHashBits
	lzHashMask = lzHashSize - 1
	lzNilHash  = -1
)

type lzHashTable struct {
	head [lzHashSize]int
	next []int
}

func newLZHashTable(dataLen int) *lzHashTable {
	ht := &lzHashTable{next: make([]int, dataLen)}
	for i := range ht.head {
		ht.head[i] = lzNilHash
	}
	return ht
}

func lzHash3(data []byte, pos int) int {
	if pos+2 >= len(data) {
		return 0
	}
	h := int(data[pos])*0x1225 ^ int(data[pos+1])*0x3355 ^ int(data[pos+2])*0x55aa
	return h & lzHashMask
}

func (ht *lzHashTable) insert(data []byte, pos int) {
	h := lzHash3(data, pos)
	ht.next[pos] = ht.head[h]
	ht.head[h] = pos
}

// findMatch finds the best LZ77 match at position pos.
func (ht *lzHashTable) findMatch(data []byte, pos, dictSize, maxChain int) (bestLen, bestDist int) {
	n := len(data)
	if pos+r5MinMatch > n {
		return 0, 0
	}

	h := lzHash3(data, pos)
	cur := ht.head[h]

	bestLen = r5MinMatch - 1
	bestDist = 0

	for chainCount := 0; cur != lzNilHash && chainCount < maxChain; chainCount++ {
		dist := pos - cur
		if dist > dictSize || dist <= 0 {
			break
		}

		maxLen := n - pos
		if maxLen > r5MaxMatch {
			maxLen = r5MaxMatch
		}
		matchLen := 0
		for matchLen < maxLen && data[pos+matchLen] == data[cur+matchLen] {
			matchLen++
		}

		if matchLen > bestLen {
			bestLen = matchLen
			bestDist = dist
			if bestLen == r5MaxMatch {
				break
			}
		}
		cur = ht.next[cur]
	}

	if bestLen < r5MinMatch {
		return 0, 0
	}
	return bestLen, bestDist
}

// ---- LZ symbol types ----

type lzSymbol struct {
	isMatch  bool
	literal  byte
	length   int
	distance int
}

// lz77Parse parses data into LZ77 symbols (greedy matching).
func lz77Parse(data []byte, dictSize, maxChain int) []lzSymbol {
	n := len(data)
	if n == 0 {
		return nil
	}

	ht := newLZHashTable(n)
	symbols := make([]lzSymbol, 0, n)

	for pos := 0; pos < n; {
		length, dist := ht.findMatch(data, pos, dictSize, maxChain)
		if length >= r5MinMatch {
			symbols = append(symbols, lzSymbol{true, 0, length, dist})
			for i := 0; i < length; i++ {
				if pos+i+r5MinMatch <= n {
					ht.insert(data, pos+i)
				}
			}
			pos += length
		} else {
			symbols = append(symbols, lzSymbol{false, data[pos], 0, 0})
			ht.insert(data, pos)
			pos++
		}
	}
	return symbols
}

// ---- Slot conversion helpers ----

// lengthToSlot converts an encoded match length (after distance adjustment) to slot.
// Returns (slot, extraBits, extraValue).
func lengthToSlot(encLen int) (slot, extraLen, extraVal int) {
	lval := encLen - 2 // 0-based
	if lval < 0 {
		lval = 0
	}
	if lval < 8 {
		return lval, 0, 0
	}
	// Slot >= 8: lbits = slot/4 - 1, base = (4 | (slot & 3)) << lbits
	for s := 8; s < r5RC; s++ {
		lbits := s/4 - 1
		base := (4 | (s & 3)) << uint(lbits)
		top := base + (1 << uint(lbits))
		if lval >= base && lval < top {
			return s, lbits, lval - base
		}
	}
	// Clamp to maximum slot
	s := r5RC - 1
	lbits := s/4 - 1
	base := (4 | (s & 3)) << uint(lbits)
	return s, lbits, lval - base
}

// distanceToSlot converts a match distance to (distSlot, dbitsHigh, highVal, ldcSym).
// dbitsHigh is the number of raw high bits to write (0 if none).
// highVal is the raw high bits value.
// ldcSym is the LDD symbol to encode the low 4 bits (when dbits >= 4).
func distanceToSlot(dist int) (slot, dbitsHigh, highVal, ldcSym int) {
	d := dist - 1 // 0-based distance
	if d < 4 {
		return d, 0, 0, 0
	}
	for s := 4; s < r5DCB; s++ {
		dbits := s/2 - 1
		base := (2 | (s & 1)) << uint(dbits)
		top := base + (1 << uint(dbits))
		if d >= base && d < top {
			extra := d - base
			if dbits < 4 {
				// Write all extra bits as raw
				return s, dbits, extra, 0
			}
			// dbits >= 4: write (dbits-4) raw high bits + LDC for low 4 bits
			return s, dbits - 4, extra >> 4, extra & 0xf
		}
	}
	return r5DCB - 1, 0, 0, 0
}

// distNeedsLDC returns true when the distance slot uses the LDD table.
func distNeedsLDC(slot int) bool {
	if slot < 4 {
		return false
	}
	return (slot/2 - 1) >= 4 // dbits >= 4
}

// ---- RAR5 compressor ----

// compress5 compresses data using RAR5 LZ77+Huffman coding.
// method is 1-5 (1=fastest, 5=best). Returns nil if compression is not beneficial.
func compress5(data []byte, method int) []byte {
	if len(data) == 0 {
		return []byte{}
	}

	const dictSize = 0x20000 // 128KB (dictBits=0)

	ds := dictSize
	if ds > len(data) {
		ds = len(data)
	}

	// Chain length by method (1=fast, 5=best)
	chainLens := []int{0, 4, 8, 24, 96, 256}
	chainLen := chainLens[method]

	// LZ77 parse
	symbols := lz77Parse(data, ds, chainLen)

	// Count symbol frequencies
	var ldFreq [r5NC]int
	var ddFreq [r5DCB]int
	var lddFreq [r5LDC]int
	var rdFreq [r5RC]int

	for _, sym := range symbols {
		if !sym.isMatch {
			ldFreq[int(sym.literal)]++
		} else {
			// Adjust length for distance
			length := sym.length
			dist := sym.distance
			if dist > 0x100 {
				length--
				if dist > 0x2000 {
					length--
					if dist > 0x40000 {
						length--
					}
				}
			}
			slot, _, _ := lengthToSlot(length)
			if 262+slot < r5NC {
				ldFreq[262+slot]++
			}
			distSlot, _, _, _ := distanceToSlot(dist)
			ddFreq[distSlot]++
			if distNeedsLDC(distSlot) {
				_, _, _, ldcSym := distanceToSlot(dist)
				lddFreq[ldcSym]++
			}
		}
	}

	// Ensure at least one literal and at least one distance symbol used
	// (avoid empty tables which cause invalid code assignments)
	for i := range ldFreq {
		if ldFreq[i] == 0 && i < 256 {
			// Give each literal a minimal count to avoid zero-length codes
			// for symbols we might emit. Actually we don't need all literals.
			break
		}
	}

	// Build Huffman code lengths
	ldLens := buildHuffmanLengths(ldFreq[:], r5MaxCodeLen)
	ddLens := buildHuffmanLengths(ddFreq[:], r5MaxCodeLen)
	lddLens := buildHuffmanLengths(lddFreq[:], r5MaxCodeLen)
	rdLens := buildHuffmanLengths(rdFreq[:], r5MaxCodeLen)

	// If no distances were used, give slot 0 a code so the table isn't empty
	if ddFreq[0] == 0 && !anyNonZero(ddFreq[:]) {
		ddLens[0] = 1
	}
	if lddFreq[0] == 0 && !anyNonZero(lddFreq[:]) {
		lddLens[0] = 1
	}
	if rdFreq[0] == 0 && !anyNonZero(rdFreq[:]) {
		rdLens[0] = 1
	}

	// Build canonical codes
	ldCodes := makeCanonicalCodes(ldLens)
	ddCodes := makeCanonicalCodes(ddLens)
	lddCodes := makeCanonicalCodes(lddLens)
	rdCodes := makeCanonicalCodes(rdLens)
	_ = rdCodes // only used if repeat-distance symbols are emitted

	// Build full table for BC pre-coding
	fullTable := make([]byte, r5HuffTableSize)
	for i, l := range ldLens {
		fullTable[i] = byte(l)
	}
	for i, l := range ddLens {
		fullTable[r5NC+i] = byte(l)
	}
	for i, l := range lddLens {
		fullTable[r5NC+r5DCB+i] = byte(l)
	}
	for i, l := range rdLens {
		fullTable[r5NC+r5DCB+r5LDC+i] = byte(l)
	}

	// Encode the full table using pre-codes
	rle := rleEncodeTable(fullTable)
	var bcFreq [r5BC]int
	for _, e := range rle {
		bcFreq[e.sym]++
	}
	bcLens := buildHuffmanLengths(bcFreq[:], r5MaxCodeLen)
	// Ensure all used BC symbols have valid codes
	for i, f := range bcFreq {
		if f > 0 && bcLens[i] == 0 {
			bcLens[i] = 1
		}
	}
	bcCodes := makeCanonicalCodes(bcLens)

	// Write block content to bit writer
	var bw bitWriter

	// Write BC pre-code table (20 × 4 bits, with run-of-zeros)
	writeBCTable(&bw, bcLens)

	// Write main Huffman table using BC codes
	for _, e := range rle {
		bw.writeBits(uint64(bcCodes[e.sym]), bcLens[e.sym])
		if e.extraLen > 0 {
			bw.writeBits(uint64(e.extra), e.extraLen)
		}
	}

	// Write LZ data
	for _, sym := range symbols {
		if !sym.isMatch {
			l := int(sym.literal)
			bw.writeBits(uint64(ldCodes[l]), ldLens[l])
		} else {
			dist := sym.distance
			length := sym.length
			// Adjust encoded length for distance
			encLen := length
			if dist > 0x100 {
				encLen--
				if dist > 0x2000 {
					encLen--
					if dist > 0x40000 {
						encLen--
					}
				}
			}

			slot, extraLen, extraVal := lengthToSlot(encLen)
			ldSym := 262 + slot
			bw.writeBits(uint64(ldCodes[ldSym]), ldLens[ldSym])
			if extraLen > 0 {
				bw.writeBits(uint64(extraVal), extraLen)
			}

			distSlot, dbitsHigh, highVal, ldcSym := distanceToSlot(dist)
			bw.writeBits(uint64(ddCodes[distSlot]), ddLens[distSlot])
			if dbitsHigh > 0 {
				bw.writeBits(uint64(highVal), dbitsHigh)
			}
			if distNeedsLDC(distSlot) {
				bw.writeBits(uint64(lddCodes[ldcSym]), lddLens[ldcSym])
			}
		}
	}

	compressed, lastBits := bw.flush()

	// Build block header
	blockSize := len(compressed)
	if blockSize == 0 {
		return nil
	}

	// Determine ByteCount for block size field
	byteCount := 1
	if blockSize >= 256 {
		byteCount = 2
	}
	if blockSize >= 65536 {
		byteCount = 3
	}
	// byteCount must be 1, 2, or 3 (value 4 is invalid)

	// BlockFlags: bits 0-2 = blockBitSize-1, bits 3-4 = byteCount-1,
	//             bit 6 = lastBlock, bit 7 = tablePresent
	blockFlags := byte(0x40 | 0x80) // lastBlock | tablePresent
	blockFlags |= byte(byteCount-1) << 3
	blockFlags |= byte(lastBits - 1)

	checkSum := byte(0x5a ^ blockFlags ^ byte(blockSize) ^ byte(blockSize>>8) ^ byte(blockSize>>16))

	header := []byte{blockFlags, checkSum}
	for i := 0; i < byteCount; i++ {
		header = append(header, byte(blockSize>>(i*8)))
	}

	result := append(header, compressed...)

	// Only use compression if it's actually smaller
	if len(result) >= len(data) {
		return nil
	}
	return result
}

// anyNonZero returns true if any element in the slice is > 0.
func anyNonZero(a []int) bool {
	for _, v := range a {
		if v > 0 {
			return true
		}
	}
	return false
}
