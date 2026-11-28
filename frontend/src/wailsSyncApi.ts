import type {
  SyncDirectSendResult,
  SyncFlowSession,
  SyncFlowStatus,
  SyncImportSummary,
  SyncInboxItem,
  SyncOverview,
  SyncPingInfo,
  SyncRelayStatus,
} from './syncTypes'
import { callGo } from './wailsSftpApi'

export const syncApi = {
  getSyncOverview: () => callGo<SyncOverview>('GetSyncOverview'),
  setCustomSyncRelay: (rawUri: string) => callGo<SyncRelayStatus>('SetCustomSyncRelay', rawUri),
  testSyncRelay: (rawUri: string) => callGo<SyncPingInfo>('TestSyncRelay', rawUri),
  setSyncDeviceLabel: (label: string) => callGo<void>('SetSyncDeviceLabel', label),
  listSyncInbox: () => callGo<SyncInboxItem[]>('ListSyncInbox'),
  startWatchSyncInbox: () => callGo<void>('StartWatchSyncInbox'),
  stopWatchSyncInbox: () => callGo<void>('StopWatchSyncInbox'),
  sendSyncToDevice: (deviceId: string, deliveryMode: string) =>
    callGo<SyncDirectSendResult>('SendSyncToDevice', deviceId, deliveryMode),
  applyInboxSync: (itemId: string, importMode: 'merge' | 'overwrite') =>
    callGo<SyncImportSummary>('ApplyInboxSync', itemId, importMode),
  dismissInboxItem: (itemId: string) => callGo<void>('DismissInboxItem', itemId),
  revokeSyncDevice: (deviceId: string) => callGo<void>('RevokeSyncDevice', deviceId),
  startSyncDeviceBind: (mode: 1 | 2, includeRelay: boolean) =>
    callGo<SyncFlowSession>('StartSyncDeviceBind', mode, includeRelay),
  acceptSyncDeviceBind: (codeOrUri: string) =>
    callGo<SyncFlowSession>('AcceptSyncDeviceBind', codeOrUri),
  startSyncOneShotSend: (mode: 1 | 2, includeRelay: boolean) =>
    callGo<SyncFlowSession>('StartSyncOneShotSend', mode, includeRelay),
  startSyncReceive: (codeOrUri: string) =>
    callGo<SyncFlowSession>('StartSyncReceive', codeOrUri),
  getSyncFlowStatus: (sessionId: string) =>
    callGo<SyncFlowStatus>('GetSyncFlowStatus', sessionId),
  submitSyncSasInput: (sessionId: string, sasCode: string) =>
    callGo<void>('SubmitSyncSASInput', sessionId, sasCode),
  confirmSyncSas: (sessionId: string, approved: boolean) =>
    callGo<void>('ConfirmSyncSAS', sessionId, approved),
  cancelSyncFlow: (sessionId: string) => callGo<void>('CancelSyncFlow', sessionId),
  applySyncReceive: (previewId: string, mode: 'merge' | 'overwrite') =>
    callGo<SyncImportSummary>('ApplySyncReceive', previewId, mode),
  clearSyncPreview: (previewId: string) => callGo<void>('ClearSyncPreview', previewId),
}
