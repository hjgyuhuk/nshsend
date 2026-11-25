import { expect, open, test } from "nsh/test";

test("bootstraps receive view, switches tabs, and accepts incoming offer", async () => {
  let quickSave = "off";
  const respondedOffers: Array<{ sessionId: string; accept: boolean }> = [];

  const app = await open("./App.tsx", {
    width: 980,
    height: 680,
    platform: "linux",
    locale: "zh-CN",
    host: {
      params: { mockSidecar: true },
      handlers: {
        "sidecar.call": (raw: unknown) => {
          const { method, params } = raw as { method: string; params?: Record<string, unknown> };
          if (method === "core.bootstrap") {
            return {
              fingerprint: "ABCDEF1234567890",
              settings: {
                alias: "TestHost",
                port: 53317,
                https: true,
                saveDir: "/tmp/downloads",
                quickSave,
                pin: "",
                multicastGroup: "224.0.0.167",
                multicastPort: 53317,
                verifyChecksums: true,
                deviceModel: "Linux",
              },
              server: {
                running: true,
                port: 53317,
                https: true,
                localAddresses: ["192.168.1.50"],
              },
              devices: [
                {
                  fingerprint: "PEER-FP-1",
                  alias: "Pixel 9",
                  version: "2.2",
                  deviceModel: "Pixel",
                  deviceType: "mobile",
                  host: "192.168.1.88",
                  port: 53317,
                  protocol: "https",
                  verified: true,
                  download: false,
                  lastSeenMs: 1700000000000,
                },
              ],
              history: [],
            };
          }
          if (method === "settings.update") {
            if (typeof params?.quickSave === "string") {
              quickSave = params.quickSave;
            }
            return {
              alias: (params?.alias as string) ?? "TestHost",
              port: (params?.port as number) ?? 53317,
              https: true,
              saveDir: "/tmp/downloads",
              quickSave,
              pin: "",
              multicastGroup: "224.0.0.167",
              multicastPort: 53317,
              verifyChecksums: true,
              deviceModel: "Linux",
            };
          }
          if (method === "receive.respond") {
            respondedOffers.push({
              sessionId: String(params?.sessionId ?? ""),
              accept: Boolean(params?.accept),
            });
            return { ok: true };
          }
          if (method === "logs.tail") {
            return {
              logFilePath: "/tmp/nshsend-test/core.log",
              lines: ["[send] prepare-upload -> HTTP 200 (12ms)"],
            };
          }
          return { ok: true };
        },
      },
    },
  });

  await app.waitFor("text:TestHost");
  expect(app.texts()).toContain("监听中 :53317");
  expect(app.texts()).toContain("192.168.1.50:53317");

  await app.press("id:quick-save-switch");
  expect(quickSave).toBe("on");

  await app.press("id:nav-send");
  await app.waitFor("text:Pixel 9");
  await app.press("id:add-text-btn");
  await app.type("id:text-draft-input", "Hello NshSend");
  await app.press("id:confirm-add-text-btn");
  await app.waitFor("text:文本: Hello NshSend");

  await app.host.send("sidecar.event", {
    event: "receive.incoming",
    data: {
      sessionId: "sess-1",
      senderAlias: "MacBook Pro",
      senderIp: "192.168.1.90",
      senderFingerprint: "FP-MAC",
      senderVerified: true,
      files: {
        f1: {
          id: "f1",
          fileName: "report.pdf",
          size: 2048,
          fileType: "application/pdf",
        },
      },
    },
  });
  await app.waitFor("text:来自 MacBook Pro 的传输请求");
  expect(app.texts()).toContain("report.pdf");
  await app.press("id:accept-offer-btn");
  expect(respondedOffers).toEqual([{ sessionId: "sess-1", accept: true }]);

  await app.press("id:nav-history");
  await app.waitFor("text:暂无接收记录");

  await app.press("id:nav-settings");
  await app.waitFor("text:保存并应用");
  await app.press("id:refresh-logs-btn");
  await app.waitFor("text:[send] prepare-upload -> HTTP 200 (12ms)");
});

test("language picker switches the interface to English and back", async () => {
  const app = await open("./App.tsx", {
    width: 980,
    height: 680,
    platform: "linux",
    locale: "zh-CN",
    host: {
      params: { mockSidecar: true },
      handlers: {
        "sidecar.call": (raw: unknown) => {
          const { method } = raw as { method: string };
          if (method === "core.bootstrap") {
            return {
              fingerprint: "ABCDEF1234567890",
              settings: {
                alias: "TestHost",
                port: 53317,
                https: true,
                saveDir: "/tmp/downloads",
                quickSave: "off",
                pin: "",
                multicastGroup: "224.0.0.167",
                multicastPort: 53317,
                verifyChecksums: true,
                deviceModel: "Linux",
              },
              server: { running: true, port: 53317, https: true, localAddresses: ["192.168.1.50"] },
              devices: [],
              history: [],
            };
          }
          return { ok: true };
        },
      },
    },
  });

  await app.press("id:nav-settings");
  await app.waitFor("text:保存并应用");

  await app.press("id:locale-en");
  await app.waitFor("text:Save and apply");
  expect(app.texts()).toContain("Language");

  await app.press("id:locale-zh-CN");
  await app.waitFor("text:保存并应用");
});
