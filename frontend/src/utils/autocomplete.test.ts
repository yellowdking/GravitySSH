import { describe, expect, it } from 'vitest'
import {
  applyTerminalInputToLine,
  computeCompletionInsert,
  formatBytes,
  getActiveTokenPrefix,
  sanitizePasteText,
} from './autocomplete'

describe('autocomplete helpers', () => {
  it('computes clean suffix insertion for prefix matches', () => {
    expect(computeCompletionInsert('apt ', 'apt update')).toEqual({
      backspaces: 0,
      insertText: 'update',
    })
    expect(computeCompletionInsert('dock', 'docker ps -a')).toEqual({
      backspaces: 0,
      insertText: 'er ps -a',
    })
    expect(computeCompletionInsert('apk a', 'apk add --no-cache ')).toEqual({
      backspaces: 0,
      insertText: 'dd --no-cache ',
    })
  })

  it('completes directory and file paths on command tail without duplicating prefixes', () => {
    expect(computeCompletionInsert('cd doc', 'cd docker-compose/')).toEqual({
      backspaces: 0,
      insertText: 'ker-compose/',
    })
    expect(computeCompletionInsert('nano /etc/ng', 'nano /etc/nginx/')).toEqual({
      backspaces: 0,
      insertText: 'inx/',
    })
    expect(computeCompletionInsert('cd ', 'cd logs/')).toEqual({
      backspaces: 0,
      insertText: 'logs/',
    })
    expect(computeCompletionInsert('cd DOC', 'cd docker-compose/')).toEqual({
      backspaces: 3,
      insertText: 'docker-compose/',
    })
    expect(computeCompletionInsert('nano /etc/conf', 'nano /etc/nginx.conf')).toEqual({
      backspaces: 4,
      insertText: 'nginx.conf',
    })
  })

  it('uses Unicode code-point case-sensitive LCP for CJK, emoji, and chained commands', () => {
    expect(computeCompletionInsert('cd 项目', 'cd 项目文档/')).toEqual({
      backspaces: 0,
      insertText: '文档/',
    })
    expect(computeCompletionInsert('cd 项目A', 'cd 项目文档/')).toEqual({
      backspaces: 1,
      insertText: '文档/',
    })
    expect(computeCompletionInsert('cd 🚀a', 'cd 🚀b')).toEqual({
      backspaces: 1,
      insertText: 'b',
    })
    expect(computeCompletionInsert('cd 🚀', 'cd 📦')).toEqual({
      backspaces: 1,
      insertText: '📦',
    })
    expect(computeCompletionInsert('cd /opt && Docker c', 'cd /opt && docker compose ')).toEqual({
      backspaces: 8,
      insertText: 'docker compose ',
    })
  })

  it('replaces typed token when selecting a substring match', () => {
    expect(computeCompletionInsert('compose', 'docker compose up -d')).toEqual({
      backspaces: 7,
      insertText: 'docker compose up -d',
    })
  })

  it('tracks local terminal input and clears on horizontal arrows, Home/End, and Tab', () => {
    let line = ''
    line = applyTerminalInputToLine(line, 'dock')
    expect(line).toBe('dock')
    line = applyTerminalInputToLine(line, 'er p')
    expect(line).toBe('docker p')
    line = applyTerminalInputToLine(line, '\x7f')
    expect(line).toBe('docker ')
    expect(applyTerminalInputToLine('cd /opt', '\x1b[C')).toBe('')
    expect(applyTerminalInputToLine('cd /opt', '\x1b[D')).toBe('')
    expect(applyTerminalInputToLine('cd /opt', '\x1b[H')).toBe('')
    expect(applyTerminalInputToLine('cd /opt', '\x1b[F')).toBe('')
    expect(applyTerminalInputToLine('cd /opt', '\x1b[1~')).toBe('')
    expect(applyTerminalInputToLine('cd /opt', '\x1b[4~')).toBe('')
    expect(applyTerminalInputToLine('cd /opt', '\t')).toBe('')
    expect(applyTerminalInputToLine('cd 🚀', '\x7f')).toBe('cd ')
    line = applyTerminalInputToLine(line, '\r')
    expect(line).toBe('')
  })

  it('unwraps bracketed paste sequences so single-line pastes stay in sync and multi-line pastes reset cleanly', () => {
    expect(applyTerminalInputToLine('cd ', '\x1b[200~/var/log/ng\x1b[201~')).toBe('cd /var/log/ng')
    expect(applyTerminalInputToLine('', '\x1b[200~docker compose \x1b[201~')).toBe('docker compose ')
    expect(applyTerminalInputToLine('cd ', '\x1b[200~项目文档\x1b[201~')).toBe('cd 项目文档')
    expect(applyTerminalInputToLine('cd ', '\x1b[200~\x1b[201~')).toBe('cd ')
    expect(applyTerminalInputToLine('cd ', '\x1b[200~line1\nline2\x1b[201~')).toBe('')
    expect(applyTerminalInputToLine('cd ', '\x1b[200~ls -la\r\x1b[201~')).toBe('')
    expect(applyTerminalInputToLine('cd ', '\x1b[200~foo\x1b[Dbar\x1b[201~')).toBe('')
  })

  it('extracts active token prefix by source for cyan prefix highlighting', () => {
    expect(getActiveTokenPrefix('cd /opt/ng', 'dir')).toBe('ng')
    expect(getActiveTokenPrefix('cd /opt/', 'dir')).toBe('')
    expect(getActiveTokenPrefix('nano /etc/nginx.c', 'file')).toBe('nginx.c')
    expect(getActiveTokenPrefix('nano doc', 'file')).toBe('doc')
    expect(getActiveTokenPrefix('docker c', 'subcmd')).toBe('c')
    expect(getActiveTokenPrefix('docker ', 'subcmd')).toBe('')
    expect(getActiveTokenPrefix('ls -l', 'flag')).toBe('-l')
    expect(getActiveTokenPrefix('cd /opt && dock', 'builtin')).toBe('dock')
    expect(getActiveTokenPrefix('docker co', 'history_local')).toBe('docker co')
    expect(getActiveTokenPrefix('cd /opt && docker co', 'history')).toBe('docker co')
    expect(getActiveTokenPrefix('   ', 'dir')).toBe('')
  })

  it('formats byte sizes cleanly', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(2048)).toBe('2.0 KB')
    expect(formatBytes(1024 * 1024 * 3.5)).toBe('3.5 MB')
  })

  it('resets input buffer on readline control keys, unlisted ESC sequences, and embedded ESC', () => {
    expect(applyTerminalInputToLine('cd /opt', '\x01')).toBe('') // Ctrl+A
    expect(applyTerminalInputToLine('cd /opt', '\x02')).toBe('') // Ctrl+B
    expect(applyTerminalInputToLine('cd /opt', '\x05')).toBe('') // Ctrl+E
    expect(applyTerminalInputToLine('cd /opt', '\x0b')).toBe('') // Ctrl+K
    expect(applyTerminalInputToLine('cd /opt', '\x12')).toBe('') // Ctrl+R
    expect(applyTerminalInputToLine('cd /opt', '\x01sudo ')).toBe('')
    expect(applyTerminalInputToLine('cd /opt', '\x1b[1;5D')).toBe('') // Ctrl+Left
    expect(applyTerminalInputToLine('cd /opt', '\x1b[3~')).toBe('') // Delete
    expect(applyTerminalInputToLine('cd /opt', '\x1b\x7f')).toBe('') // Alt+Backspace
    expect(applyTerminalInputToLine('cd /opt', '\x1bb')).toBe('') // Alt+B
    expect(applyTerminalInputToLine('cd /opt', 'a\x1b[Db')).toBe('') // embedded ESC
    expect(applyTerminalInputToLine('cd /opt', '\x9bD')).toBe('') // C1 CSI
  })

  it('normalizes CRLF/CR and loop-strips nested 7-bit and C1 bracketed paste escapes', () => {
    expect(sanitizePasteText('line1\r\nline2\r\n  indented\rline3')).toBe('line1\nline2\n  indented\nline3')
    expect(sanitizePasteText('a\x1b[201~b\x1b[200~c')).toBe('abc')
    // Nested breakout payloads must be stripped until fixed point
    expect(sanitizePasteText('safe\x1b[20\x1b[201~1~\nevil\n')).toBe('safe\nevil\n')
    expect(sanitizePasteText('safe\x1b\x1b[200~[201~evil')).toBe('safeevil')
    expect(sanitizePasteText('safe\x9b201~evil\x9b200~')).toBe('safeevil')
    expect(sanitizePasteText('safe\x9b20\x9b201~1~evil')).toBe('safeevil')
    expect(sanitizePasteText('x\x1b[20\x1b[20\x1b[201~1~1~y')).toBe('xy')
  })
})
