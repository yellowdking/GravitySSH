import { describe, expect, it } from 'vitest'
import { attachTerminalListener, clearAllTerminalBuffers, discardTerminalBuffer, pushTerminalData } from './terminalStream'

describe('terminalStream early buffering', () => {
  it('buffers early chunks before mount and drains them in order on attach', () => {
    const sid = 'sess-early-1'
    pushTerminalData(sid, 'chunk-1')
    pushTerminalData(sid, 'chunk-2')

    const received: string[] = []
    const detach = attachTerminalListener(sid, (d) => received.push(d))
    expect(received).toEqual(['chunk-1', 'chunk-2'])

    pushTerminalData(sid, 'chunk-3')
    expect(received).toEqual(['chunk-1', 'chunk-2', 'chunk-3'])

    detach()
    discardTerminalBuffer(sid)
  })

  it('replays early chunks across StrictMode immediate remount when no live data arrived', () => {
    const sid = 'sess-strict-1'
    pushTerminalData(sid, 'banner')

    const first: string[] = []
    const detach1 = attachTerminalListener(sid, (d) => first.push(d))
    expect(first).toEqual(['banner'])
    detach1()

    const second: string[] = []
    const detach2 = attachTerminalListener(sid, (d) => second.push(d))
    expect(second).toEqual(['banner'])

    pushTerminalData(sid, 'live')
    expect(second).toEqual(['banner', 'live'])
    detach2()
    discardTerminalBuffer(sid)
  })

  it('drops buffered chunks and ignores future chunks after discardTerminalBuffer', () => {
    const sid = 'sess-discard-1'
    pushTerminalData(sid, 'early')
    discardTerminalBuffer(sid)
    pushTerminalData(sid, 'after-discard')

    const received: string[] = []
    const detach = attachTerminalListener(sid, (d) => received.push(d))
    expect(received).toEqual([])
    detach()
    discardTerminalBuffer(sid)
  })

  it('purges all early and live session buffers on clearAllTerminalBuffers (vault lock)', () => {
    pushTerminalData('sess-lock-1', 'secret-scrollback-1')
    pushTerminalData('sess-lock-2', 'secret-scrollback-2')
    clearAllTerminalBuffers()
    pushTerminalData('sess-lock-1', 'late-after-lock')

    const r1: string[] = []
    const d1 = attachTerminalListener('sess-lock-1', (d) => r1.push(d))
    expect(r1).toEqual([])
    d1()
    discardTerminalBuffer('sess-lock-1')
    discardTerminalBuffer('sess-lock-2')
  })
})
