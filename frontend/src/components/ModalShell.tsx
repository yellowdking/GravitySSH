import React, { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'

interface ModalShellProps {
  open: boolean
  onClose: () => void
  zIndexClass?: string
  backdropClassName?: string
  closeOnBackdrop?: boolean
  children: React.ReactNode
}

const escapeStack: Array<() => void> = []

function handleGlobalKeyDown(e: KeyboardEvent) {
  if (e.key !== 'Escape' || e.defaultPrevented || escapeStack.length === 0) return
  e.preventDefault()
  const top = escapeStack[escapeStack.length - 1]
  top()
}

let listenerAttached = false

function syncGlobalListener() {
  if (escapeStack.length > 0 && !listenerAttached) {
    window.addEventListener('keydown', handleGlobalKeyDown)
    listenerAttached = true
  } else if (escapeStack.length === 0 && listenerAttached) {
    window.removeEventListener('keydown', handleGlobalKeyDown)
    listenerAttached = false
  }
}

export const ModalShell: React.FC<ModalShellProps> = ({
  open,
  onClose,
  zIndexClass = 'z-50',
  backdropClassName = 'bg-black/70',
  closeOnBackdrop = false,
  children,
}) => {
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    if (!open) return
    const handler = () => onCloseRef.current()
    escapeStack.push(handler)
    syncGlobalListener()
    return () => {
      const idx = escapeStack.lastIndexOf(handler)
      if (idx !== -1) escapeStack.splice(idx, 1)
      syncGlobalListener()
    }
  }, [open])

  if (!open) return null

  return createPortal(
    <div
      role="dialog"
      aria-modal="true"
      className={`fixed inset-0 ${zIndexClass} ${backdropClassName} backdrop-blur-sm flex items-center justify-center p-4`}
      onMouseDown={
        closeOnBackdrop
          ? (e) => {
              if (e.target === e.currentTarget) onCloseRef.current()
            }
          : undefined
      }
    >
      {children}
    </div>,
    document.body
  )
}
