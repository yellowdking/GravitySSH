// GF(256) Reed-Solomon ECC & Byte-mode codeword encoder for QR Versions 1-6 (Level M).

interface QrVersionSpec {
  ver: number
  size: number
  dataCap: number
  blocks: number
  ecPerBlock: number
  align: number[]
}

export const QR_VERSIONS_M: QrVersionSpec[] = [
  { ver: 1, size: 21, dataCap: 16, blocks: 1, ecPerBlock: 10, align: [] },
  { ver: 2, size: 25, dataCap: 28, blocks: 1, ecPerBlock: 16, align: [6, 18] },
  { ver: 3, size: 29, dataCap: 44, blocks: 1, ecPerBlock: 26, align: [6, 22] },
  { ver: 4, size: 33, dataCap: 64, blocks: 2, ecPerBlock: 18, align: [6, 26] },
  { ver: 5, size: 37, dataCap: 86, blocks: 2, ecPerBlock: 24, align: [6, 30] },
  { ver: 6, size: 41, dataCap: 108, blocks: 4, ecPerBlock: 16, align: [6, 34] },
]

const GF_EXP = new Uint8Array(512)
const GF_LOG = new Uint8Array(256)
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

function gfMul(a: number, b: number): number {
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
  const bits: number[] = []
  const push = (val: number, len: number) => {
    for (let i = len - 1; i >= 0; i--) bits.push((val >> i) & 1)
  }
  push(0b0100, 4)
  push(bytes.length, 8)
  for (const b of bytes) push(b, 8)
  const capBits = spec.dataCap * 8
  push(0, Math.min(4, capBits - bits.length))
  while (bits.length % 8 !== 0) bits.push(0)

  const data = new Uint8Array(spec.dataCap)
  for (let i = 0; i < bits.length / 8; i++) {
    let v = 0
    for (let b = 0; b < 8; b++) v = (v << 1) | bits[i * 8 + b]
    data[i] = v
  }
  for (let i = bits.length / 8, p = 0; i < spec.dataCap; i++, p++) {
    data[i] = p % 2 === 0 ? 0xec : 0x11
  }

  const perBlock = spec.dataCap / spec.blocks
  const dBlocks: Uint8Array[] = []
  const eBlocks: Uint8Array[] = []
  for (let b = 0; b < spec.blocks; b++) {
    const slice = data.subarray(b * perBlock, (b + 1) * perBlock)
    dBlocks.push(slice)
    eBlocks.push(rsRemainder(slice, spec.ecPerBlock))
  }
  const out = new Uint8Array(spec.dataCap + spec.blocks * spec.ecPerBlock)
  let pos = 0
  for (let i = 0; i < perBlock; i++) for (let b = 0; b < spec.blocks; b++) out[pos++] = dBlocks[b][i]
  for (let i = 0; i < spec.ecPerBlock; i++) for (let b = 0; b < spec.blocks; b++) out[pos++] = eBlocks[b][i]
  return out
}
