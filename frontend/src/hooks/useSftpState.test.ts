import { describe, expect, it } from 'vitest'
import type { TransferProgress } from '../types'
import {
  markRunningTransfersInterrupted,
  pruneTransferMap,
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
