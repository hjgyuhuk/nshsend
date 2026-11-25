import { Scroll, Text, View, signal } from "nsh";
import { t } from "nsh/i18n";
import {
  ActionButton,
  EmptyPlaceholder,
  ModalSheet,
  ProgressMeter,
  StatusBadge,
  SurfaceCard,
  TextField,
  ToggleSwitch,
} from "../ui";
import type { PeerDeviceView, ProtocolType } from "../sidecar";
import {
  activeSendProgress,
  activeSendTransferId,
  cancelActiveSend,
  clearStagedEntries,
  closePinPrompt,
  isScanningPeers,
  nearbyDevices,
  openPinPromptForPeer,
  pasteClipboardFilesToStage,
  pickFilesToStage,
  pickFolderToStage,
  pinPromptTarget,
  registerManualPeer,
  removeStagedEntry,
  scanNearbyPeers,
  sendStagedToPeer,
  sendStatusMessage,
  sendPinRequired,
  stageFileDropUris,
  stageTextMessage,
  stagedEntries,
} from "./store";

/** Send tab view with file/text staging, drag-and-drop, peer discovery, and PIN prompt. */
export function SendPage() {
  const draggingOver = signal(false);
  const textModalOpen = signal(false);
  const textDraft = signal("");
  const manualModalOpen = signal(false);
  const manualHost = signal("");
  const manualPort = signal("53317");
  const manualHttps = signal(true);
  const pinInput = signal("");
  const lastSelectedPeer = signal<PeerDeviceView | null>(null);

  const handleSendClick = (peer: PeerDeviceView) => {
    lastSelectedPeer.set(peer);
    void sendStagedToPeer(peer);
  };

  return (
    <Scroll class="flex-1 p-6">
      <View class="gap-4 max-w-2xl">
        <View class="gap-1">
          <Text class="text-xl font-semibold text-foreground">{t("send.title")}</Text>
          <Text class="text-sm text-muted-foreground">{t("send.subtitle")}</Text>
        </View>

        <View
          onFileDragEnter={() => draggingOver.set(true)}
          onFileDragLeave={() => draggingOver.set(false)}
          onFileDrop={(e) => {
            draggingOver.set(false);
            stageFileDropUris(e.uris);
          }}
          class={`rounded-(--radius) border p-4 gap-3 ${
            draggingOver() ? "border-primary bg-accent/40" : "border-border bg-card"
          }`}
        >
          <View class="flex-row flex-wrap items-center justify-between gap-2">
            <Text class="text-sm font-medium text-foreground">
              {draggingOver() ? t("send.dropHint") : t("send.stageHint")}
            </Text>
            <If when={stagedEntries().length > 0}>
              <ActionButton label={t("common.clear")} variant="ghost" size="sm" onPress={clearStagedEntries} />
            </If>
          </View>

          <View class="flex-row flex-wrap gap-2">
            <ActionButton testID="pick-files-btn" label={t("send.pickFiles")} variant="secondary" size="sm" onPress={() => void pickFilesToStage()} />
            <ActionButton testID="pick-folder-btn" label={t("send.pickFolder")} variant="secondary" size="sm" onPress={() => void pickFolderToStage()} />
            <ActionButton label={t("send.clipboard")} variant="secondary" size="sm" onPress={() => void pasteClipboardFilesToStage()} />
            <ActionButton testID="add-text-btn" label={t("send.addText")} variant="secondary" size="sm" onPress={() => textModalOpen.set(true)} />
          </View>

          <If
            when={stagedEntries().length > 0}
            else={<Text class="text-xs text-muted-foreground">{t("send.stageEmpty")}</Text>}
          >
            <View class="gap-1.5">
              <Each of={stagedEntries()} key={(item) => item.id}>
                {(item) => (
                  <View class="flex-row items-center justify-between gap-2 rounded-(--radius) bg-secondary px-3 py-1.5">
                    <Text class="flex-1 text-xs text-foreground line-clamp-1">
                      {item().kind === "text" ? t("send.stagedText", { preview: item().label }) : item().label}
                    </Text>
                    <ActionButton label={t("common.remove")} variant="ghost" size="sm" onPress={() => removeStagedEntry(item().id)} />
                  </View>
                )}
              </Each>
            </View>
          </If>
        </View>

        <If when={activeSendProgress()}>
          {(progress) => (
            <SurfaceCard title={t("send.inProgress")} subtitle={t("common.fileProgress", { current: progress().completedFiles + 1, total: progress().totalFiles })}>
              <ProgressMeter
                label={progress().fileName}
                bytesTransferred={progress().bytesTransferred}
                totalBytes={progress().totalBytes}
              />
              <View class="flex-row justify-end">
                <ActionButton label={t("send.cancelSend")} variant="destructive" size="sm" onPress={() => void cancelActiveSend()} />
              </View>
            </SurfaceCard>
          )}
        </If>

        <If when={activeSendTransferId() && !activeSendProgress()}>
          <SurfaceCard title={t("send.waitingConfirm")}>
            <Text class="text-xs text-muted-foreground">{t("send.waitingConfirmDesc")}</Text>
            <View class="flex-row justify-end">
              <ActionButton label={t("send.cancelRequest")} variant="destructive" size="sm" onPress={() => void cancelActiveSend()} />
            </View>
          </SurfaceCard>
        </If>

        <If when={sendStatusMessage()}>
          {(msg) => (
            <View class="flex-row items-center justify-between gap-2 rounded-(--radius) border border-border bg-secondary px-3 py-2">
              <Text class="flex-1 text-xs text-secondary-foreground">{msg()}</Text>
              <If when={sendPinRequired() && lastSelectedPeer()}>
                <ActionButton
                  label={t("send.enterPin")}
                  variant="primary"
                  size="sm"
                  onPress={() => {
                    const p = lastSelectedPeer();
                    if (p) openPinPromptForPeer(p);
                  }}
                />
              </If>
            </View>
          )}
        </If>

        <SurfaceCard title={t("send.nearbyDevices")} subtitle={t("send.nearbyHint")}>
          <View class="flex-row flex-wrap gap-2">
            <ActionButton
              testID="scan-peers-btn"
              label={t("send.scan")}
              variant="outline"
              size="sm"
              loading={isScanningPeers()}
              onPress={() => void scanNearbyPeers()}
            />
            <ActionButton
              testID="manual-peer-btn"
              label={t("send.manualAdd")}
              variant="outline"
              size="sm"
              onPress={() => manualModalOpen.set(true)}
            />
          </View>

          <If
            when={nearbyDevices().length > 0}
            else={
              <EmptyPlaceholder
                title={t("send.noDevices")}
                description={t("send.noDevicesHint")}
              />
            }
          >
            <View class="gap-2">
              <Each of={nearbyDevices()} key={(d) => d.fingerprint}>
                {(peer) => (
                  <View
                    onPress={() => handleSendClick(peer())}
                    class="flex-row items-center justify-between gap-3 rounded-(--radius) border border-border bg-background p-3 hover:bg-secondary active:bg-muted"
                  >
                    <View class="flex-1 gap-0.5">
                      <Text class="text-sm font-medium text-foreground">{peer().alias}</Text>
                      <Text class="text-xs text-muted-foreground">
                        {`${peer().host}:${peer().port} · ${peer().deviceModel || peer().deviceType || "desktop"}`}
                      </Text>
                    </View>
                    <StatusBadge
                      label={peer().verified ? t("send.httpsVerified") : peer().protocol.toUpperCase()}
                      tone={peer().verified ? "success" : "neutral"}
                    />
                  </View>
                )}
              </Each>
            </View>
          </If>
        </SurfaceCard>
      </View>

      <ModalSheet
        open={textModalOpen()}
        title={t("send.textModalTitle")}
        onClose={() => textModalOpen.set(false)}
        actions={
          <View class="flex-row gap-2">
            <ActionButton label={t("common.cancel")} variant="outline" onPress={() => textModalOpen.set(false)} />
            <ActionButton
              testID="confirm-add-text-btn"
              label={t("common.add")}
              variant="primary"
              onPress={() => {
                stageTextMessage(textDraft());
                textDraft.set("");
                textModalOpen.set(false);
              }}
            />
          </View>
        }
      >
        <TextField
          testID="text-draft-input"
          label={t("send.textLabel")}
          value={textDraft()}
          onChange={(v) => textDraft.set(v)}
          multiline
          placeholder={t("send.textPlaceholder")}
        />
      </ModalSheet>

      <ModalSheet
        open={manualModalOpen()}
        title={t("send.manualModalTitle")}
        description={t("send.manualModalDesc")}
        onClose={() => manualModalOpen.set(false)}
        actions={
          <View class="flex-row gap-2">
            <ActionButton label={t("common.cancel")} variant="outline" onPress={() => manualModalOpen.set(false)} />
            <ActionButton
              label={t("send.connectAdd")}
              variant="primary"
              onPress={async () => {
                const portNum = Number.parseInt(manualPort(), 10) || 53317;
                const proto: ProtocolType = manualHttps() ? "https" : "http";
                const ok = await registerManualPeer(manualHost(), portNum, proto);
                if (ok) {
                  manualHost.set("");
                  manualModalOpen.set(false);
                }
              }}
            />
          </View>
        }
      >
        <View class="gap-3">
          <TextField label={t("send.ipLabel")} value={manualHost()} onChange={(v) => manualHost.set(v)} placeholder="192.168.1.10" />
          <TextField label={t("send.portLabel")} value={manualPort()} onChange={(v) => manualPort.set(v)} placeholder="53317" />
          <ToggleSwitch label={t("send.useHttps")} checked={manualHttps()} onChange={(v) => manualHttps.set(v)} />
        </View>
      </ModalSheet>

      <ModalSheet
        open={Boolean(pinPromptTarget())}
        title={t("send.pinTitle")}
        description={pinPromptTarget() ? t("send.pinDesc", { alias: pinPromptTarget()!.alias }) : undefined}
        onClose={closePinPrompt}
        actions={
          <View class="flex-row gap-2">
            <ActionButton label={t("common.cancel")} variant="outline" onPress={closePinPrompt} />
            <ActionButton
              label={t("send.submitSend")}
              variant="primary"
              onPress={() => {
                const peer = pinPromptTarget();
                const pinVal = pinInput();
                pinInput.set("");
                if (peer) void sendStagedToPeer(peer, pinVal);
              }}
            />
          </View>
        }
      >
        <TextField label={t("send.pinLabel")} value={pinInput()} onChange={(v) => pinInput.set(v)} masked placeholder={t("send.pinPlaceholder")} />
      </ModalSheet>
    </Scroll>
  );
}
