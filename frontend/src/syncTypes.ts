export interface SyncSendSession {
  code: string
  qrText: string
  expiresAt: string
  relayUrl: string
  serverCount: number
  keyCount: number
  shortcutCount: number
  templateCount: number
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
