import { Clipboard, signal } from "nsh";
import { t } from "nsh/i18n";
import {
  callSidecar,
  type AppSettings,
  type BootstrapResult,
  type IncomingOfferEvent,
  type ReceiveProgressEvent,
  type ReceiveSessionEndEvent,
  type ServerStateSnapshot,
} from "../sidecar";

const localFingerprintSignal = signal("");
const localAliasSignal = signal("NshSend Desktop");
const quickSaveSignal = signal(false);
const serverStateSignal = signal<ServerStateSnapshot>({
  running: false,
  port: 53317,
  https: true,
  localAddresses: [],
});
const pendingOfferSignal = signal<IncomingOfferEvent | null>(null);
const activeProgressSignal = signal<ReceiveProgressEvent | null>(null);
const lastReceivedTextSignal = signal<string | null>(null);
const lastStatusMessageSignal = signal<string | null>(null);
const copiedNoticeSignal = signal(false);

export const receiveFingerprint = () => localFingerprintSignal();
export const receiveAlias = () => localAliasSignal();
export const receiveQuickSave = () => quickSaveSignal();
export const receiveServerState = () => serverStateSignal();
export const pendingIncomingOffer = () => pendingOfferSignal();
export const activeReceiveProgress = () => activeProgressSignal();
export const lastReceivedText = () => lastReceivedTextSignal();
export const lastReceiveStatus = () => lastStatusMessageSignal();
export const receiveTextCopied = () => copiedNoticeSignal();

export function hydrateReceiveFromBootstrap(boot: BootstrapResult): void {
  localFingerprintSignal.set(boot.fingerprint);
  applySettingsToReceive(boot.settings);
  serverStateSignal.set(boot.server);
}

export function applySettingsToReceive(settings: AppSettings): void {
  localAliasSignal.set(settings.alias);
  quickSaveSignal.set(settings.quickSave === "on");
}

export function applyServerStateEvent(state: ServerStateSnapshot): void {
  serverStateSignal.set(state);
}

export function applyIncomingOfferEvent(offer: IncomingOfferEvent): void {
  pendingOfferSignal.set(offer);
}

export function applyOfferAbortedEvent(sessionId: string): void {
  const current = pendingOfferSignal();
  if (current && current.sessionId === sessionId) {
    pendingOfferSignal.set(null);
  }
}

export function applyReceiveProgressEvent(progress: ReceiveProgressEvent): void {
  activeProgressSignal.set(progress);
}

export function applyReceiveEndedEvent(end: ReceiveSessionEndEvent): void {
  activeProgressSignal.set(null);
  const current = pendingOfferSignal();
  if (current && current.sessionId === end.sessionId) {
    pendingOfferSignal.set(null);
  }
  if (end.textBody) {
    lastReceivedTextSignal.set(end.textBody);
    copiedNoticeSignal.set(false);
  }
  if (end.reason === "finished") {
    const count = end.files?.length ?? 0;
    lastStatusMessageSignal.set(
      count > 0
        ? t("receive.statusFinished", { count, dir: end.savedDir ?? "" })
        : t("receive.statusFinishedText"),
    );
  } else if (end.reason === "cancelled") {
    lastStatusMessageSignal.set(t("receive.statusCancelled"));
  } else if (end.reason === "failed") {
    lastStatusMessageSignal.set(t("receive.statusFailed", { error: end.error ?? t("common.unknownError") }));
  }
}

export async function toggleQuickSaveMode(next: boolean): Promise<void> {
  const prev = quickSaveSignal();
  quickSaveSignal.set(next);
  try {
    const updated = await callSidecar("settings.update", {
      quickSave: next ? "on" : "off",
    });
    applySettingsToReceive(updated);
  } catch (err) {
    quickSaveSignal.set(prev);
    lastStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function respondToPendingOffer(accept: boolean): Promise<void> {
  const offer = pendingOfferSignal();
  if (!offer) return;
  pendingOfferSignal.set(null);
  const acceptedFileIds = accept ? Object.keys(offer.files) : [];
  try {
    await callSidecar("receive.respond", {
      sessionId: offer.sessionId,
      accept,
      acceptedFileIds,
    });
  } catch (err) {
    lastStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function cancelActiveReceive(): Promise<void> {
  const progress = activeProgressSignal();
  if (!progress) return;
  try {
    await callSidecar("receive.cancel", { sessionId: progress.sessionId });
  } catch (err) {
    lastStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function copyLastReceivedText(): Promise<void> {
  const text = lastReceivedTextSignal();
  if (!text) return;
  try {
    await Clipboard.writeText(text);
    copiedNoticeSignal.set(true);
  } catch (err) {
    lastStatusMessageSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export function formatByteSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
