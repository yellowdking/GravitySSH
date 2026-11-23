import { describe, expect, it } from 'vitest'
import { applyTerminalInputToLine, computeCompletionInsert, formatBytes } from './autocomplete'

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

  it('replaces typed token when selecting a substring match', () => {
    expect(computeCompletionInsert('compose', 'docker compose up -d')).toEqual({
      backspaces: 7,
      insertText: 'docker compose up -d',
    })
  })

  it('tracks local terminal input accurately for instant suggestions', () => {
    let line = ''
    line = applyTerminalInputToLine(line, 'dock')
    expect(line).toBe('dock')
    line = applyTerminalInputToLine(line, 'er p')
    expect(line).toBe('docker p')
    line = applyTerminalInputToLine(line, '\x7f')
    expect(line).toBe('docker ')
    line = applyTerminalInputToLine(line, '\r')
    expect(line).toBe('')
  })

  it('formats byte sizes cleanly', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(2048)).toBe('2.0 KB')
    expect(formatBytes(1024 * 1024 * 3.5)).toBe('3.5 MB')
  })
})
