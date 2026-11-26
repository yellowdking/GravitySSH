import { describe, expect, it } from 'vitest'
import { generateQrMatrix } from './qrCode'

describe('generateQrMatrix', () => {
  it('encodes gssh-sync QR URI into a valid 29x29 Version 3 matrix', () => {
    const uri = 'gssh-sync://v1?code=K7M9-P4X2'
    const matrix = generateQrMatrix(uri)
    expect(matrix.length).toBe(29)
    expect(matrix.every((row) => row.length === 29)).toBe(true)

    // Verify top-left, top-right, bottom-left 7x7 finder outer borders & 3x3 centers
    const checkFinder = (tr: number, tc: number) => {
      for (let dr = 0; dr < 7; dr++) {
        for (let dc = 0; dc < 7; dc++) {
          const expected = dr === 0 || dr === 6 || dc === 0 || dc === 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4)
          expect(matrix[tr + dr][tc + dc]).toBe(expected)
        }
      }
    }
    checkFinder(0, 0)
    checkFinder(0, 22)
    checkFinder(22, 0)

    // Verify timing patterns & dark module at (size - 8, 8)
    for (let i = 8; i < 21; i++) {
      expect(matrix[6][i]).toBe(i % 2 === 0)
      expect(matrix[i][6]).toBe(i % 2 === 0)
    }
    expect(matrix[21][8]).toBe(true)
  })

  it('uses Version 1 for short strings and Version 4 for longer URIs', () => {
    expect(generateQrMatrix('HELLO').length).toBe(21)
    expect(generateQrMatrix('gssh-sync://v1?code=K7M9-P4X2&relay=custom.example').length).toBe(33)
  })
})
