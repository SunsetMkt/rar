"""RAR 5.0 LZ77 + Huffman compressor.

Implements the RAR5 compression block format used by compression methods 1–5.
Method 0 (store) bypasses this module entirely.
"""

import heapq
import struct
from collections import defaultdict

# Huffman table sizes
NC = 306       # literal/length codes (0–255 literals, 256 EOB, 257–261 reserved, 262–305 length slots)
DCB = 64       # distance codes
LDC = 16       # low-distance codes (low 4 bits of large distances)
RC = 44        # repeat/length codes
BC = 20        # base (meta) codes
HUFF_TABLE_SIZEB = NC + DCB + LDC + RC   # 430

MAX_LZ_MATCH = 0x1001          # maximum match length (matches compress.hpp)
MAX_HASH_CHAIN_LENGTH = 32     # how many hash-chain candidates to inspect per position


# ---------------------------------------------------------------------------
# Bit writer (MSB-first within each byte)
# ---------------------------------------------------------------------------

class BitWriter:
    def __init__(self):
        self.data = bytearray()
        self.current_byte = 0
        self.bit_pos = 0   # number of bits already written into current_byte (0 = fresh)

    def write_bits(self, value: int, nbits: int):
        for i in range(nbits - 1, -1, -1):
            bit = (value >> i) & 1
            self.current_byte |= bit << (7 - self.bit_pos)
            self.bit_pos += 1
            if self.bit_pos == 8:
                self.data.append(self.current_byte)
                self.current_byte = 0
                self.bit_pos = 0

    def flush(self):
        """Flush any pending bits.  Returns (bytes, bits_used_in_last_byte).
        bits_used_in_last_byte is 1–8; 8 means the last byte is fully used.
        """
        if self.bit_pos > 0:
            self.data.append(self.current_byte)
            return bytes(self.data), self.bit_pos
        # Every byte was full.
        return bytes(self.data), 8


# ---------------------------------------------------------------------------
# Huffman building
# ---------------------------------------------------------------------------

def build_huffman_lengths(freqs, num_symbols: int, max_bits: int = 15):
    """Build canonical Huffman bit-lengths from frequency table.

    Returns a list of length *num_symbols* where entry[sym] = bit length
    (0 means the symbol is not used / has no code).
    """
    lengths = [0] * num_symbols
    active = [(freq, sym) for sym, freq in enumerate(freqs) if freq > 0]

    if not active:
        return lengths

    if len(active) == 1:
        lengths[active[0][1]] = 1
        return lengths

    # Build a Huffman tree with a min-heap.
    # Each heap element: (freq, node_id, left_child, right_child)
    # Leaf nodes have left_child=None, right_child=None, and node_id = symbol.
    heap = [(f, s, None, None) for f, s in active]
    heapq.heapify(heap)

    counter = 0
    while len(heap) > 1:
        f1, id1, l1, r1 = heapq.heappop(heap)
        f2, id2, l2, r2 = heapq.heappop(heap)
        counter -= 1   # unique internal node id (negative to avoid clash with sym ids)
        heapq.heappush(heap, (f1 + f2, counter, (f1, id1, l1, r1), (f2, id2, l2, r2)))

    def assign_depths(node, depth):
        freq, sym_or_id, left, right = node
        if left is None and right is None:
            # Leaf — sym_or_id is the actual symbol
            lengths[sym_or_id] = depth
        else:
            assign_depths(left, depth + 1)
            assign_depths(right, depth + 1)

    assign_depths(heap[0], 0)

    # Limit to max_bits using the classic "push up" approach:
    # cap lengths, then fix the Kraft inequality by lengthening the
    # cheapest (shortest) codes until the tree is valid.
    capped = any(l > max_bits for l in lengths)
    if capped:
        for i in range(len(lengths)):
            if lengths[i] > max_bits:
                lengths[i] = max_bits

        # Rebalance: compute excess Kraft sum and reduce it by lengthening codes.
        while True:
            kraft = sum(1 << (max_bits - l) for l in lengths if l > 0)
            total = 1 << max_bits
            if kraft <= total:
                break
            # Find the shortest non-zero length and increase it by 1
            min_len = min(l for l in lengths if l > 0)
            for i in range(len(lengths)):
                if lengths[i] == min_len:
                    if lengths[i] < max_bits:
                        lengths[i] += 1
                    break

    return lengths


