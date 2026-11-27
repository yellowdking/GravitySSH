import React, { useMemo } from 'react'
import { generateQrMatrix } from '../utils/qrCode'

interface QrCodeSvgProps {
  value: string
  size?: number
  className?: string
}

export const QrCodeSvg: React.FC<QrCodeSvgProps> = ({ value, size = 176, className = '' }) => {
  const quiet = 3
  const { dim, pathData } = useMemo(() => {
    try {
      const matrix = generateQrMatrix(value)
      const parts: string[] = []
      for (let r = 0; r < matrix.length; r++) {
        const row = matrix[r]
        let c = 0
        while (c < row.length) {
          if (!row[c]) {
            c++
            continue
          }
          const start = c
          while (c < row.length && row[c]) c++
          parts.push(`M${start + quiet} ${r + quiet}h${c - start}v1h-${c - start}z`)
        }
      }
      return { dim: matrix.length + quiet * 2, pathData: parts.join('') }
    } catch {
      return { dim: 0, pathData: '' }
    }
  }, [value])

  if (!dim) return null

  return (
    <div className={`inline-flex p-2.5 rounded-xl bg-white shadow-lg border border-indigo-400/30 ${className}`}>
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${dim} ${dim}`}
        shapeRendering="crispEdges"
        role="img"
        aria-label={`QR code for ${value}`}
      >
        <rect width={dim} height={dim} fill="#ffffff" />
        {pathData && <path d={pathData} fill="#0b0d12" />}
      </svg>
    </div>
  )
}
