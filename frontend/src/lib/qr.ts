// A small, dependency-free QR-code encoder — just enough to render the short
// HomeKit setup payload ("X-HM://…", ~20 ASCII chars) as a scannable code without
// pulling in an npm library (the project keeps its dependency surface minimal).
//
// It encodes in byte mode at error-correction level M, auto-selecting the smallest
// version (1–6) that fits, and returns a square boolean matrix (true = dark).
// Implements the pieces the spec requires: GF(256) Reed–Solomon ECC, block
// interleaving, function-pattern placement, all eight data masks with penalty
// scoring, and BCH format info. Versions ≥7 also need a version-information block,
// which is out of scope: level M version 6 already holds 108 bytes, far more than
// the ~20-byte HomeKit setup URI this renders, so encode throws rather than emit an
// unscannable code past that.

// --- GF(256) arithmetic (primitive polynomial 0x11d) -----------------------
const EXP = new Uint8Array(512);
const LOG = new Uint8Array(256);
(() => {
  let x = 1;
  for (let i = 0; i < 255; i++) {
    EXP[i] = x;
    LOG[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
  }
  for (let i = 255; i < 512; i++) EXP[i] = EXP[i - 255];
})();

const gfMul = (a, b) => (a === 0 || b === 0 ? 0 : EXP[LOG[a] + LOG[b]]);

// Generator polynomial for `degree` error-correction codewords.
function rsGenerator(degree) {
  let poly = [1];
  for (let d = 0; d < degree; d++) {
    const next = new Array(poly.length + 1).fill(0);
    for (let i = 0; i < poly.length; i++) {
      next[i] ^= gfMul(poly[i], EXP[d]);
      next[i + 1] ^= poly[i];
    }
    poly = next;
  }
  return poly;
}

// Reed–Solomon error-correction codewords for one data block. rsGenerator returns
// coefficients constant-term-first; the division wants them highest-degree-first,
// and drops the monic leading 1, leaving `ecLen` divisor coefficients.
function rsEncode(data, ecLen) {
  const gen = rsGenerator(ecLen).reverse().slice(1);
  const res = new Array(ecLen).fill(0);
  for (const byte of data) {
    const factor = byte ^ res[0];
    res.shift();
    res.push(0);
    for (let i = 0; i < ecLen; i++) res[i] ^= gfMul(gen[i], factor);
  }
  return res;
}

// --- Version tables (error-correction level M) -----------------------------
// [ecPerBlock, group1Blocks, group1DataCw, group2Blocks, group2DataCw]
const MAX_VERSION = 6;
const EC_M = {
  1: [10, 1, 16, 0, 0],
  2: [16, 1, 28, 0, 0],
  3: [26, 1, 44, 0, 0],
  4: [18, 2, 32, 0, 0],
  5: [24, 2, 43, 0, 0],
  6: [16, 4, 27, 0, 0],
};

// Alignment-pattern centre coordinates per version.
const ALIGN = {
  1: [], 2: [6, 18], 3: [6, 22], 4: [6, 26], 5: [6, 30], 6: [6, 34],
};

const dataCapacity = (v) => {
  const [, g1, d1, g2, d2] = EC_M[v];
  return g1 * d1 + g2 * d2;
};

// --- Bit buffer ------------------------------------------------------------
function bits() {
  const arr = [];
  return {
    put(val, len) { for (let i = len - 1; i >= 0; i--) arr.push((val >> i) & 1); },
    get: () => arr,
  };
}

// Byte-mode data codewords for `str`, padded to the version's capacity.
function encodeData(str, version) {
  const buf = bits();
  buf.put(0b0100, 4);          // byte mode
  buf.put(str.length, 8);      // char count (8 bits for versions 1–9)
  for (let i = 0; i < str.length; i++) buf.put(str.charCodeAt(i) & 0xff, 8);

  const capBits = dataCapacity(version) * 8;
  const b = buf.get();
  for (let i = 0; i < 4 && b.length < capBits; i++) b.push(0); // terminator
  while (b.length % 8 !== 0) b.push(0);

  const cw = [];
  for (let i = 0; i < b.length; i += 8) {
    let byte = 0;
    for (let j = 0; j < 8; j++) byte = (byte << 1) | b[i + j];
    cw.push(byte);
  }
  const pad = [0xec, 0x11];
  for (let i = 0; cw.length < dataCapacity(version); i++) cw.push(pad[i % 2]);
  return cw;
}

// Interleave data + error-correction codewords across blocks per the spec.
function interleave(dataCw, version) {
  const [ecLen, g1, d1, g2, d2] = EC_M[version];
  const blocks = [];
  let pos = 0;
  for (let i = 0; i < g1; i++) { blocks.push(dataCw.slice(pos, pos + d1)); pos += d1; }
  for (let i = 0; i < g2; i++) { blocks.push(dataCw.slice(pos, pos + d2)); pos += d2; }
  const ecBlocks = blocks.map((b) => rsEncode(b, ecLen));

  const out = [];
  const maxData = Math.max(d1, d2);
  for (let i = 0; i < maxData; i++) for (const b of blocks) if (i < b.length) out.push(b[i]);
  for (let i = 0; i < ecLen; i++) for (const e of ecBlocks) out.push(e[i]);
  return out;
}

// --- Matrix construction ---------------------------------------------------
function newMatrix(size) {
  const m = [];
  for (let i = 0; i < size; i++) m.push(new Array(size).fill(null)); // null = free
  return m;
}

function placeFinder(m, r, c) {
  for (let dy = -1; dy <= 7; dy++) {
    for (let dx = -1; dx <= 7; dx++) {
      const y = r + dy, x = c + dx;
      if (y < 0 || x < 0 || y >= m.length || x >= m.length) continue;
      const on = dy >= 0 && dy <= 6 && dx >= 0 && dx <= 6 &&
        (dx === 0 || dx === 6 || dy === 0 || dy === 6 || (dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4));
      m[y][x] = on;
    }
  }
}

function placeFunctionPatterns(m, version) {
  const size = m.length;
  placeFinder(m, 0, 0);
  placeFinder(m, 0, size - 7);
  placeFinder(m, size - 7, 0);

  for (let i = 8; i < size - 8; i++) {
    const on = i % 2 === 0;
    if (m[6][i] === null) m[6][i] = on;
    if (m[i][6] === null) m[i][6] = on;
  }

  const centers = ALIGN[version];
  const last = centers[centers.length - 1];
  for (const r of centers) {
    for (const c of centers) {
      // Skip the three that would sit on a finder pattern; the rest are placed even
      // where they cross the timing line (the alignment pattern takes precedence).
      if ((r === 6 && c === 6) || (r === 6 && c === last) || (r === last && c === 6)) continue;
      for (let dy = -2; dy <= 2; dy++) {
        for (let dx = -2; dx <= 2; dx++) {
          m[r + dy][c + dx] = Math.max(Math.abs(dy), Math.abs(dx)) !== 1;
        }
      }
    }
  }

  m[size - 8][8] = true; // dark module
}

// Reserve the format-info modules so data placement skips them.
function reserveFormat(m) {
  const size = m.length;
  for (let i = 0; i < 9; i++) {
    if (m[8][i] === null) m[8][i] = false;
    if (m[i][8] === null) m[i][8] = false;
  }
  for (let i = 0; i < 8; i++) {
    if (m[8][size - 1 - i] === null) m[8][size - 1 - i] = false;
    if (m[size - 1 - i][8] === null) m[size - 1 - i][8] = false;
  }
}

function isFunction(reserved, r, c) { return reserved[r][c]; }

// Zig-zag data placement (bottom-right upward), skipping function modules.
function placeData(m, reserved, codewords) {
  const size = m.length;
  const bitsArr = [];
  for (const cw of codewords) for (let i = 7; i >= 0; i--) bitsArr.push((cw >> i) & 1);

  let idx = 0, up = true;
  for (let col = size - 1; col > 0; col -= 2) {
    if (col === 6) col--; // skip vertical timing column
    for (let i = 0; i < size; i++) {
      const row = up ? size - 1 - i : i;
      for (let k = 0; k < 2; k++) {
        const c = col - k;
        if (!isFunction(reserved, row, c)) {
          m[row][c] = idx < bitsArr.length ? bitsArr[idx] === 1 : false;
          idx++;
        }
      }
    }
    up = !up;
  }
}

const MASKS = [
  (r, c) => (r + c) % 2 === 0,
  (r) => r % 2 === 0,
  (r, c) => c % 3 === 0,
  (r, c) => (r + c) % 3 === 0,
  (r, c) => (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0,
  (r, c) => ((r * c) % 2) + ((r * c) % 3) === 0,
  (r, c) => (((r * c) % 2) + ((r * c) % 3)) % 2 === 0,
  (r, c) => (((r + c) % 2) + ((r * c) % 3)) % 2 === 0,
];

function applyMask(m, reserved, mask) {
  const fn = MASKS[mask];
  const size = m.length;
  const out = m.map((row) => row.slice());
  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      if (!isFunction(reserved, r, c) && fn(r, c)) out[r][c] = !out[r][c];
    }
  }
  return out;
}