def build_canonical_codes(lengths):
    """Assign canonical Huffman codes from a lengths list.

    Returns a dict {symbol: code_int}.  Symbols with length 0 are excluded.
    """
    syms = sorted((l, s) for s, l in enumerate(lengths) if l > 0)
    code = 0
    prev_len = 0
    result = {}
    for l, s in syms:
        code <<= (l - prev_len)
        result[s] = code
        code += 1
        prev_len = l
    return result


# ---------------------------------------------------------------------------
# Length / distance slot tables
# ---------------------------------------------------------------------------

def length_to_slot_and_extra(length: int):
    """Map a stored match length (>= 2) to (slot, extra_value, extra_bits).

    Slot 0–7: LBits = 0, base = slot + 2
    Slot >= 8: LBits = slot//4 - 1, base = 2 + ((4 | (slot & 3)) << LBits)
    """
    if length <= 9:
        return length - 2, 0, 0
    for slot in range(8, RC):
        lbits = slot // 4 - 1
        base = 2 + ((4 | (slot & 3)) << lbits)
        top = base + (1 << lbits)
        if length < top:
            return slot, length - base, lbits
    # Clamp to last slot
    slot = RC - 1
    lbits = slot // 4 - 1
    base = 2 + ((4 | (slot & 3)) << lbits)
    return slot, length - base, lbits


def dist_to_slot_and_extra(dist_0indexed: int):
    """Map a 0-indexed distance to distance encoding components.

    The RAR5 decoder computes: Distance (1-indexed) = 1 + base + extra,
    where for slot < 4:  base = slot, extra = 0
    and  for slot >= 4:  base = (2|(slot&1)) << (slot//2-1), extra from bits.

    We receive dist_0indexed = Distance - 1, so the base (for slot >= 4) is:
        base = (2|(slot&1)) << (slot//2-1)   (no +1)

    For dbits >= 4: split extra into (extra >> 4) plain bits + LDD symbol.
    For 0 < dbits < 4: write extra as plain bits.
    For dbits == 0: no extra bits.

    Returns (slot, high_extra, high_bits, ldd_sym_or_minus1)
    """
    dist = dist_0indexed
    if dist < 4:
        return dist, 0, 0, -1

    for slot in range(4, DCB):
        dbits = slot // 2 - 1
        base = (2 | (slot & 1)) << dbits
        top = base + (1 << dbits)
        if dist < top:
            extra = dist - base
            if dbits >= 4:
                high_bits = dbits - 4
                return slot, extra >> 4, high_bits, extra & 0xF
            else:
                return slot, extra, dbits, -1

    # Clamp to last slot
    slot = DCB - 1
    dbits = slot // 2 - 1
    base = (2 | (slot & 1)) << dbits
    extra = dist - base
    if extra < 0:
        extra = 0
    if dbits >= 4:
        high_bits = dbits - 4
        return slot, extra >> 4, high_bits, extra & 0xF
    return slot, extra, dbits, -1


def distance_bonus(dist_0indexed: int) -> int:
    """Extra match-length bonus from RAR5 for large distances (0-indexed).

    The unrar decoder checks 1-indexed Distance:
      if Distance > 0x100: Length++   → dist_0indexed >= 0x100
      if Distance > 0x2000: Length++  → dist_0indexed >= 0x2000
      if Distance > 0x40000: Length++ → dist_0indexed >= 0x40000
    """
    bonus = 0
    if dist_0indexed >= 0x100:
        bonus += 1
    if dist_0indexed >= 0x2000:
        bonus += 1
    if dist_0indexed >= 0x40000:
        bonus += 1
    return bonus


# ---------------------------------------------------------------------------
# LZ77 parser
# ---------------------------------------------------------------------------

def lz_parse(data: bytes, window_size: int = 0x80000, min_match: int = 3):
    """Simple hash-chain LZ77 parser.

    Returns list of ('lit', byte) or ('match', length, dist_1indexed) tokens.
    """
    n = len(data)
    if n == 0:
        return []

    result = []
    pos = 0
    hash_table = defaultdict(list)   # 3-byte hash → positions

    def h3(p):
        return (data[p] << 16) | (data[p + 1] << 8) | data[p + 2]

    while pos < n:
        best_len = 0
        best_dist = 0

        if pos + 2 < n:
            key = h3(pos)
            candidates = hash_table.get(key, [])

            for cand in reversed(candidates[-MAX_HASH_CHAIN_LENGTH:]):
                dist = pos - cand
                if dist <= 0 or dist > window_size:
                    continue
                # Extend match
                max_len = min(n - pos, MAX_LZ_MATCH)
                ml = 0
                while ml < max_len and data[pos + ml] == data[cand + ml]:
                    ml += 1
                if ml > best_len:
                    best_len = ml
                    best_dist = dist
                    if best_len >= 258:
                        break

            hash_table[key].append(pos)

        if best_len >= min_match:
            dist_0 = best_dist - 1
            bonus = distance_bonus(dist_0)
            stored_len = best_len - bonus
            if stored_len >= 2:
                result.append(('match', best_len, best_dist))
                for i in range(1, best_len):
                    np = pos + i
                    if np + 2 < n:
                        hash_table[h3(np)].append(np)
                pos += best_len
                continue

        result.append(('lit', data[pos]))
        pos += 1

    return result


