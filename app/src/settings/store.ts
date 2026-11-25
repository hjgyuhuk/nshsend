import { batch, signal } from "nsh";
import { t } from "nsh/i18n";
import { callSidecar, type AppSettings, type BootstrapResult } from "../sidecar";

const aliasDraftSignal = signal("NshSend Desktop");
const portDraftSignal = signal("53317");
const httpsDraftSignal = signal(true);
const saveDirDraftSignal = signal("");
const quickSaveDraftSignal = signal(false);
const pinDraftSignal = signal("");
const verifyChecksumsDraftSignal = signal(true);
const savingSignal = signal(false);
const settingsNoticeSignal = signal<string | null>(null);
const settingsErrorSignal = signal<string | null>(null);
const coreLogPathSignal = signal("");
const coreLogLinesSignal = signal<string[]>([]);
const loadingLogsSignal = signal(false);

export const draftAlias = () => aliasDraftSignal();
export const draftPort = () => portDraftSignal();
export const draftHttps = () => httpsDraftSignal();
export const draftSaveDir = () => saveDirDraftSignal();
export const draftQuickSave = () => quickSaveDraftSignal();
export const draftPin = () => pinDraftSignal();
export const draftVerifyChecksums = () => verifyChecksumsDraftSignal();
export const isSavingSettings = () => savingSignal();
export const settingsNotice = () => settingsNoticeSignal();
export const settingsError = () => settingsErrorSignal();
export const coreLogPath = () => coreLogPathSignal();
export const coreLogLines = () => coreLogLinesSignal();
export const isLoadingCoreLogs = () => loadingLogsSignal();

export function setDraftAlias(v: string): void {
  aliasDraftSignal.set(v);
}
export function setDraftPort(v: string): void {
  portDraftSignal.set(v);
}
export function setDraftHttps(v: boolean): void {
  httpsDraftSignal.set(v);
}
export function setDraftSaveDir(v: string): void {
  saveDirDraftSignal.set(v);
}
export function setDraftQuickSave(v: boolean): void {
  quickSaveDraftSignal.set(v);
}
export function setDraftPin(v: string): void {
  pinDraftSignal.set(v);
}
export function setDraftVerifyChecksums(v: boolean): void {
  verifyChecksumsDraftSignal.set(v);
}

export function hydrateSettingsFromBootstrap(boot: BootstrapResult): void {
  syncDraftFromSettings(boot.settings);
}

export function syncDraftFromSettings(s: AppSettings): void {
  batch(() => {
    aliasDraftSignal.set(s.alias);
    portDraftSignal.set(String(s.port));
    httpsDraftSignal.set(s.https);
    saveDirDraftSignal.set(s.saveDir);
    quickSaveDraftSignal.set(s.quickSave === "on");
    pinDraftSignal.set(s.pin);
    verifyChecksumsDraftSignal.set(s.verifyChecksums);
  });
}

export async function browseSaveDirectory(): Promise<void> {
  try {
    const res = await callSidecar("dialog.pickFolder", undefined, 120_000);
    if (res.path) {
      saveDirDraftSignal.set(res.path);
    }
  } catch (err) {
    settingsErrorSignal.set(err instanceof Error ? err.message : String(err));
  }
}

export async function saveSettingsChanges(onUpdated: (settings: AppSettings) => void): Promise<void> {
  const parsedPort = Number.parseInt(portDraftSignal().trim(), 10);
  if (!Number.isInteger(parsedPort) || parsedPort < 1 || parsedPort > 65535) {
    settingsErrorSignal.set(t("settings.portInvalid"));
    return;
  }

  savingSignal.set(true);
  settingsNoticeSignal.set(null);
  settingsErrorSignal.set(null);

  try {
    const updated = await callSidecar("settings.update", {
      alias: aliasDraftSignal().trim(),
      port: parsedPort,
      https: httpsDraftSignal(),
      saveDir: saveDirDraftSignal().trim(),
      quickSave: quickSaveDraftSignal() ? "on" : "off",
      pin: pinDraftSignal().trim(),
      verifyChecksums: verifyChecksumsDraftSignal(),
    });
    syncDraftFromSettings(updated);
    onUpdated(updated);
    settingsNoticeSignal.set(t("settings.saved"));
  } catch (err) {
    settingsErrorSignal.set(err instanceof Error ? err.message : String(err));
  } finally {
    savingSignal.set(false);
  }
}

export async function refreshCoreLogs(): Promise<void> {
  loadingLogsSignal.set(true);
  try {
    const res = await callSidecar("logs.tail", { limit: 40 });
    coreLogPathSignal.set(res.logFilePath);
    coreLogLinesSignal.set(res.lines);
  } catch (err) {
    settingsErrorSignal.set(err instanceof Error ? err.message : String(err));
  } finally {
    loadingLogsSignal.set(false);
  }
}

export async function openCoreLogFile(): Promise<void> {
  const path = coreLogPathSignal();
  if (!path) return;
  try {
    await callSidecar("system.openPath", { path });
  } catch (err) {
    settingsErrorSignal.set(err instanceof Error ? err.message : String(err));
  }
}
