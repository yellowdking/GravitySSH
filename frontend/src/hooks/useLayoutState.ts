import { useCallback, useState } from 'react'

function readNum(key: string, fallback: number, min: number, max: number): number {
  try {
    const raw = window.localStorage.getItem(key)
    const n = raw ? Number(raw) : NaN
    if (Number.isFinite(n)) return Math.max(min, Math.min(max, Math.round(n)))
  } catch { /* ignore */ }
  return fallback
}

function readBool(key: string, fallback: boolean): boolean {
  try {
    const raw = window.localStorage.getItem(key)
    if (raw === '1' || raw === 'true') return true
    if (raw === '0' || raw === 'false') return false
  } catch { /* ignore */ }
  return fallback
}

function saveStr(key: string, val: string) {
  try { window.localStorage.setItem(key, val) } catch { /* ignore */ }
}

export function useLayoutState() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => readBool('gssh.sidebarCollapsed', false))
  const [sidebarWidth, setSidebarWidth] = useState(() => readNum('gssh.sidebarWidth', 268, 180, 440))
  const [sftpWidth, setSftpWidth] = useState(() => readNum('gssh.sftpWidth', 360, 240, 620))
  const [drawerWidth, setDrawerWidth] = useState(() => readNum('gssh.drawerWidth', 360, 260, 560))

  const toggleSidebar = useCallback(() => {
    setSidebarCollapsed((prev) => {
      const next = !prev
      saveStr('gssh.sidebarCollapsed', next ? '1' : '0')
      return next
    })
  }, [])

  const commitSidebarWidth = useCallback((w: number) => saveStr('gssh.sidebarWidth', String(w)), [])
  const commitSftpWidth = useCallback((w: number) => saveStr('gssh.sftpWidth', String(w)), [])
  const commitDrawerWidth = useCallback((w: number) => saveStr('gssh.drawerWidth', String(w)), [])

  const estimateInitialPty = useCallback(
    (container: HTMLDivElement | null, fontSize: number, withSftp: boolean, withDrawer: boolean) => {
      const rect = container?.getBoundingClientRect()
      const fs = Math.max(10, fontSize || 14)
      const availW = (rect?.width || 960) - (withSftp ? sftpWidth : 0) - (withDrawer ? drawerWidth : 0) - 36
      const availH = (rect?.height || 620) - 64
      const cols = Math.max(60, Math.min(240, Math.floor(availW / (fs * 0.62))))
      const rows = Math.max(18, Math.min(80, Math.floor(availH / (fs * 1.15))))
      return { cols, rows }
    },
    [sftpWidth, drawerWidth]
  )

  return {
    sidebarCollapsed,
    sidebarWidth,
    setSidebarWidth,
    sftpWidth,
    setSftpWidth,
    drawerWidth,
    setDrawerWidth,
    toggleSidebar,
    commitSidebarWidth,
    commitSftpWidth,
    commitDrawerWidth,
    estimateInitialPty,
  }
}
