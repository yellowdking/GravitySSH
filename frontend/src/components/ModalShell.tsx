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

interface ModalEntry {
  getContainer: () => HTMLDivElement | null
  onClose: () => void
}

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(', ')

function getFocusableElements(container: HTMLElement): HTMLElement[] {
  const nodes = container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)
  const list: HTMLElement[] = []
  for (const el of nodes) {
    if (el.closest('[inert]')) continue
    if (el.offsetParent === null && getComputedStyle(el).position !== 'fixed') continue
    list.push(el)
  }
  return list
}

const modalStack: ModalEntry[] = []

function handleGlobalKeyDown(e: KeyboardEvent) {
  if (e.isComposing || e.keyCode === 229 || modalStack.length === 0) return
  const top = modalStack[modalStack.length - 1]
  if (e.key === 'Escape') {
    if (e.defaultPrevented) return
    e.preventDefault()
    top.onClose()
    return
  }
  if (e.key === 'Tab') {
    if (e.defaultPrevented) return
    const container = top.getContainer()
    if (!container) return
    const focusables = getFocusableElements(container)
    if (focusables.length === 0) {
      e.preventDefault()
      container.focus()
      return
    }
    const first = focusables[0]
    const last = focusables[focusables.length - 1]
    const active = document.activeElement as HTMLElement | null
    if (!active || !container.contains(active)) {
      e.preventDefault()
      ;(e.shiftKey ? last : first).focus()
      return
    }
    if (e.shiftKey) {
      if (active === first || active === container) {
        e.preventDefault()
        last.focus()
      }
    } else if (active === last) {
      e.preventDefault()
      first.focus()
    }
  }
}

let listenerAttached = false

function syncGlobalListener() {
  if (modalStack.length > 0 && !listenerAttached) {
    window.addEventListener('keydown', handleGlobalKeyDown)
    listenerAttached = true
  } else if (modalStack.length === 0 && listenerAttached) {
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
  const dialogRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    const prevFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const entry: ModalEntry = {
      getContainer: () => dialogRef.current,
      onClose: () => onCloseRef.current(),
    }
    modalStack.push(entry)
    syncGlobalListener()

    const rafId = requestAnimationFrame(() => {
      const container = dialogRef.current
      if (!container) return
      if (document.activeElement && container.contains(document.activeElement)) return
      const focusables = getFocusableElements(container)
      if (focusables.length > 0) focusables[0].focus({ preventScroll: true })
      else container.focus({ preventScroll: true })
    })

    return () => {
      cancelAnimationFrame(rafId)
      const idx = modalStack.lastIndexOf(entry)
      if (idx !== -1) modalStack.splice(idx, 1)
      syncGlobalListener()
      if (prevFocused && prevFocused.isConnected && !prevFocused.closest('[inert]')) {
        prevFocused.focus({ preventScroll: true })
      }
    }
  }, [open])

  if (!open) return null

  return createPortal(
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      tabIndex={-1}
      className={`fixed inset-0 ${zIndexClass} ${backdropClassName} backdrop-blur-sm flex items-center justify-center p-4 focus:outline-none`}
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
