import { Scroll, Text, View } from "nsh";
import { t } from "nsh/i18n";
import { ActionButton, ModalSheet, ProgressMeter, StatusBadge, SurfaceCard, ToggleSwitch } from "../ui";
import {
  activeReceiveProgress,
  cancelActiveReceive,
  copyLastReceivedText,
  formatByteSize,
  lastReceiveStatus,
  lastReceivedText,
  pendingIncomingOffer,
  receiveAlias,
  receiveFingerprint,
  receiveQuickSave,
  receiveServerState,
  receiveTextCopied,
  respondToPendingOffer,
  toggleQuickSaveMode,
} from "./store";

/** Receive tab view showing local node status, quick-save toggle, and active transfer progress. */
export function ReceivePage() {
  const shortFingerprint = () => {
    const fp = receiveFingerprint();
    return fp ? fp.slice(0, 8) : "--------";
  };

  return (
    <Scroll class="flex-1 p-6">
      <View class="gap-4 max-w-2xl">
        <View class="gap-1">
          <Text class="text-xl font-semibold text-foreground">{t("receive.title")}</Text>
          <Text class="text-sm text-muted-foreground">{t("receive.subtitle")}</Text>
        </View>

        <SurfaceCard title={receiveAlias()} subtitle={t("receive.fingerprint", { fingerprint: shortFingerprint() })}>
          <View class="flex-row flex-wrap items-center gap-2">
            <StatusBadge
              label={receiveServerState().running ? t("receive.listening", { port: receiveServerState().port }) : t("receive.serverStopped")}
              tone={receiveServerState().running ? "success" : "destructive"}
            />
            <StatusBadge
              label={receiveServerState().https ? "HTTPS / mTLS" : t("receive.httpPlain")}
              tone={receiveServerState().https ? "primary" : "warning"}
            />
          </View>

          <If when={receiveServerState().error}>
            <Text class="text-xs text-destructive">{receiveServerState().error}</Text>
          </If>

          <View class="gap-1 pt-1">
            <Text class="text-xs font-medium text-muted-foreground">{t("receive.localIps")}</Text>
            <If
              when={receiveServerState().localAddresses.length > 0}
              else={<Text class="text-xs text-muted-foreground">{t("receive.noInterface")}</Text>}
            >
              <Each of={receiveServerState().localAddresses}>
                {(ip) => <Text class="text-xs text-foreground">{`${ip()}:${receiveServerState().port}`}</Text>}
              </Each>
            </If>
          </View>
        </SurfaceCard>

        <SurfaceCard>
          <ToggleSwitch
            testID="quick-save-switch"
            label={t("receive.quickSave")}
            description={t("receive.quickSaveDesc")}
            checked={receiveQuickSave()}
            onChange={(checked) => {
              void toggleQuickSaveMode(checked);
            }}
          />
        </SurfaceCard>

        <If when={activeReceiveProgress()}>
          {(progress) => (
            <SurfaceCard title={t("receive.inProgress")} subtitle={t("common.fileProgress", { current: progress().completedFiles + 1, total: progress().totalFiles })}>
              <ProgressMeter
                label={progress().fileName}
                bytesTransferred={progress().bytesTransferred}
                totalBytes={progress().totalBytes}
                detailText={`${formatByteSize(progress().bytesTransferred)} / ${formatByteSize(progress().totalBytes)}`}
              />
              <View class="flex-row justify-end">
                <ActionButton label={t("receive.cancelReceive")} variant="destructive" size="sm" onPress={() => void cancelActiveReceive()} />
              </View>
            </SurfaceCard>
          )}
        </If>

        <If when={lastReceivedText()}>
          {(textBody) => (
            <SurfaceCard title={t("receive.lastText")}>
              <Text class="text-sm text-foreground">{textBody()}</Text>
              <View class="flex-row justify-end">
                <ActionButton
                  label={receiveTextCopied() ? t("receive.copied") : t("receive.copyText")}
                  variant="secondary"
                  size="sm"
                  onPress={() => void copyLastReceivedText()}
                />
              </View>
            </SurfaceCard>
          )}
        </If>

        <If when={lastReceiveStatus()}>
          {(msg) => (
            <View class="rounded-(--radius) border border-border bg-secondary px-3 py-2">
              <Text class="text-xs text-secondary-foreground">{msg()}</Text>
            </View>
          )}
        </If>
      </View>
    </Scroll>
  );
}

/** Global modal prompt for incoming file/text offers, usable from any active tab. */
export function IncomingOfferDialog() {
  return (
    <If when={pendingIncomingOffer()}>
      {(offer) => {
        const items = () => Object.values(offer().files);
        const totalSize = () => items().reduce((sum, item) => sum + item.size, 0);
        return (
          <ModalSheet
            open={true}
            title={t("receive.offerTitle", { alias: offer().senderAlias })}
            description={t("receive.offerDescription", { ip: offer().senderIp, count: items().length, size: formatByteSize(totalSize()) })}
            onClose={() => void respondToPendingOffer(false)}
            actions={
              <View class="flex-row gap-2">
                <ActionButton
                  testID="decline-offer-btn"
                  label={t("receive.decline")}
                  variant="outline"
                  onPress={() => void respondToPendingOffer(false)}
                />
                <ActionButton
                  testID="accept-offer-btn"
                  label={t("receive.accept")}
                  variant="primary"
                  onPress={() => void respondToPendingOffer(true)}
                />
              </View>
            }
          >
            <View class="gap-2">
              <StatusBadge
                label={offer().senderVerified ? t("receive.mtlsVerified") : t("receive.unverified")}
                tone={offer().senderVerified ? "success" : "warning"}
              />
              <View class="gap-1 max-h-48">
                <Each of={items()} key={(f) => f.id}>
                  {(file) => (
                    <View class="flex-row items-center justify-between gap-2 rounded-(--radius) bg-secondary px-3 py-1.5">
                      <Text class="flex-1 text-xs text-foreground line-clamp-1">
                        {file().fileName}
                      </Text>
                      <Text class="text-xs text-muted-foreground">{formatByteSize(file().size)}</Text>
                    </View>
                  )}
                </Each>
              </View>
            </View>
          </ModalSheet>
        );
      }}
    </If>
  );
}
