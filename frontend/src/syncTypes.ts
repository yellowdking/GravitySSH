export interface SyncSendSession {
  sessionId: string
  code: string
  qrText: string
  expiresAt: string
  relayUrl: string
  serverCount: number
  keyCount: number
  shortcutCount: number
  templateCount: number
}

export interface SyncSendPollResult {
  status: 'waiting' | 'requested' | 'approved' | 'rejected' | 'consumed' | 'expired' | 'error' | string
  requestId?: string
  receiverCode?: string
  receiverDevice?: string
  receiverPlatform?: string
  requestedAt?: string
  message?: string
}

export interface SyncReceiveWaitSession {
  waitId: string
  receiverCode: string
  expiresAt: string
}

export interface SyncReceivePollResult {
  status: 'pending' | 'rejected' | 'expired' | 'ready' | string
  preview?: SyncReceivePreview
  message?: string
}

export interface SyncReceivePreview {
  previewId: string
  schemaVersion: number
  exportedAt: string
  sourcePlatform: string
  sourceDevice: string
  serverCount: number
  keyCount: number
  shortcutCount: number
  templateCount: number
}

export interface SyncImportSummary {
  mode: 'merge' | 'overwrite' | string
  serversAdded: number
  serversUpdated: number
  keysAdded: number
  keysUpdated: number
  shortcutsAdded: number
  shortcutsUpdated: number
  templatesAdded: number
  templatesUpdated: number
}
