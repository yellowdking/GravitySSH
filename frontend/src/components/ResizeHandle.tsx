import React, { useState } from 'react'

interface ResizeHandleProps {
  width: number
  min: number
  max: number
  direction: 'left' | 'right'
  onChange: (nextWidth: number) => void
  onCommit?: (finalWidth: number) => void
  title?: string
}

export const ResizeHandle: React.FC<ResizeHandleProps> = ({
  width,
  min,
  max,
  direction,
  onChange,
  onCommit,
  title = '拖动调整面板宽度',
}) => {
  const [dragging, setDragging] = useState(false)

  const handleMouseDown = (e: React.MouseEvent) => {
    e.preventDefault()
    const startX = e.clientX
    const startWidth = width
    let latestWidth = startWidth

    setDragging(true)
    const prevCursor = document.body.style.cursor
    const prevSelect = document.body.style.userSelect
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'

    const onMove = (ev: MouseEvent) => {
      const delta = direction === 'right' ? ev.clientX - startX : startX - ev.clientX
      const clamped = Math.max(min, Math.min(max, Math.round(startWidth + delta)))
      if (clamped !== latestWidth) {
        latestWidth = clamped
        onChange(clamped)
      }
    }

    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      document.body.style.cursor = prevCursor
      document.body.style.userSelect = prevSelect
      setDragging(false)
      onCommit?.(latestWidth)
    }

    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      title={title}
      onMouseDown={handleMouseDown}
      className="w-1 shrink-0 h-full cursor-col-resize relative select-none group z-20 bg-transparent"
    >
      <div className="absolute inset-y-0 -left-1 -right-1" />
      <div
        className={`absolute inset-y-0 left-1/2 -translate-x-1/2 w-[2px] transition-colors ${
          dragging
            ? 'bg-indigo-400 shadow-[0_0_8px_rgba(99,102,241,0.75)]'
            : 'bg-transparent group-hover:bg-indigo-500/65'
        }`}
      />
    </div>
  )
}
