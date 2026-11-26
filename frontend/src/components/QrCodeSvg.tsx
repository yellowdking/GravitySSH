import React, { useMemo } from 'react'
import { generateQrMatrix } from '../utils/qrCode'

interface QrCodeSvgProps {
  value: string
  size?: number
  className?: string
}

export const QrCodeSvg: React.FC<QrCodeSvgProps> = ({ value, size = 176, className = '' }) => {
  const matrix = useMemo(() => {
    try {
      return generateQrMatrix(value)
    } catch {
      return null
    }
  }, [value])

  if (!matrix) return null
  const quiet = 3
  const dim = matrix.length + quiet * 2

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
        {matrix.map((row, r) =>
          row.map((dark, c) =>
            dark ? <rect key={`${r}-${c}`} x={c + quiet} y={r + quiet} width={1} height={1} fill="#0b0d12" /> : null,
          ),
        )}
      </svg>
    </div>
  )
}