# ---------------------------------------------------------------------------
# Meta-symbol (Huffman table) encoder
# ---------------------------------------------------------------------------

def encode_huffman_tables(all_lengths):
    """RLE-encode 430 Huffman bit-lengths into meta-symbols.

    Returns (meta_syms, meta_extras) where meta_extras[i] = (value, bits).
    Uses only literal (0–15) and zero-run (18, 19) symbols to avoid the
    "repeat previous at index 0" edge case.
    """
    meta_syms = []
    meta_extras = []
    i = 0
    n = len(all_lengths)

    while i < n:
        val = all_lengths[i]
        if val == 0:
            # Count zero run
            run = 0
            while i + run < n and all_lengths[i + run] == 0:
                run += 1
            while run > 0:
                if run >= 11:
                    take = min(run, 138)
                    meta_syms.append(19)
                    meta_extras.append((take - 11, 7))
                    run -= take
                    i += take
                elif run >= 3:
                    take = min(run, 10)
                    meta_syms.append(18)
                    meta_extras.append((take - 3, 3))
                    run -= take
                    i += take
                else:
                    meta_syms.append(0)
                    meta_extras.append((0, 0))
                    run -= 1
                    i += 1
        else:
            meta_syms.append(val)
            meta_extras.append((0, 0))
            i += 1

    return meta_syms, meta_extras


# ---------------------------------------------------------------------------
# Block compressor
# ---------------------------------------------------------------------------

