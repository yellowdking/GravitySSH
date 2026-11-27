import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TransferProgress } from '../types'
import {
  createUploadRefreshCoalescer,
  markRunningTransfersInterrupted,
  normalizeSftpPath,
  pruneTransferMap,
  shouldIgnoreCwdSync,
  shouldSkipDuplicateSftpList,
} from './useSftpState'

function makeTransfer(id: string, status: TransferProgress['status'], error = ''): TransferProgress {
  return {
    transferId: id,
    sessionId: 'sess-1',
    direction: 'upload',
    fileName: `${id}.txt`,
    remotePath: `/tmp/${id}.txt`,
    localPath: `C:\\tmp\\${id}.txt`,
    transferred: status === 'done' ? 100 : 50,
    total: 100,
    percent: status === 'done' ? 100 : 50,
    status,
    error,
  }
}

describe('SFTP transfer pruning & disconnect cleanup', () => {
  it('caps error and done items while preserving all running transfers', () => {
    const map: Record<string, TransferProgress> = {
      run1: makeTransfer('run1', 'running'),
      err1: makeTransfer('err1', 'error', 'fail 1'),
      err2: makeTransfer('err2', 'error', 'fail 2'),
      err3: makeTransfer('err3', 'error', 'fail 3'),
      err4: makeTransfer('err4', 'error', 'fail 4'),
      done1: makeTransfer('done1', 'done'),
      done2: makeTransfer('done2', 'done'),
      done3: makeTransfer('done3', 'done'),
      run2: makeTransfer('run2', 'running'),
    }

    const { next, evictedIds } = pruneTransferMap(map, 2, 2)
    expect(evictedIds).toEqual(['err1', 'err2', 'done1'])
    expect(Object.keys(next)).toEqual(['run1', 'err3', 'err4', 'done2', 'done3', 'run2'])
  })

  it('marks stuck running transfers as interrupted errors on disconnect', () => {
    const map: Record<string, TransferProgress> = {
      run1: makeTransfer('run1', 'running'),
      done1: makeTransfer('done1', 'done'),
    }

    const updated = markRunningTransfersInterrupted(map)
    expect(updated.run1.status).toBe('error')
    expect(updated.run1.error).toContain('连接已断开')
    expect(updated.done1.status).toBe('done')

    // No-op when no running transfers remain
    expect(markRunningTransfersInterrupted(updated)).toBe(updated)
  })
})

describe('SFTPList dedupe & upload refresh coalescing', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('normalizes paths and deduplicates concurrent SFTPList and session:cwd on connect', () => {
    expect(normalizeSftpPath('/root/', '/root')).toBe('/root')
    expect(normalizeSftpPath('', '/root')).toBe('/root')
    expect(normalizeSftpPath('~', '/home/ubuntu/')).toBe('/home/ubuntu')
    expect(normalizeSftpPath('/', '/root')).toBe('/')

    // Initial connect loads '/root' while currentPath is still '/'; session:cwd('/root') must be ignored!
    expect(shouldIgnoreCwdSync('/root', '/', '/root', '/root')).toBe(true)
    expect(shouldIgnoreCwdSync('/root/', '/', '/root', '/root')).toBe(true)
    expect(shouldIgnoreCwdSync('/var/log', '/', '/root', '/root')).toBe(false)
    expect(shouldIgnoreCwdSync('/root', '/root', null, '/root')).toBe(true)

    // Duplicate non-forced SFTPList for the already in-flight path is skipped; force=true bypasses
    expect(shouldSkipDuplicateSftpList('/root/', '/root', '/root', false)).toBe(true)
    expect(shouldSkipDuplicateSftpList('/root', '/root', '/root', true)).toBe(false)
    expect(shouldSkipDuplicateSftpList('/var', '/root', '/root', false)).toBe(false)
  })

  it('coalesces N upload-done events plus dialog completion into 1 single refresh and supports cancel', () => {
    const onRefresh = vi.fn()
    const coalescer = createUploadRefreshCoalescer(150, onRefresh)

    // Simulate 5-file batch upload: 5 sftp:progress done + 1 dialog resolve
    for (let i = 0; i < 5; i++) {
      coalescer.schedule('/srv/uploads')
      vi.advanceTimersByTime(30)
    }
    coalescer.schedule('/srv/uploads')
    expect(onRefresh).not.toHaveBeenCalled()

    vi.advanceTimersByTime(150)
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(onRefresh).toHaveBeenCalledWith('/srv/uploads')

    // Navigating away cancels pending upload refresh
    coalescer.schedule('/srv/uploads')
    vi.advanceTimersByTime(50)
    coalescer.cancel()
    vi.advanceTimersByTime(500)
    expect(onRefresh).toHaveBeenCalledTimes(1)
  })
})
