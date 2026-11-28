// GF(256) Reed-Solomon ECC & Byte-mode codeword encoder for ISO/IEC 18004 QR Versions 1-13 (Level M, <= 331 bytes).

export interface QrVersionSpec {
  ver: number
  size: number
  numBlocksG1: number
  dataCwG1: number
  numBlocksG2: number
  dataCwG2: number
  ecCwPerBlock: number
  align: number[]
}

export const ALIGNMENT_CENTERS: Record<number, number[]> = {
  1: [],
  2: [6, 18],
  3: [6, 22],
  4: [6, 26],
  5: [6, 30],
  6: [6, 34],
  7: [6, 22, 38],
  8: [6, 24, 42],
  9: [6, 26, 46],
  10: [6, 28, 50],
  11: [6, 30, 54],
  12: [6, 32, 58],
  13: [6, 34, 62],
}

export const QR_VERSIONS_M: QrVersionSpec[] = [
  { ver: 1, size: 21, numBlocksG1: 1, dataCwG1: 16, numBlocksG2: 0, dataCwG2: 0, ecCwPerBlock: 10, align: ALIGNMENT_CENTERS[1] },
  { ver: 2, size: 25, numBlocksG1: 1, dataCwG1: 28, numBlocksG2: 0, dataCwG2: 0, ecCwPerBlock: 16, align: ALIGNMENT_CENTERS[2] },
  { ver: 3, size: 29, numBlocksG1: 1, dataCwG1: 44, numBlocksG2: 0, dataCwG2: 0, ecCwPerBlock: 26, align: ALIGNMENT_CENTERS[3] },
  { ver: 4, size: 33, numBlocksG1: 2, dataCwG1: 32, numBlocksG2: 0, dataCwG2: 0, ecCwPerBlock: 18, align: ALIGNMENT_CENTERS[4] },
  { ver: 5, size: 37, numBlocksG1: 2, dataCwG1: 43, numBlocksG2: 0, dataCwG2: 0, ecCwPerBlock: 24, align: ALIGNMENT_CENTERS[5] },
  { ver: 6, size: 41, numBlocksG1: 4, dataCwG1: 27, numBlocksG2: 0, dataCwG2: 0, ecCwPerBlock: 16, align: ALIGNMENT_CENTERS[6] },
  { ver: 7, size: 45, numBlocksG1: 4, dataCwG1: 31, numBlocksG2: 0, dataCwG2: 0, ecCwPerBlock: 18, align: ALIGNMENT_CENTERS[7] },
  { ver: 8, size: 49, numBlocksG1: 2, dataCwG1: 38, numBlocksG2: 2, dataCwG2: 39, ecCwPerBlock: 22, align: ALIGNMENT_CENTERS[8] },
  { ver: 9, size: 53, numBlocksG1: 3, dataCwG1: 36, numBlocksG2: 2, dataCwG2: 37, ecCwPerBlock: 22, align: ALIGNMENT_CENTERS[9] },
  { ver: 10, size: 57, numBlocksG1: 4, dataCwG1: 43, numBlocksG2: 1, dataCwG2: 44, ecCwPerBlock: 26, align: ALIGNMENT_CENTERS[10] },
  { ver: 11, size: 61, numBlocksG1: 1, dataCwG1: 50, numBlocksG2: 4, dataCwG2: 51, ecCwPerBlock: 30, align: ALIGNMENT_CENTERS[11] },
  { ver: 12, size: 65, numBlocksG1: 6, dataCwG1: 36, numBlocksG2: 2, dataCwG2: 37, ecCwPerBlock: 22, align: ALIGNMENT_CENTERS[12] },
  { ver: 13, size: 69, numBlocksG1: 8, dataCwG1: 37, numBlocksG2: 1, dataCwG2: 38, ecCwPerBlock: 22, align: ALIGNMENT_CENTERS[13] },
]

export function qrDataCapacityCw(spec: QrVersionSpec): number {
  return spec.numBlocksG1 * spec.dataCwG1 + spec.numBlocksG2 * spec.dataCwG2
}

export function qrCharCountBits(ver: number): number {
  return ver >= 10 ? 16 : 8
}