// Penalty score (lower is better) used to pick the least-noisy mask.
function penalty(m) {
  const size = m.length;
  let score = 0;
  // Rule 1: runs of 5+ same-colour modules per row/column.
  for (let r = 0; r < size; r++) {
    for (const line of [m[r], m.map((row) => row[r])]) {
      let run = 1;
      for (let i = 1; i < size; i++) {
        if (line[i] === line[i - 1]) { run++; if (run === 5) score += 3; else if (run > 5) score++; }
        else run = 1;
      }
    }
  }
  // Rule 2: 2x2 blocks of the same colour.
  for (let r = 0; r < size - 1; r++) {
    for (let c = 0; c < size - 1; c++) {
      const v = m[r][c];
      if (v === m[r][c + 1] && v === m[r + 1][c] && v === m[r + 1][c + 1]) score += 3;
    }
  }
  // Rule 3: finder-like 1:1:3:1:1 patterns.
  const pat1 = [true, false, true, true, true, false, true, false, false, false, false];
  const pat2 = [false, false, false, false, true, false, true, true, true, false, true];
  const match = (line, i, pat) => pat.every((v, k) => line[i + k] === v);
  for (let r = 0; r < size; r++) {
    const rowLine = m[r];
    const colLine = m.map((row) => row[r]);
    for (let i = 0; i + 11 <= size; i++) {
      if (match(rowLine, i, pat1) || match(rowLine, i, pat2)) score += 40;
      if (match(colLine, i, pat1) || match(colLine, i, pat2)) score += 40;
    }
  }
  // Rule 4: deviation from 50% dark.
  let dark = 0;
  for (let r = 0; r < size; r++) for (let c = 0; c < size; c++) if (m[r][c]) dark++;
  const pct = (dark * 100) / (size * size);
  score += Math.floor(Math.abs(pct - 50) / 5) * 10;
  return score;
}

