import type { RemoteFileContent, SFTPListResult } from './types'

export function callGo<T>(method: string, ...args: unknown[]): Promise<T> {
  const fn = window.go?.main?.App?.[method]
  if (!fn) {
    return Promise.reject(new Error(`Wails runtime 未就绪: ${method}`))
  }
  return fn(...args) as Promise<T>
}

export const sftpApi = {
  sftpList: (sessionId: string, remotePath: string) => callGo<SFTPListResult>('SFTPList', sessionId, remotePath),
  sftpMkdir: (sessionId: string, remotePath: string) => callGo<void>('SFTPMkdir', sessionId, remotePath),
  sftpRename: (sessionId: string, oldPath: string, newPath: string) =>
    callGo<void>('SFTPRename', sessionId, oldPath, newPath),
  sftpDelete: (sessionId: string, remotePath: string) => callGo<void>('SFTPDelete', sessionId, remotePath),
  sftpAlignPermissions: (sessionId: string, remotePath: string) =>
    callGo<string>('SFTPAlignPermissions', sessionId, remotePath),
  sftpExtractArchive: (sessionId: string, remotePath: string) =>
    callGo<string>('SFTPExtractArchive', sessionId, remotePath),
  sftpReadTextFile: (sessionId: string, remotePath: string) =>
    callGo<RemoteFileContent>('SFTPReadTextFile', sessionId, remotePath),
  sftpWriteTextFile: (sessionId: string, remotePath: string, content: string) =>
    callGo<void>('SFTPWriteTextFile', sessionId, remotePath, content),
  sftpCompress: (sessionId: string, remoteDir: string, names: string[], format: 'tar.gz' | 'zip') =>
    callGo<string>('SFTPCompress', sessionId, remoteDir, names, format),
  sftpDownloadFolderDialog: (sessionId: string, remotePath: string) =>
    callGo<{ cancelled: boolean; localDir?: string; fileCount?: number }>('SFTPDownloadFolderDialog', sessionId, remotePath),
  sftpUploadDialog: (sessionId: string, remoteDir: string) =>
    callGo<number>('SFTPUploadDialog', sessionId, remoteDir),
  sftpUploadFiles: (sessionId: string, localPaths: string[], remoteDir: string) =>
    callGo<number>('SFTPUploadFiles', sessionId, localPaths, remoteDir),
  sftpDownloadDialog: (sessionId: string, remotePath: string) =>
    callGo<string>('SFTPDownloadDialog', sessionId, remotePath),
}
