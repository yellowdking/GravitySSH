import type { SyncImportSummary, SyncReceivePreview, SyncSendSession } from './syncTypes'
import { callGo } from './wailsSftpApi'

export const syncApi = {
  getDefaultSyncRelayUrl: () => callGo<string>('GetDefaultSyncRelayURL'),
  startSyncSend: (relayUrl: string) => callGo<SyncSendSession>('StartSyncSend', relayUrl),
  cancelSyncSend: (code: string, relayUrl: string) => callGo<void>('CancelSyncSend', code, relayUrl),
  previewSyncReceive: (code: string, relayUrl: string) =>
    callGo<SyncReceivePreview>('PreviewSyncReceive', code, relayUrl),
  applySyncReceive: (previewId: string, mode: 'merge' | 'overwrite') =>
    callGo<SyncImportSummary>('ApplySyncReceive', previewId, mode),
}
