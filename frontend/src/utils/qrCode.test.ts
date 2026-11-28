import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { QrCodeSvg } from '../components/QrCodeSvg'
import { formatBitsM, generateQrMatrix, versionInfoBits } from './qrCode'
import { GF_EXP, gfMul, QR_VERSIONS_M, qrDataCapacityCw, qrMaxByteCapacity } from './qrEcc'

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

// Independent ISO/IEC 18004 QR Level M decoder + Reed-Solomon syndrome verifier for round-trip testing.
function decodeAndVerifyQrLevelM(matrix: boolean[][]): {
  ver: number
  mask: number
  text: string
  bytes: Uint8Array
} {
  const size = matrix.length
  expect((size - 17) % 4).toBe(0)
  const ver = (size - 17) / 4
  const spec = QR_VERSIONS_M.find((s) => s.ver === ver)
  expect(spec).toBeDefined()
  if (!spec) throw new Error('missing spec')

  // Verify fixed dark module
  expect(matrix[size - 8][8]).toBe(true)

  // Read & cross-verify both copies of 15-bit Format Information
  let fmt1 = 0
  for (let i = 0; i <= 5; i++) fmt1 = (fmt1 << 1) | (matrix[8][i] ? 1 : 0)
  fmt1 = (fmt1 << 1) | (matrix[8][7] ? 1 : 0)
  fmt1 = (fmt1 << 1) | (matrix[8][8] ? 1 : 0)
  fmt1 = (fmt1 << 1) | (matrix[7][8] ? 1 : 0)
  for (let i = 5; i >= 0; i--) fmt1 = (fmt1 << 1) | (matrix[i][8] ? 1 : 0)

  let fmt2 = 0
  for (let i = 0; i <= 6; i++) fmt2 = (fmt2 << 1) | (matrix[size - 1 - i][8] ? 1 : 0)
  for (let i = 0; i <= 7; i++) fmt2 = (fmt2 << 1) | (matrix[8][size - 8 + i] ? 1 : 0)

  expect(fmt1).toBe(fmt2)
  const unmaskedFmt = fmt1 ^ 0x5412
  const ecIndicator = (unmaskedFmt >> 13) & 0b11
  expect(ecIndicator).toBe(0b00) // Level M = 00 in ISO 18004 Table 25
  const mask = (unmaskedFmt >> 10) & 0b111
  expect(formatBitsM(mask)).toBe(fmt1)

  // Verify 18-bit BCH Version Information (both 3x6 bottom-left and 6x3 top-right blocks) for V7+
  if (ver >= 7) {
    let vBottomLeft = 0
    let vTopRight = 0
    for (let i = 17; i >= 0; i--) {
      const rem = i % 3
      const div = Math.floor(i / 3)
      vBottomLeft = (vBottomLeft << 1) | (matrix[size - 11 + rem][div] ? 1 : 0)
      vTopRight = (vTopRight << 1) | (matrix[div][size - 11 + rem] ? 1 : 0)
    }
    expect(vBottomLeft).toBe(versionInfoBits(ver))
    expect(vTopRight).toBe(versionInfoBits(ver))
    expect(vBottomLeft >> 12).toBe(ver)
  }

  // Reconstruct function-module reservation map
  const reserved: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false))
  const markFinder = (tr: number, tc: number) => {
    for (let dr = -1; dr <= 7; dr++) {
      for (let dc = -1; dc <= 7; dc++) {
        const r = tr + dr
        const c = tc + dc
        if (r >= 0 && r < size && c >= 0 && c < size) reserved[r][c] = true
      }
    }
  }
  markFinder(0, 0)
  markFinder(0, size - 7)
  markFinder(size - 7, 0)

  for (const ar of spec.align) {
    for (const ac of spec.align) {
      if ((ar <= 8 && ac <= 8) || (ar <= 8 && ac >= size - 9) || (ar >= size - 9 && ac <= 8)) continue
      for (let dr = -2; dr <= 2; dr++) {
        for (let dc = -2; dc <= 2; dc++) {
          reserved[ar + dr][ac + dc] = true
        }
      }
      // Verify alignment pattern 5x5 dark/light center & ring
      for (let dr = -2; dr <= 2; dr++) {
        for (let dc = -2; dc <= 2; dc++) {
          expect(matrix[ar + dr][ac + dc]).toBe(Math.max(Math.abs(dr), Math.abs(dc)) !== 1)
        }
      }
    }
  }
  for (let i = 8; i < size - 8; i++) {
    reserved[6][i] = true
    reserved[i][6] = true
  }
  for (let i = 0; i <= 8; i++) {
    reserved[8][i] = true
    reserved[i][8] = true
  }
  for (let i = 0; i < 8; i++) {
    reserved[8][size - 1 - i] = true
    reserved[size - 1 - i][8] = true
  }
  if (ver >= 7) {
    for (let i = 0; i < 18; i++) {
      const rem = i % 3
      const div = Math.floor(i / 3)
      reserved[size - 11 + rem][div] = true
      reserved[div][size - 11 + rem] = true
    }
  }

  // Unmask and extract raw interleaved bitstream
  const maskFn = MASKS[mask]
  const rawBits: number[] = []
  let up = true
  for (let col = size - 1; col > 0; col -= 2) {
    if (col === 6) col--
    for (let step = 0; step < size; step++) {
      const r = up ? size - 1 - step : step
      for (let dx = 0; dx < 2; dx++) {
        const c = col - dx
        if (reserved[r][c]) continue
        const bit = (matrix[r][c] ? 1 : 0) ^ (maskFn(r, c) ? 1 : 0)
        rawBits.push(bit)
      }
    }
    up = !up
  }

  const totalBlocks = spec.numBlocksG1 + spec.numBlocksG2
  const dataCap = qrDataCapacityCw(spec)
  const totalCw = dataCap + totalBlocks * spec.ecCwPerBlock
  expect(Math.floor(rawBits.length / 8)).toBe(totalCw)

  const interleaved = new Uint8Array(totalCw)
  for (let i = 0; i < totalCw; i++) {
    let v = 0
    for (let b = 0; b < 8; b++) v = (v << 1) | rawBits[i * 8 + b]
    interleaved[i] = v
  }

  // De-interleave 2-group data + EC codewords
  const dBlocks: Uint8Array[] = []
  for (let b = 0; b < spec.numBlocksG1; b++) dBlocks.push(new Uint8Array(spec.dataCwG1))
  for (let b = 0; b < spec.numBlocksG2; b++) dBlocks.push(new Uint8Array(spec.dataCwG2))
  const eBlocks: Uint8Array[] = Array.from({ length: totalBlocks }, () => new Uint8Array(spec.ecCwPerBlock))

  let pos = 0
  const maxDataCw = Math.max(spec.dataCwG1, spec.dataCwG2)
  for (let i = 0; i < maxDataCw; i++) {
    for (let b = 0; b < totalBlocks; b++) {
      if (i < dBlocks[b].length) dBlocks[b][i] = interleaved[pos++]
    }
  }
  for (let i = 0; i < spec.ecCwPerBlock; i++) {
    for (let b = 0; b < totalBlocks; b++) {
      eBlocks[b][i] = interleaved[pos++]
    }
  }
  expect(pos).toBe(totalCw)

  // Verify Reed-Solomon syndromes S_0..S_{ecLen-1} === 0 for every block
  for (let b = 0; b < totalBlocks; b++) {
    const cw = new Uint8Array(dBlocks[b].length + spec.ecCwPerBlock)
    cw.set(dBlocks[b], 0)
    cw.set(eBlocks[b], dBlocks[b].length)
    for (let s = 0; s < spec.ecCwPerBlock; s++) {
      let syn = 0
      const root = GF_EXP[s]
      for (let j = 0; j < cw.length; j++) {
        syn = gfMul(syn, root) ^ cw[j]
      }
      expect(syn).toBe(0)
    }
  }

  // Reassemble sequential data stream & parse Byte-mode segment
  const seqData = new Uint8Array(dataCap)
  let outOff = 0
  for (const db of dBlocks) {
    seqData.set(db, outOff)
    outOff += db.length
  }

  const dataBits: number[] = []
  for (const byte of seqData) {
    for (let b = 7; b >= 0; b--) dataBits.push((byte >> b) & 1)
  }
  const readBits = (start: number, len: number) => {
    let v = 0
    for (let i = 0; i < len; i++) v = (v << 1) | dataBits[start + i]
    return v
  }
  expect(readBits(0, 4)).toBe(0b0100)
  const countBits = ver >= 10 ? 16 : 8
  const byteLen = readBits(4, countBits)
  const payload = new Uint8Array(byteLen)
  let bitPos = 4 + countBits
  for (let i = 0; i < byteLen; i++) {
    payload[i] = readBits(bitPos, 8)
    bitPos += 8
  }
  return {
    ver,
    mask,
    bytes: payload,
    text: new TextDecoder().decode(payload),
  }
}

