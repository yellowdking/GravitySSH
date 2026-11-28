import React, { useMemo } from 'react'
import { AlertTriangle } from 'lucide-react'
import { generateQrMatrix } from '../utils/qrCode'

interface QrCodeSvgProps {
  value: string
  size?: number
  className?: string
}

export const QrCodeSvg: React.FC<QrCodeSvgProps> = ({ value, size = 184, className = '' }) => {
  const quiet = 4
  const { dim, pathData, error } = useMemo(() => {
    if (!value) {
      return { dim: 0, pathData: '', error: '配对二维码内容为空' }
    }
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
      return { dim: matrix.length + quiet * 2, pathData: parts.join(''), error: '' }
    } catch {
      return {
        dim: 0,
        pathData: '',
        error: '二维码生成失败（链接超出容量限制），请改用复制链接或 8 位房间码',
      }
    }
  }, [value])

  if (!dim || error) {
    return (
      <div
        role="alert"
        style={{ width: size, minHeight: Math.min(size, 148) }}
        className={`inline-flex flex-col items-center justify-center gap-2 p-3 rounded-xl bg-rose-950/40 border border-rose-500/40 text-center ${className}`}
      >
        <AlertTriangle className="w-5 h-5 text-rose-400 shrink-0" />
        <span className="text-[11px] text-rose-200 leading-relaxed">{error}</span>
      </div>
    )
  }

  return (
    <div className={`inline-flex p-2.5 rounded-xl bg-white shadow-lg border border-indigo-400/30 ${className}`}>
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${dim} ${dim}`}
        shapeRendering="crispEdges"
        role="img"
        aria-label="Sync pairing QR code"
      >
        <rect width={dim} height={dim} fill="#ffffff" />
        {pathData && <path d={pathData} fill="#0b0d12" />}
      </svg>
    </div>
  )
}
