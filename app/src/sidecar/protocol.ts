export type ProtocolType = "http" | "https";
export type DeviceType = "mobile" | "desktop" | "web" | "headless" | "server";
export type QuickSaveMode = "off" | "on";

export interface AppSettings {
  alias: string;
  port: number;
  https: boolean;
  saveDir: string;
  quickSave: QuickSaveMode;
  pin: string;
  multicastGroup: string;
  multicastPort: number;
  verifyChecksums: boolean;
  deviceModel: string;
}

export type SettingsPatch = Partial<Omit<AppSettings, "deviceModel">>;

export interface ServerStateSnapshot {
  running: boolean;
  port: number;
  https: boolean;
  localAddresses: string[];
  error?: string;
}

export interface PeerDeviceView {
  fingerprint: string;
  alias: string;
  version: string;
  deviceModel?: string;
  deviceType?: DeviceType;
  host: string;
  port: number;
  protocol: ProtocolType;
  verified: boolean;
  download: boolean;
  lastSeenMs: number;
}

export interface FileOfferV2 {
  id: string;
  fileName: string;
  size: number;
  fileType: string;
  sha256?: string;
  preview?: string;
}

export interface IncomingOfferEvent {
  sessionId: string;
  senderAlias: string;
  senderIp: string;
  senderDeviceModel?: string;
  senderDeviceType?: DeviceType;
  senderFingerprint: string;
  senderVerified: boolean;
  files: Record<string, FileOfferV2>;
}

export interface ReceiveProgressEvent {
  sessionId: string;
  fileId: string;
  fileName: string;
  bytesTransferred: number;
  totalBytes: number;
  completedFiles: number;
  totalFiles: number;
}

export interface ReceiveSessionEndEvent {
  sessionId: string;
  reason: "finished" | "cancelled" | "failed";
  savedDir?: string;
  files?: string[];
  textBody?: string;
  error?: string;
}

export interface SendProgressEvent {
  transferId: string;
  fileId: string;
  fileName: string;
  bytesTransferred: number;
  totalBytes: number;
  completedFiles: number;
  totalFiles: number;
}

export interface SendEndEvent {
  transferId: string;
  status: "finished" | "cancelled" | "pin_required" | "failed";
  error?: string;
  pinRequired?: boolean;
}

export interface HistoryEntry {
  id: string;
  fileName: string;
  fileType: string;
  size: number;
  savedPath?: string;
  textPreview?: string;
  senderAlias: string;
  timestampMs: number;
  isText: boolean;
}

export interface StagedItemInput {
  kind: "file" | "text";
  path?: string;
  text?: string;
  name?: string;
}

export interface LogsTailResult {
  logFilePath: string;
  lines: string[];
}

export interface BootstrapResult {
  fingerprint: string;
  settings: AppSettings;
  server: ServerStateSnapshot;
  devices: PeerDeviceView[];
  history: HistoryEntry[];
}

export interface SidecarEventMap {
  "server.state": ServerStateSnapshot;
  "devices.updated": PeerDeviceView[];
  "receive.incoming": IncomingOfferEvent;
  "receive.aborted": { sessionId: string };
  "receive.progress": ReceiveProgressEvent;
  "receive.ended": ReceiveSessionEndEvent;
  "send.progress": SendProgressEvent;
  "send.ended": SendEndEvent;
  "history.updated": HistoryEntry[];
}

export type SidecarEventName = keyof SidecarEventMap;

export interface SidecarRequestMap {
  "core.bootstrap": { params: undefined; result: BootstrapResult };
  "discovery.scan": { params: undefined; result: { ok: boolean } };
  "discovery.registerManual": {
    params: { host: string; port: number; protocol: ProtocolType };
    result: PeerDeviceView;
  };
  "send.start": {
    params: {
      targetFingerprint: string;
      host: string;
      port: number;
      protocol: ProtocolType;
      items: StagedItemInput[];
      pin?: string;
    };
    result: { transferId: string };
  };
  "send.cancel": { params: { transferId: string }; result: { cancelled: boolean } };
  "receive.respond": {
    params: { sessionId: string; accept: boolean; acceptedFileIds?: string[] };
    result: { ok: boolean };
  };
  "receive.cancel": { params: { sessionId: string }; result: { cancelled: boolean } };
  "settings.update": { params: SettingsPatch; result: AppSettings };
  "history.list": { params: undefined; result: HistoryEntry[] };
  "history.remove": { params: { id: string }; result: HistoryEntry[] };
  "history.clear": { params: undefined; result: HistoryEntry[] };
  "dialog.pickFiles": { params: { multiple: boolean }; result: { paths: string[] } };
  "dialog.pickFolder": { params: undefined; result: { path: string } };
  "system.openPath": { params: { path: string }; result: { ok: boolean } };
  "system.revealPath": { params: { path: string }; result: { ok: boolean } };
  "logs.tail": { params: { limit?: number }; result: LogsTailResult };
}
