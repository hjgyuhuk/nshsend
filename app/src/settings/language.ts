import { signal, Storage } from "nsh";
import { setLocale } from "nsh/i18n";

/** The user's interface-language choice: follow the device, or a fixed locale. */
export type LocalePreference = "system" | "zh-CN" | "en";

const LOCALE_STORAGE_KEY = "nshsend.locale";
const localePreferenceSignal = signal<LocalePreference>("system");

export const localePreference = () => localePreferenceSignal();

/** Fixed locales offered in the picker, each labelled in its own language. */
export const LOCALE_ENDONYMS: ReadonlyArray<{ value: Exclude<LocalePreference, "system">; label: string }> = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en", label: "English" },
];

function applyLocalePreference(pref: LocalePreference): void {
  localePreferenceSignal.set(pref);
  setLocale(pref === "system" ? null : pref);
}

/** Restores the saved choice at startup; best-effort, keeps the system default on failure. */
export async function restoreLocalePreference(): Promise<void> {
  try {
    const saved = await Storage.getItem(LOCALE_STORAGE_KEY);
    if (saved === "zh-CN" || saved === "en" || saved === "system") {
      applyLocalePreference(saved);
    }
  } catch (err) {
    console.warn("Failed to restore locale preference:", err);
  }
}

/** Applies a choice immediately and persists it for the next launch. */
export function changeLocalePreference(pref: LocalePreference): void {
  applyLocalePreference(pref);
  Storage.setItem(LOCALE_STORAGE_KEY, pref).catch((err) => {
    console.warn("Failed to persist locale preference:", err);
  });
}