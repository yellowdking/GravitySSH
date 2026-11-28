// Strip any #fragment secret (#k= / #s=) from synctool:// URIs before displaying in UI.
export function redactSyncUri(raw: string): string {
  if (!raw) return ''
  const trimmed = raw.trim()
  const idx = trimmed.indexOf('#')
  return idx >= 0 ? `${trimmed.slice(0, idx)}#[REDACTED]` : trimmed
}

export function isSynctoolUri(raw: string): boolean {
  return /^synctool:\/\//i.test(raw.trim())
}

// Formats either a synctool://pair URI (preserving case-sensitive b64u #s=/#k=!)
// or an 8-character Crockford Base32 room code (XXXX-XXXX).
export function formatSyncCodeOrUriInput(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return ''
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed) || trimmed.includes('://') || /^synctool:/i.test(trimmed)) {
    return trimmed
  }
  const normalized = trimmed
    .toUpperCase()
    .replace(/O/g, '0')
    .replace(/[IL]/g, '1')
    .replace(/[^0-9A-HJ-KM-NP-TV-Z]/g, '')
    .slice(0, 8)
  return normalized.length > 4 ? `${normalized.slice(0, 4)}-${normalized.slice(4)}` : normalized
}

export function isReadySyncCodeOrUri(raw: string): boolean {
  const trimmed = raw.trim()
  if (!trimmed) return false
  if (isSynctoolUri(trimmed)) return trimmed.length >= 20
  const clean = trimmed.replace(/-/g, '')
  return /^[0-9A-HJ-KM-NP-TV-Z]{8}$/i.test(clean)
}

export function formatRoomCodeDisplay(code?: string): string {
  if (!code) return '---- ----'
  const clean = code.trim().toUpperCase().replace(/[^0-9A-Z]/g, '')
  if (clean.length === 8) return `${clean.slice(0, 4)}-${clean.slice(4)}`
  return code.trim()
}

export function formatSasDisplay(sas?: string): string {
  if (!sas) return '--- ---'
  const digits = sas.replace(/\D/g, '').slice(0, 6)
  if (digits.length === 6) return `${digits.slice(0, 3)}-${digits.slice(3)}`
  return sas.trim()
}

export function parseSyncTimestampMs(val: number | string | null | undefined): number {
  if (val === null || val === undefined || val === '' || val === 0) return Number.NaN
  if (typeof val === 'number') {
    if (!Number.isFinite(val) || val <= 0) return Number.NaN
    return val > 1e11 ? val : val * 1000
  }
  const s = val.trim()
  if (!s) return Number.NaN
  if (/^\d+$/.test(s)) {
    const n = Number(s)
    if (!Number.isFinite(n) || n <= 0) return Number.NaN
    return n > 1e11 ? n : n * 1000
  }
  return new Date(s).getTime()
}

export function formatSyncTimestamp(val: number | string | null | undefined): string {
  const ms = parseSyncTimestampMs(val)
  if (!Number.isFinite(ms)) return '未知时间'
  return new Date(ms).toLocaleString()
}

export function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

export function formatPlatformLabel(raw?: string): string {
  const p = (raw || '').trim().toLowerCase()
  if (p === 'android') return 'Android'
  if (p === 'windows') return 'Windows'
  if (p === 'darwin' || p === 'macos') return 'macOS'
  if (p === 'ios') return 'iOS'
  if (p === 'linux') return 'Linux'
  return raw?.trim() || '未知平台'
}

export function certExpiryAlert(
  certExpiresAt?: string,
  certRotateRecommended?: boolean
): { level: 'none' | 'warn' | 'urgent'; daysLeft: number | null; formattedDate: string } {
  const ms = parseSyncTimestampMs(certExpiresAt)
  if (!Number.isFinite(ms)) {
    return {
      level: certRotateRecommended ? 'warn' : 'none',
      daysLeft: null,
      formattedDate: certExpiresAt || '',
    }
  }
  const daysLeft = Math.ceil((ms - Date.now()) / 86_400_000)
  const formattedDate = new Date(ms).toLocaleDateString()
  if (daysLeft <= 7) return { level: 'urgent', daysLeft, formattedDate }
  if (daysLeft < 30 || certRotateRecommended) return { level: 'warn', daysLeft, formattedDate }
  return { level: 'none', daysLeft, formattedDate }
}