describe('generateQrMatrix (ISO/IEC 18004 Versions 1-13 Level M)', () => {
  it('verified ISO 18004 BCH(18, 6) version info constants for V7-V13', () => {
    expect(versionInfoBits(7)).toBe(0x07c94)
    expect(versionInfoBits(8)).toBe(0x085bc)
    expect(versionInfoBits(9)).toBe(0x09a99)
    expect(versionInfoBits(10)).toBe(0x0a4d3)
    expect(versionInfoBits(11)).toBe(0x0bbf6)
    expect(versionInfoBits(12)).toBe(0x0c762)
    expect(versionInfoBits(13)).toBe(0x0d847)
    expect(qrMaxByteCapacity(QR_VERSIONS_M[12])).toBe(331)
  })

  it('encodes 84-byte default synctool://pair (m=1) URI cleanly at Version 5', () => {
    const uri84 =
      'synctool://pair?v=1&r=4K9M2P7X&m=1&p=oneshot&exp=1764300999#s=AbCdEfGhIjKlMnOpQrStUv'
    expect(uri84.length).toBe(84)
    const matrix = generateQrMatrix(uri84)
    expect(matrix.length).toBe(37) // Version 5
    const decoded = decodeAndVerifyQrLevelM(matrix)
    expect(decoded.ver).toBe(5)
    expect(decoded.text).toBe(uri84)
  })

  it('encodes 150B (Version 8) and 180B (Version 9) payloads with 2-group RS interleaving and V7+ version info', () => {
    const base150 =
      'synctool://pair?v=1&r=8N3Q5R9T&m=1&p=pair&u=https%3A%2F%2Fsync.example.org%3A54843&exp=1764309999#s=0123456789abcdefghijkl&k='
    const uri150 = base150 + 'X'.repeat(150 - base150.length)
    expect(uri150.length).toBe(150)
    const m8 = generateQrMatrix(uri150)
    expect(m8.length).toBe(49) // Version 8
    expect(decodeAndVerifyQrLevelM(m8).text).toBe(uri150)

    const uri180 = base150 + 'Y'.repeat(180 - base150.length)
    expect(uri180.length).toBe(180)
    const m9 = generateQrMatrix(uri180)
    expect(m9.length).toBe(53) // Version 9 (max 180B with 8-bit count)
    expect(decodeAndVerifyQrLevelM(m9).text).toBe(uri180)
  })

  it('encodes 210B (V10), 245B (V11), and 260B (V12) custom relay includeRelay URIs using 16-bit byte length', () => {
    const prefix =
      'synctool://pair?v=1&r=9W4Z7B2C&m=1&p=pair&u=https%3A%2F%2F203.0.113.42%3A54843&pin=' +
      'a1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff00' +
      '&exp=1764309999#s=AbCdEfGhIjKlMnOpQrStUv&k='
    const uri210 = prefix + 'T'.repeat(210 - prefix.length)
    expect(uri210.length).toBe(210)
    const m10 = generateQrMatrix(uri210)
    expect(m10.length).toBe(57) // Version 10
    expect(decodeAndVerifyQrLevelM(m10).text).toBe(uri210)

    const uri245 = prefix + 'K'.repeat(245 - prefix.length)
    expect(uri245.length).toBe(245)
    const m11 = generateQrMatrix(uri245)
    expect(m11.length).toBe(61) // Version 11
    expect(decodeAndVerifyQrLevelM(m11).text).toBe(uri245)

    const uri260 = prefix + 'Z'.repeat(260 - prefix.length)
    expect(uri260.length).toBe(260)
    const m12 = generateQrMatrix(uri260)
    expect(m12.length).toBe(65) // Version 12
    expect(decodeAndVerifyQrLevelM(m12).text).toBe(uri260)
  })

  it('encodes 325B and max 331B (Version 13) payloads and throws on >331B overflow', () => {
    const payload325 = 'synctool://pair?v=1&r=ABCDEFGH&m=1&p=oneshot#' + 'M'.repeat(325 - 45)
    expect(payload325.length).toBe(325)
    const m13 = generateQrMatrix(payload325)
    expect(m13.length).toBe(69) // Version 13
    expect(decodeAndVerifyQrLevelM(m13).text).toBe(payload325)

    const payload331 = 'synctool://pair?v=1&r=ABCDEFGH&m=1&p=oneshot#' + 'N'.repeat(331 - 45)
    expect(payload331.length).toBe(331)
    expect(decodeAndVerifyQrLevelM(generateQrMatrix(payload331)).text).toBe(payload331)

    const overflow332 = 'X'.repeat(332)
    expect(() => generateQrMatrix(overflow332)).toThrow(/Version 13/)
  })

  it('QrCodeSvg never leaks #s=/#k= into aria-label and renders an error fallback card on overflow', () => {
    const secretUri = 'synctool://pair?v=1&r=4K9M2P7X&m=1&p=oneshot#s=TOP_SECRET_OOB_128&k=TOP_SECRET_RELAY_KEY_32CHARS_MINIMUM'
    const html = renderToStaticMarkup(React.createElement(QrCodeSvg, { value: secretUri }))
    expect(html).toContain('aria-label="Sync pairing QR code"')
    expect(html).not.toContain('TOP_SECRET_OOB_128')
    expect(html).not.toContain('TOP_SECRET_RELAY_KEY')

    const errHtml = renderToStaticMarkup(React.createElement(QrCodeSvg, { value: 'Q'.repeat(400) }))
    expect(errHtml).toContain('role="alert"')
    expect(errHtml).toContain('二维码生成失败')
  })
})
