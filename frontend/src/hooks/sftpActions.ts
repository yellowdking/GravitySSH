import type { SFTPFileEntry } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import type { ConfirmOptions, PromptOptions } from './useActionDialogs'

interface SftpActionDeps {
  sessionId: string
  getCurrentPath: () => string
  getEditingFile: () => { token: number; path: string; content: string } | null
  addBusyPath: (p: string) => void
  removeBusyPath: (p: string) => void
  removeEntryLocally: (p: string) => void
  setEditingFile: (f: { path: string; content: string } | null) => void
  updateEditingFileIfCurrent: (token: number, f: { path: string; content: string }) => void
  beginSaving: (token: number) => boolean
  endSaving: (token: number) => void
  loadDir: (path: string, syncCwd?: boolean, force?: boolean) => Promise<void>
  scheduleUploadRefresh?: (destDir: string) => void
  confirm: (opts: ConfirmOptions) => Promise<boolean>
  prompt: (opts: PromptOptions) => Promise<string | null>
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const inFlightPaths = new Set<string>()

function validateEntryName(raw: string, emptyLabel: string): string | null {
  const t = raw.trim()
  if (!t) return emptyLabel
  if (t === '.' || t === '..') return '名称不能为 "." 或 ".."'
  if (t.includes('/') || t.includes('\0')) return '名称不能包含 "/" 或空字符'
  return null
}

function parentDirOf(remotePath: string, fallbackDir: string): string {
  const slash = remotePath.lastIndexOf('/')
  if (slash > 0) return remotePath.slice(0, slash)
  if (slash === 0) return '/'
  return fallbackDir || '/'
}

export function createSftpActions(d: SftpActionDeps) {
  const lockKey = (path: string) => `${d.sessionId}\0${path}`
  const withBusy = async (path: string, fn: () => Promise<void>) => {
    const key = lockKey(path)
    if (inFlightPaths.has(key)) return
    inFlightPaths.add(key)
    d.addBusyPath(path)
    try { await fn() } finally { inFlightPaths.delete(key); d.removeBusyPath(path) }
  }

  const reload = () => d.loadDir(d.getCurrentPath(), false, true)
  const fail = (label: string, err: unknown) => d.onToast(`${label}: ${errMsg(err)}`, 'error')

  return {
    handleUpload: async () => {
      try {
        const dest = d.getCurrentPath()
        const count = await api.sftpUploadDialog(d.sessionId, dest)
        if (count > 0) {
          d.onToast(`已成功上传 ${count} 个文件`)
          if (d.scheduleUploadRefresh) d.scheduleUploadRefresh(dest)
          else if (d.getCurrentPath() === dest) void reload()
        }
      } catch (err) { fail('上传失败', err) }
    },
    handleDownload: async (entry: SFTPFileEntry) => {
      try {
        const saved = await api.sftpDownloadDialog(d.sessionId, entry.path)
        if (saved) d.onToast(`已下载 ${entry.name} → ${saved}`)
      } catch (err) { fail('下载失败', err) }
    },
    handleDownloadFolder: (entry: SFTPFileEntry) => withBusy(entry.path, async () => {
      try {
        const res = await api.sftpDownloadFolderDialog(d.sessionId, entry.path)
        if (!res.cancelled) d.onToast(`已流式打包下载 ${entry.name}（共 ${res.fileCount || 0} 个文件）→ ${res.localDir}`)
      } catch (err) { fail('打包下载失败', err) }
    }),
    handleEditFile: (entry: SFTPFileEntry) => withBusy(entry.path, async () => {
      try {
        const res = await api.sftpReadTextFile(d.sessionId, entry.path)
        d.setEditingFile({ path: res.path, content: res.content })
      } catch (err) { d.onToast(errMsg(err), 'error') }
    }),
    handleSaveFile: async (content: string) => {
      const currentFile = d.getEditingFile()
      if (!currentFile) return
      const editorToken = currentFile.token
      if (!d.beginSaving(editorToken)) return
      try {
        await api.sftpWriteTextFile(d.sessionId, currentFile.path, content)
        d.updateEditingFileIfCurrent(editorToken, { path: currentFile.path, content })
        d.onToast(`已保存并回传 ${currentFile.path}`)
        void reload()
      } catch (err) { fail('保存失败', err) }
      finally { d.endSaving(editorToken) }
    },
    handleCompress: async (entry: SFTPFileEntry) => {
      const dir = parentDirOf(entry.path, d.getCurrentPath())
      const choice = await d.prompt({
        title: `压缩 "${entry.name}"`,
        message: '选择或输入压缩归档格式（仅支持 tar.gz 或 zip）：',
        defaultValue: 'tar.gz',
        placeholder: 'tar.gz 或 zip',
        presets: ['tar.gz', 'zip'],
        confirmLabel: '开始压缩',
        validate: (v) => {
          const fmt = v.trim().toLowerCase()
          return fmt === 'tar.gz' || fmt === 'zip' ? null : '压缩格式无效，仅支持 tar.gz 或 zip'
        },
      })
      if (!choice) return
      const fmt = choice.trim().toLowerCase()
      if (fmt !== 'tar.gz' && fmt !== 'zip') {
        d.onToast(`不支持的压缩格式: ${choice}`, 'error')
        return
      }
      await withBusy(entry.path, async () => {
        try {
          const archive = await api.sftpCompress(d.sessionId, dir, [entry.name], fmt)
          d.onToast(`已压缩生成 ${archive}`)
          if (d.getCurrentPath() === dir) void reload()
        } catch (err) { fail('压缩失败', err) }
      })
    },
    handleAlignPerms: (entry: SFTPFileEntry) => withBusy(entry.path, async () => {
      try {
        const mode = await api.sftpAlignPermissions(d.sessionId, entry.path)
        d.onToast(`已同步 ${entry.name} 权限为 ${mode}`)
        void reload()
      } catch (err) { fail('同步权限失败', err) }
    }),
    handleExtract: (entry: SFTPFileEntry) => withBusy(entry.path, async () => {
      d.onToast(`正在智能解压 ${entry.name}... 如缺少 unzip 将自动安装`)
      try {
        const msg = await api.sftpExtractArchive(d.sessionId, entry.path)
        d.onToast(msg || `已解压 ${entry.name}`)
        void reload()
      } catch (err) { fail('解压失败', err) }
    }),
    handleMkdir: async () => {
      const baseDir = d.getCurrentPath()
      const name = await d.prompt({
        title: '新建远程文件夹',
        message: '请输入新建文件夹名称：',
        placeholder: '例如：logs',
        confirmLabel: '创建',
        validate: (v) => validateEntryName(v, '文件夹名称不能为空'),
      })
      if (name === null) return
      const trimmed = name.trim()
      const nameErr = validateEntryName(trimmed, '文件夹名称不能为空')
      if (nameErr) { d.onToast(`创建目录失败: ${nameErr}`, 'error'); return }
      const full = baseDir === '/' ? `/${trimmed}` : `${baseDir}/${trimmed}`
      try {
        await api.sftpMkdir(d.sessionId, full)
        d.onToast(`已创建目录 ${trimmed}`)
        if (d.getCurrentPath() === baseDir) void reload()
      } catch (err) { fail('创建目录失败', err) }
    },
    handleRename: async (entry: SFTPFileEntry) => {
      if (inFlightPaths.has(lockKey(entry.path))) return
      const parentDir = parentDirOf(entry.path, d.getCurrentPath())
      const nextName = await d.prompt({
        title: '重命名',
        message: `将 "${entry.name}" 重命名为：`,
        defaultValue: entry.name,
        confirmLabel: '重命名',
        validate: (v) => validateEntryName(v, '名称不能为空'),
      })
      if (nextName === null) return
      const trimmed = nextName.trim()
      if (!trimmed || trimmed === entry.name) return
      const nameErr = validateEntryName(trimmed, '名称不能为空')
      if (nameErr) { d.onToast(`重命名失败: ${nameErr}`, 'error'); return }
      const targetPath = parentDir === '/' ? `/${trimmed}` : `${parentDir}/${trimmed}`
      await withBusy(entry.path, async () => {
        try {
          await api.sftpRename(d.sessionId, entry.path, targetPath)
          d.onToast(`已重命名为 ${trimmed}`)
          if (d.getCurrentPath() === parentDir) await reload()
        } catch (err) { fail('重命名失败', err) }
      })
    },
    handleDelete: async (entry: SFTPFileEntry) => {
      if (inFlightPaths.has(lockKey(entry.path))) return
      const tip = entry.isDir ? `目录 "${entry.name}" 及其所有内容` : `文件 "${entry.name}"`
      const ok = await d.confirm({
        title: '删除远程文件',
        message: `确定要永久删除远程 ${tip} 吗？`,
        confirmLabel: '永久删除',
      })
      if (!ok) return
      await withBusy(entry.path, async () => {
        try {
          await api.sftpDelete(d.sessionId, entry.path)
          d.removeEntryLocally(entry.path)
          d.onToast(`已删除 ${entry.name}`)
          await reload()
        } catch (err) { fail('删除失败', err) }
      })
    },
  }
}