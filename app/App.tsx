import { batch, cleanup, effect, Image, mount, signal, Text, token, View, windowSize, type ImageAsset } from "nsh";
import { Tab, Tabs } from "nsh/base";
import { Application, isAvailable as isDesktopAvailable, Tray, Window } from "nsh/desktop";
import { t } from "nsh/i18n";
import trayIcon from "./tray.svg";
import receiveNavIcon from "./icons/receive.svg";
import sendNavIcon from "./icons/send.svg";
import historyNavIcon from "./icons/history.svg";
import settingsNavIcon from "./icons/settings.svg";
import { applyHistoryUpdatedEvent, HistoryPage, hydrateHistoryFromBootstrap } from "./src/history";
import {
  applyIncomingOfferEvent,
  applyOfferAbortedEvent,
  applyReceiveEndedEvent,
  applyReceiveProgressEvent,
  applyServerStateEvent,
  applySettingsToReceive,
  hydrateReceiveFromBootstrap,
  IncomingOfferDialog,
  ReceivePage,
} from "./src/receive";
import {
  applyDevicesUpdatedEvent,
  applySendEndedEvent,
  applySendProgressEvent,
  hydrateSendFromBootstrap,
  SendPage,
} from "./src/send";
import { hydrateSettingsFromBootstrap, restoreLocalePreference, SettingsPage } from "./src/settings";
import {
  sidecarError,
  sidecarStatus,
  startSidecarBridge,
  stopSidecarBridge,
  subscribeSidecarEvent,
  type SidecarTransport,
} from "./src/sidecar";
import { ActionButton, EmptyPlaceholder, StatusBadge } from "./src/ui";

type NavTab = "receive" | "send" | "history" | "settings";

const NAV_ITEMS: ReadonlyArray<{ id: NavTab; labelKey: string; icon: ImageAsset }> = [
  { id: "receive", labelKey: "nav.receive", icon: receiveNavIcon },
  { id: "send", labelKey: "nav.send", icon: sendNavIcon },
  { id: "history", labelKey: "nav.history", icon: historyNavIcon },
  { id: "settings", labelKey: "nav.settings", icon: settingsNavIcon },
];