def compress_block(data: bytes) -> bytes:
    """Compress *data* into a single RAR5 compression block."""

    # --- LZ parse ---------------------------------------------------------
    tokens = lz_parse(data)

    # --- Frequency counting and encoded sequence --------------------------
    ld_freqs = [0] * NC
    dd_freqs = [0] * DCB
    ldd_freqs = [0] * LDC
    rd_freqs = [0] * RC

    encoded_seq = []   # list of action tuples

    for token in tokens:
        if token[0] == 'lit':
            sym = token[1]
            ld_freqs[sym] += 1
            encoded_seq.append(('ld', sym))
        else:
            _, length, dist_1indexed = token
            dist_0 = dist_1indexed - 1
            bonus = distance_bonus(dist_0)
            stored_len = max(length - bonus, 2)

            len_slot, len_extra, len_bits = length_to_slot_and_extra(stored_len)
            ld_sym = 262 + len_slot
            if ld_sym >= NC:
                ld_sym = NC - 1
            ld_freqs[ld_sym] += 1
            encoded_seq.append(('ld', ld_sym))

            if len_bits > 0:
                encoded_seq.append(('extra', len_extra, len_bits))

            dist_slot, high_extra, high_bits, ldd_sym = dist_to_slot_and_extra(dist_0)
            if dist_slot >= DCB:
                dist_slot = DCB - 1
            dd_freqs[dist_slot] += 1

            if ldd_sym >= 0:
                ldd_sym = min(ldd_sym, LDC - 1)
                ldd_freqs[ldd_sym] += 1
                encoded_seq.append(('dd', dist_slot, high_extra, high_bits, ldd_sym))
            else:
                encoded_seq.append(('dd_simple', dist_slot, high_extra, high_bits))

    # Ensure each table has at least one symbol so canonical codes work
    if all(f == 0 for f in ld_freqs):
        ld_freqs[0] = 1
    if all(f == 0 for f in dd_freqs):
        dd_freqs[0] = 1
    if all(f == 0 for f in ldd_freqs):
        ldd_freqs[0] = 1
    if all(f == 0 for f in rd_freqs):
        rd_freqs[0] = 1

    # --- Build Huffman tables ---------------------------------------------
    ld_lengths = build_huffman_lengths(ld_freqs, NC)
    dd_lengths = build_huffman_lengths(dd_freqs, DCB)
    ldd_lengths = build_huffman_lengths(ldd_freqs, LDC)
    rd_lengths = build_huffman_lengths(rd_freqs, RC)

    ld_codes = build_canonical_codes(ld_lengths)
    dd_codes = build_canonical_codes(dd_lengths)
    ldd_codes = build_canonical_codes(ldd_lengths)

    # --- Meta-encode the 430 lengths --------------------------------------
    all_lengths_list = ld_lengths + dd_lengths + ldd_lengths + rd_lengths
    assert len(all_lengths_list) == HUFF_TABLE_SIZEB

    meta_syms, meta_extras = encode_huffman_tables(all_lengths_list)

    # --- Base (meta) table ------------------------------------------------
    bc_freqs = [0] * BC
    for sym in meta_syms:
        if sym < BC:
            bc_freqs[sym] += 1

    # Need at least 2 distinct symbols for Huffman to work
    if sum(1 for f in bc_freqs if f > 0) < 2:
        for i in range(BC):
            if bc_freqs[i] == 0:
                bc_freqs[i] = 1
                break

    bc_lengths = build_huffman_lengths(bc_freqs, BC)
    bc_codes = build_canonical_codes(bc_lengths)

    # Verify every meta_sym has a code in bc (defensive)
    for sym in meta_syms:
        if sym >= BC:
            raise ValueError(f"meta_sym {sym} >= BC={BC}")
        if bc_lengths[sym] == 0:
            # This symbol appeared but Huffman gave it length 0 — fix by
            # ensuring its frequency is positive and rebuilding.
            bc_freqs[sym] += 1
            bc_lengths = build_huffman_lengths(bc_freqs, BC)
            bc_codes = build_canonical_codes(bc_lengths)

    # --- Write bit stream --------------------------------------------------
    bw = BitWriter()

    # 1. Base table: 20 × 4-bit lengths
    for i in range(BC):
        l = bc_lengths[i]
        bw.write_bits(l, 4)
        if l == 15:
            bw.write_bits(0, 4)   # ZeroCount = 0 → truly 15 bits

    # 2. Meta-encoded 430 lengths (using base table)
    for sym, (extra_val, extra_bits) in zip(meta_syms, meta_extras):
        bw.write_bits(bc_codes[sym], bc_lengths[sym])
        if extra_bits > 0:
            bw.write_bits(extra_val, extra_bits)

    # 3. Symbol stream
    for action in encoded_seq:
        kind = action[0]
        if kind == 'ld':
            sym = action[1]
            bw.write_bits(ld_codes[sym], ld_lengths[sym])
        elif kind == 'extra':
            bw.write_bits(action[1], action[2])
        elif kind == 'dd':
            _, dist_slot, high_extra, high_bits, ldd_sym = action
            bw.write_bits(dd_codes[dist_slot], dd_lengths[dist_slot])
            if high_bits > 0:
                bw.write_bits(high_extra, high_bits)
            bw.write_bits(ldd_codes[ldd_sym], ldd_lengths[ldd_sym])
        elif kind == 'dd_simple':
            _, dist_slot, extra, dbits = action
            bw.write_bits(dd_codes[dist_slot], dd_lengths[dist_slot])
            if dbits > 0:
                bw.write_bits(extra, dbits)

    compressed_bytes, last_byte_bits = bw.flush()

    # --- Build block header -----------------------------------------------
    block_size = len(compressed_bytes)

    if block_size < 256:
        byte_count = 1
    elif block_size < 65536:
        byte_count = 2
    else:
        byte_count = 3

    block_flags = (
        ((last_byte_bits - 1) & 0x7) |      # bits 0–2: valid bits in last byte minus 1
        ((byte_count - 1) << 3) |            # bits 3–4: ByteCount minus 1
        (1 << 6) |                           # bit 6: LastBlockInFile
        (1 << 7)                             # bit 7: TablePresent
    )

    checksum = (0x5A ^ block_flags
                ^ (block_size & 0xFF)
                ^ ((block_size >> 8) & 0xFF)
                ^ ((block_size >> 16) & 0xFF)) & 0xFF

    header = bytes([block_flags, checksum]) + block_size.to_bytes(byte_count, 'little')
    return header + compressed_bytes


def compress_rar5(data: bytes, method: int = 3) -> bytes:
    """Public entry point.  method 0 = store (handled in blocks.py); 1–5 compress."""
    if method == 0:
        return data
    return compress_block(data)
