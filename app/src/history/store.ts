import { Clipboard, signal } from "nsh";
import { t } from "nsh/i18n";
import { callSidecar, type BootstrapResult, type HistoryEntry } from "../sidecar";

const historyEntriesSignal = signal<HistoryEntry[]>([]);
const historyStatusSignal = signal<string | null>(null);

export const historyEntries = () => historyEntriesSignal();
export const historyStatusMessage = () => historyStatusSignal();

export function hydrateHistoryFromBootstrap(boot: BootstrapResult): void {
  historyEntriesSignal.set(boot.history);
}

export function applyHistoryUpdatedEvent(entries: HistoryEntry[]): void {
  historyEntriesSignal.set(entries);
}

export async function removeHistoryItem(id: string): Promise<void> {
  try {
    const next = await callSidecar("history.remove", { id });
    historyEntriesSignal.set(next);
  } catch (err) {
    historyStatusSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function clearAllHistory(): Promise<void> {
  try {
    const next = await callSidecar("history.clear", undefined);
    historyEntriesSignal.set(next);
  } catch (err) {
    historyStatusSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function openSavedFile(path: string): Promise<void> {
  try {
    await callSidecar("system.openPath", { path });
  } catch (err) {
    historyStatusSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function revealSavedFile(path: string): Promise<void> {
  try {
    await callSidecar("system.revealPath", { path });
  } catch (err) {
    historyStatusSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function copyHistoryText(text: string): Promise<void> {
  try {
    await Clipboard.writeText(text);
    historyStatusSignal.set(t("history.copiedNotice"));
  } catch (err) {
    historyStatusSignal.set(err instanceof Error ? err.message : String(err));
  }
}