export function qrMaxByteCapacity(spec: QrVersionSpec): number {
  const totalBits = qrDataCapacityCw(spec) * 8
  const headerBits = 4 + qrCharCountBits(spec.ver)
  return Math.floor((totalBits - headerBits) / 8)
}

export const GF_EXP = new Uint8Array(512)
export const GF_LOG = new Uint8Array(256)
;(function initGF() {
  let x = 1
  for (let i = 0; i < 255; i++) {
    GF_EXP[i] = x
    GF_LOG[x] = i
    x <<= 1
    if (x & 0x100) x ^= 0x11d
  }
  for (let i = 255; i < 512; i++) GF_EXP[i] = GF_EXP[i - 255]
})()

export function gfMul(a: number, b: number): number {
  return a === 0 || b === 0 ? 0 : GF_EXP[GF_LOG[a] + GF_LOG[b]]
}

function rsRemainder(data: Uint8Array, ecLen: number): Uint8Array {
  let gen = new Uint8Array([1])
  for (let i = 0; i < ecLen; i++) {
    const next = new Uint8Array(gen.length + 1)
    const factor = GF_EXP[i]
    for (let j = 0; j < gen.length; j++) {
      next[j] ^= gen[j]
      next[j + 1] ^= gfMul(gen[j], factor)
    }
    gen = next
  }
  const rem = new Uint8Array(ecLen)
  for (let i = 0; i < data.length; i++) {
    const f = data[i] ^ rem[0]
    for (let j = 0; j < ecLen - 1; j++) rem[j] = rem[j + 1] ^ gfMul(gen[j + 1], f)
    rem[ecLen - 1] = gfMul(gen[ecLen], f)
  }
  return rem
}

export function encodeQrCodewords(bytes: Uint8Array, spec: QrVersionSpec): Uint8Array {
  const dataCap = qrDataCapacityCw(spec)
  const countBits = qrCharCountBits(spec.ver)
  const bits: number[] = []
  const push = (val: number, len: number) => {
    for (let i = len - 1; i >= 0; i--) bits.push((val >> i) & 1)
  }
  push(0b0100, 4)
  push(bytes.length, countBits)
  for (const b of bytes) push(b, 8)
  const capBits = dataCap * 8
  push(0, Math.min(4, capBits - bits.length))
  while (bits.length % 8 !== 0) bits.push(0)

  const data = new Uint8Array(dataCap)
  for (let i = 0; i < bits.length / 8; i++) {
    let v = 0
    for (let b = 0; b < 8; b++) v = (v << 1) | bits[i * 8 + b]
    data[i] = v
  }
  for (let i = bits.length / 8, p = 0; i < dataCap; i++, p++) {
    data[i] = p % 2 === 0 ? 0xec : 0x11
  }

  const dBlocks: Uint8Array[] = []
  const eBlocks: Uint8Array[] = []
  let offset = 0
  for (let b = 0; b < spec.numBlocksG1; b++) {
    const slice = data.subarray(offset, offset + spec.dataCwG1)
    offset += spec.dataCwG1
    dBlocks.push(slice)
    eBlocks.push(rsRemainder(slice, spec.ecCwPerBlock))
  }
  for (let b = 0; b < spec.numBlocksG2; b++) {
    const slice = data.subarray(offset, offset + spec.dataCwG2)
    offset += spec.dataCwG2
    dBlocks.push(slice)
    eBlocks.push(rsRemainder(slice, spec.ecCwPerBlock))
  }

  const totalBlocks = spec.numBlocksG1 + spec.numBlocksG2
  const maxDataCw = Math.max(spec.dataCwG1, spec.dataCwG2)
  const out = new Uint8Array(dataCap + totalBlocks * spec.ecCwPerBlock)
  let pos = 0
  for (let i = 0; i < maxDataCw; i++) {
    for (let b = 0; b < totalBlocks; b++) {
      if (i < dBlocks[b].length) out[pos++] = dBlocks[b][i]
    }
  }
  for (let i = 0; i < spec.ecCwPerBlock; i++) {
    for (let b = 0; b < totalBlocks; b++) {
      out[pos++] = eBlocks[b][i]
    }
  }
  return out
}
