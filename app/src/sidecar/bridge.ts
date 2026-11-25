import os from "os";
import process from "process";
import { host, signal } from "nsh";
import { t } from "nsh/i18n";
import type {
  BootstrapResult,
  SidecarEventMap,
  SidecarEventName,
  SidecarRequestMap,
} from "./protocol";

export type SidecarConnectionStatus = "starting" | "ready" | "error";

export interface SidecarTransport {
  start(
    onLine: (line: string) => void,
    onExit: (info: { code: number | null; signal: string | null }) => void,
  ): Promise<void>;
  writeLine(line: string): void;
  stop(): void;
}

interface PendingCall {
  resolve: (value: unknown) => void;
  reject: (reason: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

const connectionStatusSignal = signal<SidecarConnectionStatus>("starting");
const connectionErrorSignal = signal<string | null>(null);

export const sidecarStatus = () => connectionStatusSignal();
export const sidecarError = () => connectionErrorSignal();

const eventListeners = new Map<SidecarEventName, Set<(data: unknown) => void>>();
const pendingCalls = new Map<string, PendingCall>();
let activeTransport: SidecarTransport | null = null;
let nextRequestSeq = 1;

function resolveCoreExecutable(): string {
  return os.platform() === "windows" ? "nshsend-core.exe" : "nshsend-core";
}

function createProcessTransport(): SidecarTransport {
  let child: ReturnType<typeof process.spawn> | null = null;

  return {
    async start(onLine, onExit) {
      const cmd = resolveCoreExecutable();
      const args: string[] = [];
      if (process.dataDir) {
        args.push("--data-dir", process.dataDir);
      }

      child = process.spawn(cmd, args, {
        stdin: "pipe",
        lines: true,
      });
      child.on("stdout", onLine);
      child.on("stderr", (line) => {
        console.warn("[nshsend-core]", line);
      });
      child.on("exit", ({ code, signal: exitSignal }) => {
        onExit({ code: code ?? null, signal: exitSignal ?? null });
      });
    },
    writeLine(line: string) {
      child?.write(`${line}\n`);
    },
    stop() {
      child?.kill();
      child = null;
    },
  };
}

function createHostTransport(): SidecarTransport {
  let stopEvent: (() => void) | null = null;
  let lineCallback: ((line: string) => void) | null = null;

  return {
    async start(onLine) {
      lineCallback = onLine;
      stopEvent = host.on("sidecar.event", (payload) => {
        lineCallback?.(JSON.stringify(payload));
      });
    },
    writeLine(line: string) {
      let parsed: { id: string; method: string; params?: unknown };
      try {
        parsed = JSON.parse(line);
      } catch (err) {
        console.warn("Invalid request line in host transport:", err);
        return;
      }
      void host
        .call("sidecar.call", { method: parsed.method, params: parsed.params })
        .then((result) => {
          lineCallback?.(JSON.stringify({ id: parsed.id, result }));
        })
        .catch((err) => {
          const message = err instanceof Error ? err.message : String(err);
          lineCallback?.(JSON.stringify({ id: parsed.id, error: { code: "HOST_ERROR", message } }));
        });
    },
    stop() {
      stopEvent?.();
      stopEvent = null;
      lineCallback = null;
    },
  };
}

function dispatchSidecarLine(raw: string): void {
  const trimmed = raw.trim();
  if (!trimmed) return;

  let parsed: {
    id?: string;
    result?: unknown;
    error?: { code?: string; message?: string };
    event?: SidecarEventName;
    data?: unknown;
  };
  try {
    parsed = JSON.parse(trimmed);
  } catch (err) {
    console.warn("Invalid NDJSON from sidecar:", err);
    return;
  }

  if (parsed.id) {
    const pending = pendingCalls.get(parsed.id);
    if (!pending) return;
    pendingCalls.delete(parsed.id);
    clearTimeout(pending.timer);
    if (parsed.error) {
      pending.reject(new Error(parsed.error.message ?? parsed.error.code ?? "Sidecar error"));
    } else {
      pending.resolve(parsed.result);
    }
    return;
  }

  if (parsed.event) {
    const listeners = eventListeners.get(parsed.event);
    if (!listeners) return;
    for (const listener of listeners) {
      listener(parsed.data);
    }
  }
}

/** Starts the Go sidecar daemon (or injected test transport) and runs core.bootstrap. */
export async function startSidecarBridge(customTransport?: SidecarTransport): Promise<BootstrapResult> {
  if (activeTransport) {
    activeTransport.stop();
    activeTransport = null;
  }

  connectionStatusSignal.set("starting");
  connectionErrorSignal.set(null);

  const transport =
    customTransport ??
    (host.available() && host.params.mockSidecar === true ? createHostTransport() : createProcessTransport());
  activeTransport = transport;

  try {
    await transport.start(dispatchSidecarLine, ({ code, signal: exitSignal }) => {
      if (activeTransport !== transport) return;
      const reason = t("app.coreExited", { code: code ?? "null", signal: exitSignal ?? "none" });
      connectionStatusSignal.set("error");
      connectionErrorSignal.set(reason);
      for (const [id, pending] of pendingCalls) {
        clearTimeout(pending.timer);
        pending.reject(new Error(reason));
        pendingCalls.delete(id);
      }
    });

    const bootstrap = await callSidecar("core.bootstrap", undefined);
    connectionStatusSignal.set("ready");
    return bootstrap;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    connectionStatusSignal.set("error");
    connectionErrorSignal.set(msg);
    throw err;
  }
}

/** Stops the running sidecar process and clears pending requests. */
export function stopSidecarBridge(): void {
  if (activeTransport) {
    const t = activeTransport;
    activeTransport = null;
    t.stop();
  }
  for (const [id, pending] of pendingCalls) {
    clearTimeout(pending.timer);
    pending.reject(new Error("Sidecar stopped"));
    pendingCalls.delete(id);
  }
}

/** Sends a typed request to the Go sidecar and awaits its correlated response. */
export function callSidecar<M extends keyof SidecarRequestMap>(
  method: M,
  params: SidecarRequestMap[M]["params"],
  timeoutMs = 30_000,
): Promise<SidecarRequestMap[M]["result"]> {
  if (!activeTransport) {
    return Promise.reject(new Error("Sidecar is not running"));
  }

  const id = String(nextRequestSeq++);
  const frame: Record<string, unknown> = { id, method };
  if (params !== undefined) {
    frame.params = params;
  }

  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pendingCalls.delete(id);
      reject(new Error(`Sidecar request timed out: ${String(method)}`));
    }, timeoutMs);

    pendingCalls.set(id, {
      resolve: resolve as (v: unknown) => void,
      reject,
      timer,
    });

    try {
      activeTransport?.writeLine(JSON.stringify(frame));
    } catch (err) {
      clearTimeout(timer);
      pendingCalls.delete(id);
      reject(err instanceof Error ? err : new Error(String(err)));
    }
  });
}

/** Subscribes to a push event emitted by the Go sidecar. Returns an unsubscribe function. */
export function subscribeSidecarEvent<E extends SidecarEventName>(
  event: E,
  listener: (data: SidecarEventMap[E]) => void,
): () => void {
  let set = eventListeners.get(event);
  if (!set) {
    set = new Set();
    eventListeners.set(event, set);
  }
  const wrapped = listener as (data: unknown) => void;
  set.add(wrapped);
  return () => {
    set?.delete(wrapped);
  };
}