// Format info: 2-bit EC level (M = 0) + 3-bit mask, BCH-protected, then masked.
function formatBits(mask) {
  const data = (0b00 << 3) | mask; // level M
  let rem = data << 10;
  const g = 0b10100110111;
  for (let i = 4; i >= 0; i--) if ((rem >> (10 + i)) & 1) rem ^= g << i;
  return ((data << 10) | rem) ^ 0b101010000010010;
}

function placeFormat(m, mask) {
  const size = m.length;
  const raw = formatBits(mask);
  let fmt = 0; // the 15 bits are placed most-significant-first
  for (let i = 0; i < 15; i++) fmt = (fmt << 1) | ((raw >> i) & 1);
  const bit = (i) => ((fmt >> i) & 1) === 1;
  for (let i = 0; i <= 5; i++) m[8][i] = bit(i);
  m[8][7] = bit(6);
  m[8][8] = bit(7);
  m[7][8] = bit(8);
  for (let i = 9; i <= 14; i++) m[14 - i][8] = bit(i);
  for (let i = 0; i <= 7; i++) m[size - 1 - i][8] = bit(i);
  for (let i = 8; i <= 14; i++) m[8][size - 15 + i] = bit(i);
  m[size - 8][8] = true; // dark module — overwrites the bit-7 cell just written
}

// encode returns a { size, modules } matrix (modules[r][c] === true → dark).
export function encode(str) {
  let version = 1;
  const need = Math.ceil((4 + 8 + 8 * str.length) / 8);
  while (version < MAX_VERSION && dataCapacity(version) < need) version++;
  if (dataCapacity(version) < need) {
    throw new Error(`qr: payload too long (${str.length} bytes exceeds version ${MAX_VERSION})`);
  }

  const codewords = interleave(encodeData(str, version), version);
  const size = 17 + 4 * version;

  const base = newMatrix(size);
  placeFunctionPatterns(base, version);
  reserveFormat(base);
  const reserved = base.map((row) => row.map((v) => v !== null));

  placeData(base, reserved, codewords);

  let best = null, bestScore = Infinity;
  for (let mask = 0; mask < 8; mask++) {
    const masked = applyMask(base, reserved, mask);
    placeFormat(masked, mask);
    const s = penalty(masked);
    if (s < bestScore) { bestScore = s; best = masked; }
  }
  return { size, modules: best.map((row) => row.map((v) => v === true)) };
}
