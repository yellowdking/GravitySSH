import type { ITheme } from '@xterm/xterm'

export const DEFAULT_TERMINAL_FONT = '"Cascadia Mono", "Consolas", "Courier New", monospace'

export const ANTIGRAVITY_TERM_THEME: ITheme = {
  background: '#0b0d12',
  foreground: '#e6eaf5',
  cursor: '#818cf8',
  cursorAccent: '#0b0d12',
  selectionBackground: 'rgba(99, 102, 241, 0.35)',
  black: '#1a1f2e',
  red: '#fb7185',
  green: '#4ade80',
  yellow: '#fbbf24',
  blue: '#60a5fa',
  magenta: '#c084fc',
  cyan: '#38bdf8',
  white: '#e2e8f0',
  brightBlack: '#525d78',
  brightRed: '#fda4af',
  brightGreen: '#86efac',
  brightYellow: '#fde047',
  brightBlue: '#93c5fd',
  brightMagenta: '#d8b4fe',
  brightCyan: '#7dd3fc',
  brightWhite: '#ffffff',
}

export function base64ToUint8Array(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}
