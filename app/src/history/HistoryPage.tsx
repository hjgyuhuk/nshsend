import { locale, Scroll, Text, View } from "nsh";
import { t } from "nsh/i18n";
import { ActionButton, EmptyPlaceholder, StatusBadge, SurfaceCard } from "../ui";
import {
  clearAllHistory,
  copyHistoryText,
  historyEntries,
  historyStatusMessage,
  openSavedFile,
  removeHistoryItem,
  revealSavedFile,
} from "./store";

function formatHistorySize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatHistoryTime(ms: number): string {
  try {
    return new Date(ms).toLocaleString(locale());
  } catch {
    return String(ms);
  }
}

/** History tab view listing received files and text snippets. */
export function HistoryPage() {
  return (
    <Scroll class="flex-1 p-6">
      <View class="gap-4 max-w-2xl">
        <View class="flex-row items-center justify-between gap-2">
          <View class="gap-1">
            <Text class="text-xl font-semibold text-foreground">{t("history.title")}</Text>
            <Text class="text-sm text-muted-foreground">{t("history.subtitle")}</Text>
          </View>
          <If when={historyEntries().length > 0}>
            <ActionButton
              testID="clear-history-btn"
              label={t("history.clearAll")}
              variant="outline"
              size="sm"
              onPress={() => void clearAllHistory()}
            />
          </If>
        </View>

        <If when={historyStatusMessage()}>
          {(msg) => (
            <View class="rounded-(--radius) border border-border bg-secondary px-3 py-2">
              <Text class="text-xs text-secondary-foreground">{msg()}</Text>
            </View>
          )}
        </If>

        <If
          when={historyEntries().length > 0}
          else={<EmptyPlaceholder title={t("history.empty")} description={t("history.emptyHint")} />}
        >
          <View class="gap-2">
            <Each of={historyEntries()} key={(entry) => entry.id}>
              {(entry) => (
                <SurfaceCard>
                  <View class="flex-row items-start justify-between gap-2">
                    <View class="flex-1 gap-1">
                      <View class="flex-row items-center gap-2">
                        <Text class="text-sm font-medium text-foreground line-clamp-1">
                          {entry().fileName}
                        </Text>
                        <StatusBadge label={entry().isText ? t("history.textBadge") : formatHistorySize(entry().size)} tone="neutral" />
                      </View>
                      <Text class="text-xs text-muted-foreground">
                        {t("history.from", { alias: entry().senderAlias, time: formatHistoryTime(entry().timestampMs) })}
                      </Text>
                      <If when={entry().isText && entry().textPreview}>
                        <Text class="text-xs text-foreground line-clamp-2">
                          {entry().textPreview}
                        </Text>
                      </If>
                    </View>
                  </View>

                  <View class="flex-row flex-wrap justify-end gap-2">
                    <If when={entry().isText && entry().textPreview}>
                      <ActionButton
                        label={t("common.copy")}
                        variant="secondary"
                        size="sm"
                        onPress={() => void copyHistoryText(entry().textPreview!)}
                      />
                    </If>
                    <If when={!entry().isText && entry().savedPath}>
                      <ActionButton
                        label={t("common.open")}
                        variant="secondary"
                        size="sm"
                        onPress={() => void openSavedFile(entry().savedPath!)}
                      />
                      <ActionButton
                        label={t("history.reveal")}
                        variant="outline"
                        size="sm"
                        onPress={() => void revealSavedFile(entry().savedPath!)}
                      />
                    </If>
                    <ActionButton
                      label={t("common.delete")}
                      variant="ghost"
                      size="sm"
                      onPress={() => void removeHistoryItem(entry().id)}
                    />
                  </View>
                </SurfaceCard>
              )}
            </Each>
          </View>
        </If>
      </View>
    </Scroll>
  );
}
