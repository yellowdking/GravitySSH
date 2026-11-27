import React, { useEffect, useRef, useState } from 'react'

interface NumericInputProps {
  value: number
  fallback: number
  min?: number
  max?: number
  placeholder?: string
  className?: string
  onChange: (next: number) => void
}

export const NumericInput: React.FC<NumericInputProps> = ({
  value,
  fallback,
  min,
  max,
  placeholder,
  className,
  onChange,
}) => {
  const [raw, setRaw] = useState(() => (Number.isFinite(value) ? String(Math.round(value)) : ''))
  const focusedRef = useRef(false)
  const allowNegative = min === undefined || min < 0

  useEffect(() => {
    if (!focusedRef.current && Number.isFinite(value)) {
      setRaw(String(Math.round(value)))
    }
  }, [value])

  const clamp = (n: number) => {
    let v = Math.trunc(n)
    if (min !== undefined && v < min) v = min
    if (max !== undefined && v > max) v = max
    return v
  }

  return (
    <input
      type="text"
      inputMode="numeric"
      pattern={allowNegative ? '-?[0-9]*' : '[0-9]*'}
      placeholder={placeholder}
      className={className}
      value={raw}
      onFocus={() => {
        focusedRef.current = true
      }}
      onChange={(e) => {
        const nextStr = e.target.value.trim()
        const validRe = allowNegative ? /^-?\d*$/ : /^\d*$/
        if (!validRe.test(nextStr)) return
        setRaw(nextStr)
        if (nextStr === '' || nextStr === '-') {
          onChange(Number.NaN)
          return
        }
        const parsed = parseInt(nextStr, 10)
        if (Number.isSafeInteger(parsed)) onChange(parsed)
      }}
      onBlur={() => {
        focusedRef.current = false
        const trimmed = raw.trim()
        const validRe = allowNegative ? /^-?\d+$/ : /^\d+$/
        const parsed = validRe.test(trimmed) ? parseInt(trimmed, 10) : Number.NaN
        const finalVal = Number.isSafeInteger(parsed) ? clamp(parsed) : fallback
        setRaw(String(finalVal))
        if (finalVal !== value) onChange(finalVal)
      }}
    />
  )
}
