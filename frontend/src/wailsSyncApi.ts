import type {
  SyncImportSummary,
  SyncReceivePollResult,
  SyncReceivePreview,
  SyncReceiveWaitSession,
  SyncSendPollResult,
  SyncSendSession,
} from './syncTypes'
import { callGo } from './wailsSftpApi'

export const syncApi = {
  getDefaultSyncRelayUrl: () => callGo<string>('GetDefaultSyncRelayURL'),
  testSyncRelay: (relayUrl: string) => callGo<number>('TestSyncRelay', relayUrl),
  startSyncSend: (relayUrl: string) => callGo<SyncSendSession>('StartSyncSend', relayUrl),
  pollSyncSend: (sessionId: string) => callGo<SyncSendPollResult>('PollSyncSend', sessionId),
  approveSyncSend: (sessionId: string, requestId: string, receiverCode: string) =>
    callGo<void>('ApproveSyncSend', sessionId, requestId, receiverCode),
  rejectSyncSend: (sessionId: string, requestId: string) =>
    callGo<void>('RejectSyncSend', sessionId, requestId),
  cancelSyncSend: (sessionIdOrCode: string, relayUrl: string) =>
    callGo<void>('CancelSyncSend', sessionIdOrCode, relayUrl),
  generateReceiverCode: () => callGo<string>('GenerateReceiverCode'),
  startSyncReceive: (sendCode: string, recvCode: string, relayUrl: string) =>
    callGo<SyncReceiveWaitSession>('StartSyncReceive', sendCode, recvCode, relayUrl),
  pollSyncReceive: (waitId: string) => callGo<SyncReceivePollResult>('PollSyncReceive', waitId),
  cancelSyncReceive: (waitId: string) => callGo<void>('CancelSyncReceive', waitId),
  previewSyncReceive: (code: string, relayUrl: string) =>
    callGo<SyncReceivePreview>('PreviewSyncReceive', code, relayUrl),
  applySyncReceive: (previewId: string, mode: 'merge' | 'overwrite') =>
    callGo<SyncImportSummary>('ApplySyncReceive', previewId, mode),
  clearSyncPreview: (previewId: string) => callGo<void>('ClearSyncPreview', previewId),
}
