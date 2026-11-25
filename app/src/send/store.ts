import { Clipboard, signal } from "nsh";
import { t } from "nsh/i18n";
import { fileURLToPath } from "url";
import {
  callSidecar,
  type BootstrapResult,
  type PeerDeviceView,
  type ProtocolType,
  type SendEndEvent,
  type SendProgressEvent,
  type StagedItemInput,
} from "../sidecar";

export interface StagedEntry extends StagedItemInput {
  id: string;
  label: string;
}

const stagedEntriesSignal = signal<StagedEntry[]>([]);
const nearbyDevicesSignal = signal<PeerDeviceView[]>([]);
const scanningSignal = signal(false);
const activeSendIdSignal = signal<string | null>(null);
const activeSendProgressSignal = signal<SendProgressEvent | null>(null);
const sendStatusMessageSignal = signal<string | null>(null);
const pinRequiredSignal = signal(false);
const pinPromptTargetSignal = signal<PeerDeviceView | null>(null);

let stagedSeq = 1;

export const stagedEntries = () => stagedEntriesSignal();
export const nearbyDevices = () => nearbyDevicesSignal();
export const isScanningPeers = () => scanningSignal();
export const activeSendProgress = () => activeSendProgressSignal();
export const activeSendTransferId = () => activeSendIdSignal();
export const sendStatusMessage = () => sendStatusMessageSignal();
export const sendPinRequired = () => pinRequiredSignal();
export const pinPromptTarget = () => pinPromptTargetSignal();

export function hydrateSendFromBootstrap(boot: BootstrapResult): void {
  nearbyDevicesSignal.set(boot.devices);
}

export function applyDevicesUpdatedEvent(devices: PeerDeviceView[]): void {
  nearbyDevicesSignal.set(devices);
}

export function applySendProgressEvent(progress: SendProgressEvent): void {
  activeSendProgressSignal.set(progress);
}

export function applySendEndedEvent(end: SendEndEvent): void {
  activeSendProgressSignal.set(null);
  const currentId = activeSendIdSignal();
  if (currentId === end.transferId) {
    activeSendIdSignal.set(null);
  }
  const needsPin = end.status === "pin_required" || Boolean(end.pinRequired);
  pinRequiredSignal.set(needsPin);
  if (end.status === "finished") {
    sendStatusMessageSignal.set(t("send.statusFinished"));
    stagedEntriesSignal.set([]);
  } else if (end.status === "cancelled") {
    sendStatusMessageSignal.set(t("send.statusCancelled"));
  } else if (needsPin) {
    sendStatusMessageSignal.set(t("send.statusPinRequired"));
  } else {
    sendStatusMessageSignal.set(t("send.statusFailed", { error: end.error ?? t("common.unknownError") }));
  }
}

export function stageFilePaths(paths: string[]): void {
  const existing = stagedEntriesSignal();
  const next = [...existing];
  for (const path of paths) {
    if (!path) continue;
    if (next.some((item) => item.kind === "file" && item.path === path)) {
      continue;
    }
    const parts = path.split(/[\\/]/);
    const label = parts[parts.length - 1] || path;
    next.push({
      id: `staged-${stagedSeq++}`,
      kind: "file",
      path,
      label,
    });
  }
  stagedEntriesSignal.set(next);
}

export function stageFileDropUris(uris: string[]): void {
  const paths: string[] = [];
  for (const uri of uris) {
    try {
      paths.push(fileURLToPath(uri));
    } catch {
      if (uri && !uri.includes("://")) {
        paths.push(uri);
      }
    }
  }
  stageFilePaths(paths);
}

export function stageTextMessage(text: string): void {
  const trimmed = text.trim();
  if (!trimmed) return;
  const preview = trimmed.length > 48 ? `${trimmed.slice(0, 48)}...` : trimmed;
  stagedEntriesSignal.set([
    ...stagedEntriesSignal(),
    {
      id: `staged-${stagedSeq++}`,
      kind: "text",
      text: trimmed,
      name: "message.txt",
      label: preview,
    },
  ]);
}

export function removeStagedEntry(id: string): void {
  stagedEntriesSignal.set(stagedEntriesSignal().filter((item) => item.id !== id));
}

export function clearStagedEntries(): void {
  stagedEntriesSignal.set([]);
}

export async function pickFilesToStage(): Promise<void> {
  try {
    const res = await callSidecar("dialog.pickFiles", { multiple: true }, 120_000);
    if (res.paths && res.paths.length > 0) {
      stageFilePaths(res.paths);
    }
  } catch (err) {
    sendStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function pickFolderToStage(): Promise<void> {
  try {
    const res = await callSidecar("dialog.pickFolder", undefined, 120_000);
    if (res.path) {
      stageFilePaths([res.path]);
    }
  } catch (err) {
    sendStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function pasteClipboardFilesToStage(): Promise<void> {
  try {
    const files = await Clipboard.readFiles();
    if (files.length > 0) {
      stageFilePaths(files);
      return;
    }
    const text = await Clipboard.readText();
    if (text.trim()) {
      stageTextMessage(text);
    } else {
      sendStatusMessageSignal.set(t("send.clipboardEmpty"));
    }
  } catch (err) {
    sendStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function scanNearbyPeers(): Promise<void> {
  scanningSignal.set(true);
  sendStatusMessageSignal.set(null);
  try {
    await callSidecar("discovery.scan", undefined);
  } catch (err) {
    sendStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  } finally {
    scanningSignal.set(false);
  }
}

export async function registerManualPeer(host: string, port: number, protocol: ProtocolType): Promise<boolean> {
  try {
    await callSidecar("discovery.registerManual", { host: host.trim(), port, protocol });
    sendStatusMessageSignal.set(null);
    return true;
  } catch (err) {
    sendStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
    return false;
  }
}

export async function sendStagedToPeer(peer: PeerDeviceView, pin?: string): Promise<void> {
  const staged = stagedEntriesSignal();
  if (staged.length === 0) {
    sendStatusMessageSignal.set(t("send.stageFirst"));
    return;
  }
  sendStatusMessageSignal.set(null);
  pinRequiredSignal.set(false);
  pinPromptTargetSignal.set(null);

  const items: StagedItemInput[] = staged.map(({ kind, path, text, name }) => ({
    kind,
    path,
    text,
    name,
  }));

  try {
    const res = await callSidecar("send.start", {
      targetFingerprint: peer.fingerprint,
      host: peer.host,
      port: peer.port,
      protocol: peer.protocol,
      items,
      pin,
    });
    activeSendIdSignal.set(res.transferId);
  } catch (err) {
    sendStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export function openPinPromptForPeer(peer: PeerDeviceView): void {
  pinPromptTargetSignal.set(peer);
}

export function closePinPrompt(): void {
  pinPromptTargetSignal.set(null);
}

export async function cancelActiveSend(): Promise<void> {
  const id = activeSendIdSignal();
  if (!id) return;
  try {
    await callSidecar("send.cancel", { transferId: id });
  } catch (err) {
    sendStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}
