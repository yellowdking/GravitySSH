export interface SyncRelayStatus {
  mode: 'default' | 'custom'
  endpoint: string
  redactedUri: string
  relayId: string
  tlsMode: string
  spkiPinShort: string
  certExpiresAt?: string
  certRotateRecommended?: boolean
}

export interface SyncPairedDevice {
  deviceId: string
  label: string
  platform: string
  relayId: string
  issuedAt: number
  pairedAt?: number
}

export interface SyncOverview {
  localDeviceId: string
  localDeviceLabel: string
  relay: SyncRelayStatus
  pairedDevices: SyncPairedDevice[]
}

export interface SyncPingInfo {
  rttMs: number
  profile: string
  spoolMode: string
  clockOffsetSec: number
  certExpiresAt?: string
  certRotateRecommended?: boolean
}

export interface SyncInboxItem {
  itemId: string
  msgId: string
  senderDeviceId: string
  senderLabel: string
  senderPlatform: string
  createdAt: number
  expiresAt: number
  totalBytes: number
  serverCount: number
  keyCount: number
  shortcutCount: number
  templateCount: number
  pairingBroken?: boolean
  errorMessage?: string
}

export interface SyncDirectSendResult {
  status: 'sent_spool' | 'suggest_pipe' | 'pipe_offer_ready'
  msgId?: string
  sessionId?: string
  pairUri?: string
  roomCode?: string
  expiresAt?: string
  message?: string
}

export type SyncFlowState =
  | 'waiting_peer'
  | 'awaiting_sas_input'
  | 'awaiting_sas_confirm'
  | 'transferring'
  | 'ready_preview'
  | 'completed'
  | 'error'
  | 'cancelled'

export interface SyncFlowSession {
  sessionId: string
  purpose: string
  role: 'initiator' | 'responder' | string
  mode: number
  state: SyncFlowState | string
  pairUri?: string
  roomCode?: string
  expiresAt?: string
  serverCount?: number
  keyCount?: number
  shortcutCount?: number
  templateCount?: number
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

export interface SyncFlowStatus {
  sessionId?: string
  purpose?: string
  role?: string
  mode?: number
  state: SyncFlowState
  pairUri?: string
  roomCode?: string
  expiresAt?: string
  sas?: string
  sasAttemptsRemaining?: number
  bytesDone?: number
  bytesTotal?: number
  pairedDevice?: SyncPairedDevice
  preview?: SyncReceivePreview
  message?: string
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
