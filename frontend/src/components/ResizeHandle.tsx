import React, { useEffect, useRef, useState } from 'react'

interface ResizeHandleProps {
  width: number
  min: number
  max: number
  direction: 'left' | 'right'
  onChange?: (nextWidth: number) => void
  onCommit?: (finalWidth: number) => void
  title?: string
}

export const ResizeHandle: React.FC<ResizeHandleProps> = React.memo(({
  width,
  min,
  max,
  direction,
  onChange,
  onCommit,
  title = '拖动调整面板宽度',
}) => {
  const [dragging, setDragging] = useState(false)
  const cleanupRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    return () => {
      cleanupRef.current?.()
      cleanupRef.current = null
    }
  }, [])

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return
    e.preventDefault()
    cleanupRef.current?.()

    const target = e.currentTarget
    const pointerId = e.pointerId
    try {
      target.setPointerCapture(pointerId)
    } catch {
      /* ignore synthetic or unsupported pointer capture */
    }

    const startX = e.clientX
    const startWidth = width
    let latestWidth = startWidth
    let rafId = 0
    let pendingWidth: number | null = null

    setDragging(true)
    const prevCursor = document.body.style.cursor
    const prevSelect = document.body.style.userSelect
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'

    const flushWidth = () => {
      rafId = 0
      if (pendingWidth !== null) {
        const w = pendingWidth
        pendingWidth = null
        onChange?.(w)
      }
    }

    let finished = false

    const teardown = () => {
      if (finished) return
      finished = true
      if (rafId) {
        cancelAnimationFrame(rafId)
        rafId = 0
      }
      try {
        if (target.hasPointerCapture?.(pointerId)) {
          target.releasePointerCapture(pointerId)
        }
      } catch {
        /* ignore */
      }
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onUp)
      window.removeEventListener('blur', onBlur)
      document.body.style.cursor = prevCursor
      document.body.style.userSelect = prevSelect
      cleanupRef.current = null
    }

    const onMove = (ev: PointerEvent) => {
      if (ev.pointerId !== pointerId) return
      const delta = direction === 'right' ? ev.clientX - startX : startX - ev.clientX
      const clamped = Math.max(min, Math.min(max, Math.round(startWidth + delta)))
      if (clamped !== latestWidth) {
        latestWidth = clamped
        pendingWidth = clamped
        if (!rafId) rafId = requestAnimationFrame(flushWidth)
      }
    }

    const finishDrag = () => {
      if (finished) return
      teardown()
      if (pendingWidth !== null) {
        onChange?.(pendingWidth)
        pendingWidth = null
      }
      setDragging(false)
      onCommit?.(latestWidth)
    }

    const onUp = (ev: PointerEvent) => {
      if (ev.pointerId !== pointerId) return
      finishDrag()
    }

    const onBlur = () => {
      finishDrag()
    }

    cleanupRef.current = teardown
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
    window.addEventListener('blur', onBlur)
  }

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      title={title}
      onPointerDown={handlePointerDown}
      className="w-1 shrink-0 h-full cursor-col-resize relative select-none touch-none group z-20 bg-transparent"
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
})
