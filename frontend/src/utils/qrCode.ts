// Zero-dependency ISO/IEC 18004 QR matrix builder (Versions 1-6, ECC Level M).
import { encodeQrCodewords, QR_VERSIONS_M } from './qrEcc'

function formatBitsM(mask: number): number {
  let rem = mask << 10
  for (let i = 4; i >= 0; i--) {
    if ((rem >> (i + 10)) & 1) rem ^= 0x537 << i
  }
  return ((mask << 10) | rem) ^ 0x5412
}

const MASKS: ((r: number, c: number) => boolean)[] = [
  (r, c) => (r + c) % 2 === 0,
  (r) => r % 2 === 0,
  (_, c) => c % 3 === 0,
  (r, c) => (r + c) % 3 === 0,
  (r, c) => (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0,
  (r, c) => ((r * c) % 2) + ((r * c) % 3) === 0,
  (r, c) => (((r * c) % 2) + ((r * c) % 3)) % 2 === 0,
  (r, c) => (((r + c) % 2) + ((r * c) % 3)) % 2 === 0,
]

function placeFormat(m: number[][], size: number, mask: number) {
  const fmt = formatBitsM(mask)
  const bit = (i: number) => (fmt >> i) & 1
  for (let i = 0; i <= 5; i++) m[8][i] = bit(14 - i)
  m[8][7] = bit(8)
  m[8][8] = bit(7)
  m[7][8] = bit(6)
  for (let i = 0; i <= 5; i++) m[5 - i][8] = bit(5 - i)
  for (let i = 0; i <= 6; i++) m[size - 1 - i][8] = bit(14 - i)
  m[size - 8][8] = 1
  for (let i = 0; i <= 7; i++) m[8][size - 8 + i] = bit(7 - i)
}

function penaltyScore(m: number[][], size: number): number {
  let score = 0
  let dark = 0
  for (let r = 0; r < size; r++) {
    let run = 1
    for (let c = 0; c < size; c++) {
      if (m[r][c]) dark++
      if (c > 0 && m[r][c] === m[r][c - 1]) {
        if (++run === 5) score += 3
        else if (run > 5) score += 1
      } else run = 1
      if (r < size - 1 && c < size - 1) {
        const v = m[r][c]
        if (v === m[r][c + 1] && v === m[r + 1][c] && v === m[r + 1][c + 1]) score += 3
      }
    }
  }
  for (let c = 0; c < size; c++) {
    let run = 1
    for (let r = 1; r < size; r++) {
      if (m[r][c] === m[r - 1][c]) {
        if (++run === 5) score += 3
        else if (run > 5) score += 1
      } else run = 1
    }
  }
  const pct = Math.abs((dark * 100) / (size * size) - 50)
  return score + Math.floor(pct / 5) * 10
}

export function generateQrMatrix(text: string): boolean[][] {
  const bytes = new TextEncoder().encode(text)
  const spec = QR_VERSIONS_M.find((v) => bytes.length <= v.dataCap - 2)
  if (!spec) throw new Error('QR payload exceeds Version 6 capacity')
  const { size } = spec
  const base: number[][] = Array.from({ length: size }, () => Array(size).fill(-1))
  const reserved: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false))
  const setFunc = (r: number, c: number, v: number) => {
    if (r >= 0 && r < size && c >= 0 && c < size) {
      base[r][c] = v
      reserved[r][c] = true
    }
  }
  const drawFinder = (tr: number, tc: number) => {
    for (let dr = -1; dr <= 7; dr++) {
      for (let dc = -1; dc <= 7; dc++) {
        const inside = dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6
        const dark = inside && (dr === 0 || dr === 6 || dc === 0 || dc === 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4))
        setFunc(tr + dr, tc + dc, dark ? 1 : 0)
      }
    }
  }
  drawFinder(0, 0)
  drawFinder(0, size - 7)
  drawFinder(size - 7, 0)
  for (let i = 8; i < size - 8; i++) {
    setFunc(6, i, i % 2 === 0 ? 1 : 0)
    setFunc(i, 6, i % 2 === 0 ? 1 : 0)
  }
  for (const ar of spec.align) {
    for (const ac of spec.align) {
      if (reserved[ar][ac]) continue
      for (let dr = -2; dr <= 2; dr++) {
        for (let dc = -2; dc <= 2; dc++) {
          setFunc(ar + dr, ac + dc, Math.max(Math.abs(dr), Math.abs(dc)) !== 1 ? 1 : 0)
        }
      }
    }
  }
  for (let i = 0; i <= 8; i++) {
    if (!reserved[8][i]) reserved[8][i] = true
    if (!reserved[i][8]) reserved[i][8] = true
  }
  for (let i = 0; i < 8; i++) {
    reserved[8][size - 1 - i] = true
    reserved[size - 1 - i][8] = true
  }
  const codewords = encodeQrCodewords(bytes, spec)
  let bitIdx = 0
  let up = true
  for (let col = size - 1; col > 0; col -= 2) {
    if (col === 6) col--
    for (let step = 0; step < size; step++) {
      const r = up ? size - 1 - step : step
      for (let dx = 0; dx < 2; dx++) {
        const c = col - dx
        if (reserved[r][c]) continue
        const byteIdx = bitIdx >> 3
        base[r][c] = byteIdx < codewords.length ? (codewords[byteIdx] >> (7 - (bitIdx & 7))) & 1 : 0
        bitIdx++
      }
    }
    up = !up
  }
  let bestScore = Infinity
  let bestGrid = base
  for (let mk = 0; mk < 8; mk++) {
    const fn = MASKS[mk]
    const g = base.map((row, r) => row.map((v, c) => (!reserved[r][c] && fn(r, c) ? v ^ 1 : v)))
    placeFormat(g, size, mk)
    const sc = penaltyScore(g, size)
    if (sc < bestScore) {
      bestScore = sc
      bestGrid = g
    }
  }
  return bestGrid.map((row) => row.map((v) => v === 1))
}
