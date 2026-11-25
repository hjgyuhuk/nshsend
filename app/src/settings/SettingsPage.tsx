import { Scroll, Text, View } from "nsh";
import { Tab, Tabs } from "nsh/base";
import { t } from "nsh/i18n";
import { ActionButton, SurfaceCard, TextField, ToggleSwitch } from "../ui";
import type { AppSettings } from "../sidecar";
import { changeLocalePreference, LOCALE_ENDONYMS, localePreference } from "./language";
import {
  browseSaveDirectory,
  coreLogLines,
  coreLogPath,
  draftAlias,
  draftHttps,
  draftPin,
  draftPort,
  draftQuickSave,
  draftSaveDir,
  draftVerifyChecksums,
  isLoadingCoreLogs,
  isSavingSettings,
  openCoreLogFile,
  refreshCoreLogs,
  saveSettingsChanges,
  setDraftAlias,
  setDraftHttps,
  setDraftPin,
  setDraftPort,
  setDraftQuickSave,
  setDraftSaveDir,
  setDraftVerifyChecksums,
  settingsError,
  settingsNotice,
} from "./store";

export interface SettingsPageProps {
  onSettingsSaved: (settings: AppSettings) => void;
}

/** Settings tab view for device identity, network port/TLS, save directory, and PIN. */
export function SettingsPage(props: SettingsPageProps) {
  return (
    <Scroll class="flex-1 p-6">
      <View class="gap-4 max-w-2xl">
        <View class="gap-1">
          <Text class="text-xl font-semibold text-foreground">{t("settings.title")}</Text>
          <Text class="text-sm text-muted-foreground">{t("settings.subtitle")}</Text>
        </View>

        <SurfaceCard title={t("settings.language")}>
          <Tabs
            orientation="horizontal"
            class="flex-row gap-1 self-start rounded-(--radius) bg-secondary p-1"
            indicatorClass="inset-0 rounded-(--radius) bg-accent"
          >
            <Tab
              testID="locale-system"
              selected={localePreference() === "system"}
              onPress={() => changeLocalePreference("system")}
              class="group rounded-(--radius) px-3 py-1.5"
            >
              <Text class="text-sm font-medium text-muted-foreground group-selected:text-foreground">
                {t("settings.languageSystem")}
              </Text>
            </Tab>
            {LOCALE_ENDONYMS.map((opt) => (
              <Tab
                testID={`locale-${opt.value}`}
                selected={localePreference() === opt.value}
                onPress={() => changeLocalePreference(opt.value)}
                class="group rounded-(--radius) px-3 py-1.5"
              >
                <Text class="text-sm font-medium text-muted-foreground group-selected:text-foreground">
                  {opt.label}
                </Text>
              </Tab>
            ))}
          </Tabs>
        </SurfaceCard>

        <SurfaceCard title={t("settings.general")}>
          <View class="gap-3">
            <TextField
              testID="settings-alias-input"
              label={t("settings.alias")}
              value={draftAlias()}
              onChange={setDraftAlias}
              placeholder="NshSend Desktop"
            />
            <TextField
              testID="settings-port-input"
              label={t("settings.port")}
              value={draftPort()}
              onChange={setDraftPort}
              placeholder="53317"
              helperText={t("settings.portHelper")}
            />
            <ToggleSwitch
              label={t("settings.httpsToggle")}
              description={t("settings.httpsDesc")}
              checked={draftHttps()}
              onChange={setDraftHttps}
            />
          </View>
        </SurfaceCard>

        <SurfaceCard title={t("settings.receiveSecurity")}>
          <View class="gap-3">
            <View class="flex-row items-end gap-2">
              <View class="flex-1">
                <TextField
                  testID="settings-savedir-input"
                  label={t("settings.saveDir")}
                  value={draftSaveDir()}
                  onChange={setDraftSaveDir}
                />
              </View>
              <ActionButton label={t("settings.browse")} variant="outline" onPress={() => void browseSaveDirectory()} />
            </View>

            <ToggleSwitch
              label={t("receive.quickSave")}
              description={t("settings.quickSaveDesc")}
              checked={draftQuickSave()}
              onChange={setDraftQuickSave}
            />

            <TextField
              label={t("settings.pinLabel")}
              value={draftPin()}
              onChange={setDraftPin}
              masked
              placeholder={t("settings.pinPlaceholder")}
            />

            <ToggleSwitch
              label={t("settings.verifyChecksums")}
              description={t("settings.verifyChecksumsDesc")}
              checked={draftVerifyChecksums()}
              onChange={setDraftVerifyChecksums}
            />
          </View>
        </SurfaceCard>

        <If when={settingsError()}>
          {(err) => (
            <View class="rounded-(--radius) border border-destructive bg-card px-3 py-2">
              <Text class="text-xs text-destructive">{err()}</Text>
            </View>
          )}
        </If>

        <If when={settingsNotice()}>
          {(msg) => (
            <View class="rounded-(--radius) border border-border bg-secondary px-3 py-2">
              <Text class="text-xs text-secondary-foreground">{msg()}</Text>
            </View>
          )}
        </If>

        <View class="flex-row justify-end">
          <ActionButton
            testID="save-settings-btn"
            label={t("settings.saveApply")}
            variant="primary"
            loading={isSavingSettings()}
            onPress={() => void saveSettingsChanges(props.onSettingsSaved)}
          />
        </View>

        <SurfaceCard title={t("settings.diagnostics")} subtitle={coreLogPath() || t("settings.diagnosticsSubtitle")}>
          <View class="flex-row flex-wrap gap-2">
            <ActionButton
              testID="refresh-logs-btn"
              label={t("settings.refreshLogs")}
              variant="outline"
              size="sm"
              loading={isLoadingCoreLogs()}
              onPress={() => void refreshCoreLogs()}
            />
            <If when={coreLogPath()}>
              <ActionButton
                label={t("settings.openLogFile")}
                variant="secondary"
                size="sm"
                onPress={() => void openCoreLogFile()}
              />
            </If>
          </View>
          <If
            when={coreLogLines().length > 0}
            else={<Text class="text-xs text-muted-foreground">{t("settings.logsEmptyHint")}</Text>}
          >
            <View class="rounded-(--radius) bg-secondary p-3 gap-1">
              <Each of={coreLogLines()}>
                {(line) => <Text class="text-xs text-secondary-foreground">{line()}</Text>}
              </Each>
            </View>
          </If>
        </SurfaceCard>
      </View>
    </Scroll>
  );
}