export function NshSendShell(props: { transport?: SidecarTransport }) {
  const activeTab = signal<NavTab>("receive");
  const compact = () => windowSize().width < 640;

  const unsubscribers = [
    subscribeSidecarEvent("server.state", applyServerStateEvent),
    subscribeSidecarEvent("devices.updated", applyDevicesUpdatedEvent),
    subscribeSidecarEvent("receive.incoming", applyIncomingOfferEvent),
    subscribeSidecarEvent("receive.aborted", ({ sessionId }) => applyOfferAbortedEvent(sessionId)),
    subscribeSidecarEvent("receive.progress", applyReceiveProgressEvent),
    subscribeSidecarEvent("receive.ended", applyReceiveEndedEvent),
    subscribeSidecarEvent("send.progress", applySendProgressEvent),
    subscribeSidecarEvent("send.ended", applySendEndedEvent),
    subscribeSidecarEvent("history.updated", applyHistoryUpdatedEvent),
  ];

  const bootCore = () => {
    void startSidecarBridge(props.transport)
      .then((boot) => {
        batch(() => {
          hydrateReceiveFromBootstrap(boot);
          hydrateSendFromBootstrap(boot);
          hydrateHistoryFromBootstrap(boot);
          hydrateSettingsFromBootstrap(boot);
        });
      })
      .catch((err) => {
        console.error("Failed to bootstrap nshsend-core:", err);
      });
  };

  bootCore();

  const stopQuitListener = isDesktopAvailable ? Application.on("quit", stopSidecarBridge) : () => {};
  cleanup(() => {
    for (const unsub of unsubscribers) {
      unsub();
    }
    stopQuitListener();
    stopSidecarBridge();
  });

  const renderTabButton = (item: { id: NavTab; labelKey: string; icon: ImageAsset }) => (
    <Tab
      testID={`nav-${item.id}`}
      selected={activeTab() === item.id}
      onPress={() => activeTab.set(item.id)}
      class={`group rounded-(--radius) px-3 ${compact() ? "py-2" : "py-3"}`}
    >
      <View class="flex-row items-center gap-3">
        <Image
          src={item.icon}
          class="size-4 shrink-0"
          style={{ tintColor: activeTab() === item.id ? token("foreground") : token("muted-foreground") }}
        />
        <Text class="text-sm font-medium text-muted-foreground group-selected:text-foreground">
          {t(item.labelKey)}
        </Text>
      </View>
    </Tab>
  );

  return (
    <View class={`flex-1 bg-background ${compact() ? "flex-col" : "flex-row"}`}>
      <View
        class={`border-border bg-card p-4 gap-4 ${
          compact() ? "border-b flex-row items-center justify-between" : "w-52 border-r justify-between"
        }`}
      >
        <View class={compact() ? "flex-row items-center gap-3 flex-1" : "gap-6"}>
          <View class="px-2 py-1 gap-0.5">
            <Text class="text-base font-semibold text-foreground">NshSend</Text>
            <If when={!compact()}>
              <StatusBadge
                label={
                  sidecarStatus() === "ready"
                    ? t("app.coreOnline")
                    : sidecarStatus() === "starting"
                      ? t("app.coreStarting")
                      : t("app.coreError")
                }
                tone={
                  sidecarStatus() === "ready"
                    ? "success"
                    : sidecarStatus() === "starting"
                      ? "neutral"
                      : "destructive"
                }
              />
            </If>
          </View>

          <Tabs
            orientation={compact() ? "horizontal" : "vertical"}
            class={compact() ? "flex-row gap-1 rounded-(--radius) bg-secondary p-1" : "gap-2"}
            indicatorClass="inset-0 rounded-(--radius) bg-accent"
          >
            {NAV_ITEMS.map(renderTabButton)}
          </Tabs>
        </View>
      </View>

      <View class="flex-1">
        <If when={sidecarStatus() === "error"}>
          <View class="flex-1 items-center justify-center p-6">
            <EmptyPlaceholder
              title={t("app.coreUnavailableTitle")}
              description={sidecarError() ?? t("app.coreUnavailableHint")}
              action={<ActionButton label={t("app.retryConnection")} variant="primary" onPress={bootCore} />}
            />
          </View>
        </If>

        <If when={sidecarStatus() !== "error"}>
          <If when={activeTab() === "receive"}>
            <ReceivePage />
          </If>
          <If when={activeTab() === "send"}>
            <SendPage />
          </If>
          <If when={activeTab() === "history"}>
            <HistoryPage />
          </If>
          <If when={activeTab() === "settings"}>
            <SettingsPage onSettingsSaved={applySettingsToReceive} />
          </If>
        </If>
      </View>

      <IncomingOfferDialog />
    </View>
  );
}

function NshSendApp() {
  const windowShown = signal(true);

  const showMainWindow = () => {
    windowShown.set(true);
    Window.get("main")?.focus();
  };

  void restoreLocalePreference();

  if (isDesktopAvailable) {
    Application.setQuitMode("explicit");
    effect(() => {
      Tray.set({
        icon: trayIcon,
        tooltip: "NshSend",
        onClick: showMainWindow,
        menu: [
          { label: t("tray.showWindow"), onSelect: showMainWindow },
          { type: "separator" },
          { role: "quit" },
        ],
      });
    });
    cleanup(() => Tray.set(null));
  }

  return (
    <Window
      id="main"
      title="NshSend"
      open={windowShown()}
      closeOnPress={false}
      onClosePress={() => windowShown.set(false)}
    >
      <NshSendShell />
    </Window>
  );
}

mount(NshSendApp);
