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
  const [raw, setRaw] = useState(() => String(value))
  const focusedRef = useRef(false)

  useEffect(() => {
    if (!focusedRef.current) setRaw(String(value))
  }, [value])

  const clamp = (n: number) => {
    let v = Math.round(n)
    if (min !== undefined && v < min) v = min
    if (max !== undefined && v > max) v = max
    return v
  }

  return (
    <input
      type="number"
      min={min}
      max={max}
      placeholder={placeholder}
      className={className}
      value={raw}
      onFocus={() => {
        focusedRef.current = true
      }}
      onChange={(e) => {
        const nextStr = e.target.value
        setRaw(nextStr)
        if (nextStr.trim() === '') return
        const parsed = Number(nextStr)
        if (Number.isFinite(parsed)) onChange(parsed)
      }}
      onBlur={() => {
        focusedRef.current = false
        const trimmed = raw.trim()
        const parsed = trimmed === '' ? NaN : Number(trimmed)
        const finalVal = Number.isFinite(parsed) ? clamp(parsed) : fallback
        setRaw(String(finalVal))
        if (finalVal !== value) onChange(finalVal)
      }}
    />
  )
}
