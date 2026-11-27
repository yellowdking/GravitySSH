import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SFTPFileEntry } from '../types'
import { createSftpActions } from './sftpActions'

function makeEntry(path: string, name: string, isDir = false): SFTPFileEntry {
  return {
    name,
    path,
    isDir,
    isSymlink: false,
    size: 128,
    mode: isDir ? 'drwxr-xr-x' : '-rw-r--r--',
    modTime: '2026-01-01T00:00:00Z',
  }
}

describe('sftpActions', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    delete (globalThis as unknown as { window?: unknown }).window
  })

  it('uses scheduleUploadRefresh instead of immediate duplicate loadDir after upload dialog', async () => {
    const SFTPUploadDialog = vi.fn().mockResolvedValue(3)
    ;(globalThis as unknown as { window: unknown }).window = {
      go: { main: { App: { SFTPUploadDialog } } },
    }

    const loadDir = vi.fn().mockResolvedValue(undefined)
    const scheduleUploadRefresh = vi.fn()
    const onToast = vi.fn()
    let currentPath = '/var/www'

    const actions = createSftpActions({
      sessionId: 'sess-upload',
      getCurrentPath: () => currentPath,
      getEditingFile: () => null,
      addBusyPath: vi.fn(),
      removeBusyPath: vi.fn(),
      removeEntryLocally: vi.fn(),
      setEditingFile: vi.fn(),
      updateEditingFileIfCurrent: vi.fn(),
      beginSaving: () => true,
      endSaving: vi.fn(),
      loadDir,
      scheduleUploadRefresh,
      confirm: vi.fn().mockResolvedValue(true),
      prompt: vi.fn().mockResolvedValue(null),
      onToast,
    })

    await actions.handleUpload()
    expect(SFTPUploadDialog).toHaveBeenCalledWith('sess-upload', '/var/www')
    expect(scheduleUploadRefresh).toHaveBeenCalledWith('/var/www')
    expect(loadDir).not.toHaveBeenCalled()
    expect(onToast).toHaveBeenCalledWith('已成功上传 3 个文件')
  })

  it('rejects traversal names (., .., /, \\0) in mkdir and rename, and captures base dir before prompt', async () => {
    const SFTPMkdir = vi.fn().mockResolvedValue(undefined)
    const SFTPRename = vi.fn().mockResolvedValue(undefined)
    ;(globalThis as unknown as { window: unknown }).window = {
      go: { main: { App: { SFTPMkdir, SFTPRename } } },
    }

    let currentPath = '/srv/app'
    let resolvePrompt!: (v: string | null) => void
    const prompt = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<string | null>((r) => {
            resolvePrompt = r
          })
      )
      .mockResolvedValueOnce('..')
      .mockResolvedValueOnce('bad/name')

    const loadDir = vi.fn().mockResolvedValue(undefined)
    const onToast = vi.fn()
    const actions = createSftpActions({
      sessionId: 'sess-path',
      getCurrentPath: () => currentPath,
      getEditingFile: () => null,
      addBusyPath: vi.fn(),
      removeBusyPath: vi.fn(),
      removeEntryLocally: vi.fn(),
      setEditingFile: vi.fn(),
      updateEditingFileIfCurrent: vi.fn(),
      beginSaving: () => true,
      endSaving: vi.fn(),
      loadDir,
      confirm: vi.fn().mockResolvedValue(true),
      prompt,
      onToast,
    })

    // CWD changes while mkdir prompt is open -> must still create under original '/srv/app' and NOT reload '/tmp'
    const mkdirPromise = actions.handleMkdir()
    currentPath = '/tmp'
    resolvePrompt('logs')
    await mkdirPromise
    expect(SFTPMkdir).toHaveBeenCalledWith('sess-path', '/srv/app/logs')
    expect(loadDir).not.toHaveBeenCalled()

    // Rename rejecting '..' and '/'
    const file = makeEntry('/srv/app/config.yaml', 'config.yaml')
    await actions.handleRename(file)
    await actions.handleRename(file)
    expect(SFTPRename).not.toHaveBeenCalled()
    expect(onToast).toHaveBeenCalledWith(expect.stringContaining('"." 或 ".."'), 'error')
    expect(onToast).toHaveBeenCalledWith(expect.stringContaining('"/"'), 'error')
  })

  it('derives compress target directory from entry.path regardless of CWD drift', async () => {
    const SFTPCompress = vi.fn().mockResolvedValue('/data/dir.tar.gz')
    ;(globalThis as unknown as { window: unknown }).window = {
      go: { main: { App: { SFTPCompress } } },
    }

    let currentPath = '/other/dir'
    const loadDir = vi.fn().mockResolvedValue(undefined)
    const actions = createSftpActions({
      sessionId: 'sess-comp',
      getCurrentPath: () => currentPath,
      getEditingFile: () => null,
      addBusyPath: vi.fn(),
      removeBusyPath: vi.fn(),
      removeEntryLocally: vi.fn(),
      setEditingFile: vi.fn(),
      updateEditingFileIfCurrent: vi.fn(),
      beginSaving: () => true,
      endSaving: vi.fn(),
      loadDir,
      confirm: vi.fn().mockResolvedValue(true),
      prompt: vi.fn().mockResolvedValue('tar.gz'),
      onToast: vi.fn(),
    })

    await actions.handleCompress(makeEntry('/data/dir', 'dir', true))
    expect(SFTPCompress).toHaveBeenCalledWith('sess-comp', '/data', ['dir'], 'tar.gz')
    expect(loadDir).not.toHaveBeenCalled()
  })
})