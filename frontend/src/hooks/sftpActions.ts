import type { SFTPFileEntry } from '../types'
import { api } from '../wailsApi'

interface SftpActionDeps {
  sessionId: string
  getCurrentPath: () => string
  editingFile: { path: string; content: string } | null
  addBusyPath: (p: string) => void
  removeBusyPath: (p: string) => void
  removeEntryLocally: (p: string) => void
  setEditingFile: (f: { path: string; content: string } | null) => void
  setSavingFile: (v: boolean) => void
  loadDir: (path: string, syncCwd?: boolean) => Promise<void>
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const inFlightPaths = new Set<string>()

function errMsg(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
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

  const reload = () => d.loadDir(d.getCurrentPath(), false)
  const fail = (label: string, err: unknown) => d.onToast(`${label}: ${errMsg(err)}`, 'error')

  return {
    handleUpload: async () => {
      try {
        const count = await api.sftpUploadDialog(d.sessionId, d.getCurrentPath())
        if (count > 0) { d.onToast(`已成功上传 ${count} 个文件`); void reload() }
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
      if (!d.editingFile) return
      d.setSavingFile(true)
      try {
        await api.sftpWriteTextFile(d.sessionId, d.editingFile.path, content)
        d.setEditingFile({ path: d.editingFile.path, content })
        d.onToast(`已保存并回传 ${d.editingFile.path}`)
        void reload()
      } catch (err) { fail('保存失败', err) }
      finally { d.setSavingFile(false) }
    },
    handleCompress: async (entry: SFTPFileEntry) => {
      const choice = window.prompt(`压缩 "${entry.name}"，输入格式：tar.gz 或 zip`, 'tar.gz')
      if (!choice) return
      const fmt = choice.trim().toLowerCase() === 'zip' ? 'zip' : 'tar.gz'
      const dir = d.getCurrentPath()
      await withBusy(entry.path, async () => {
        try {
          const archive = await api.sftpCompress(d.sessionId, dir, [entry.name], fmt)
          d.onToast(`已压缩生成 ${archive}`)
          void reload()
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
      const name = window.prompt('请输入新建文件夹名称：')
      if (!name?.trim()) return
      const cur = d.getCurrentPath()
      const full = cur === '/' ? `/${name.trim()}` : `${cur}/${name.trim()}`
      try { await api.sftpMkdir(d.sessionId, full); d.onToast(`已创建目录 ${name.trim()}`); void reload() }
      catch (err) { fail('创建目录失败', err) }
    },
    handleRename: async (entry: SFTPFileEntry) => {
      if (inFlightPaths.has(lockKey(entry.path))) return
      const nextName = window.prompt('重命名为：', entry.name)
      const trimmed = nextName?.trim()
      if (!trimmed || trimmed === entry.name) return
      if (trimmed.includes('/')) { d.onToast('重命名失败: 名称不能包含 "/"', 'error'); return }
      const cur = d.getCurrentPath()
      const slash = entry.path.lastIndexOf('/')
      const parent = slash > 0 ? entry.path.slice(0, slash) : (slash === 0 ? '' : (cur === '/' ? '' : cur))
      await withBusy(entry.path, async () => {
        try {
          await api.sftpRename(d.sessionId, entry.path, `${parent}/${trimmed}`)
          d.onToast(`已重命名为 ${trimmed}`)
          await reload()
        } catch (err) { fail('重命名失败', err) }
      })
    },
    handleDelete: async (entry: SFTPFileEntry) => {
      if (inFlightPaths.has(lockKey(entry.path))) return
      const tip = entry.isDir ? `目录 "${entry.name}" 及其所有内容` : `文件 "${entry.name}"`
      if (!window.confirm(`确定要永久删除远程 ${tip} 吗？`)) return
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
