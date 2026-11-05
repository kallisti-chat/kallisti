// The desktop app (Tauri) connects through a local bridge in the app, to any
// Mumble server, without the Kallisti server.

import { t } from "./i18n.svelte";

export type BridgeInfo = { port: number; secret: string };
export type ServerAddress = { host: string; port: number };

const DEFAULT_PORT = 64738;

// "host", "host:port", "[v6]:port" or a bare IPv6 address.
export const parseAddress = (input: string): ServerAddress | null => {
  const text = input.trim();
  if (!text) return null;
  const bracketed = /^\[([^\]]+)\](?::(\d+))?$/.exec(text);
  const [host, portText] = bracketed
    ? [bracketed[1]!, bracketed[2]]
    : text.split(":").length === 2
      ? (text.split(":") as [string, string])
      : [text, undefined];
  if (portText !== undefined && !/^\d+$/.test(portText)) return null;
  const port = portText === undefined ? DEFAULT_PORT : Number(portText);
  if (!host || port < 1 || port > 65535) return null;
  return { host, port };
};

export const bridgeUrl = (bridge: BridgeInfo, server: ServerAddress) => {
  const params = new URLSearchParams({ secret: bridge.secret, host: server.host, port: String(server.port) });
  return `ws://127.0.0.1:${bridge.port}/?${params}`;
};

// Running inside the Tauri app?
export const isDesktop = () => typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
// A Linux user agent (WebKitGTK in the desktop app), not Android.
export const linuxAgent = (ua: string) => /Linux/.test(ua) && !/Android/.test(ua);
// The desktop app on Linux (WebKitGTK).
export const linuxApp = () => isDesktop() && linuxAgent(navigator.userAgent);
// An Android user agent (the app's WebView).
export const androidAgent = (ua: string) => /Android/.test(ua);
// The Android app.
export const androidApp = () => isDesktop() && androidAgent(navigator.userAgent);

// The Android app's voice service (a foreground service: voice goes on in
// the background), on while in voice, its notification with the channel.
// Off after a moment: a reconnect replaces the chat view (its cleanup
// stops, the new one starts), and Android starts such a service again only
// from the foreground. One call at a time, in order: the app's commands
// run in parallel, and a stop must not overtake a later start.
export const createVoiceService = (call: (on: boolean, channel: string | null) => Promise<void>, stopDelayMs: number) => {
  let stopping: ReturnType<typeof setTimeout> | undefined;
  let queue = Promise.resolve();
  const send = (on: boolean, channel: string | null) => {
    const sent = queue.then(() => call(on, channel));
    queue = sent.catch(() => {});
    return sent;
  };
  return (on: boolean, channel: string | null): Promise<void> => {
    clearTimeout(stopping);
    if (on) return send(true, channel);
    stopping = setTimeout(() => void send(false, null).catch(() => {}), stopDelayMs);
    return Promise.resolve();
  };
};

export const setVoiceService = createVoiceService(async (on, channel) => {
  const { invoke } = await import("@tauri-apps/api/core");
  await invoke("voice_service", { on, channel });
}, 2000);

// Opens a URL (http or https) in the system browser.
export const openInBrowser = async (url: string) => {
  const { openUrl } = await import("@tauri-apps/plugin-opener");
  await openUrl(url);
};

// The app's window (see lib/windowHidden).
export const currentWindow = async () => (await import("@tauri-apps/api/window")).getCurrentWindow();

export const getBridge = async (): Promise<BridgeInfo> => {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<BridgeInfo>("bridge_info");
};

// The iOS app has no bridge (only Kallisti servers there): asked once.
export const bridgeCheck = (get: () => Promise<BridgeInfo>) => {
  let answer: Promise<boolean> | undefined;
  return () => (answer ??= get().then(
    () => true,
    () => false,
  ));
};
export const hasBridge = bridgeCheck(getBridge);

// The start screen's hint for the server address, and the note for an
// address that is not a Kallisti server in an app without the bridge.
export const serverHint = (bridge: boolean) =>
  t(bridge ? "client.serverHintBridge" : "client.serverHint");
export const notKallisti = (address: string) =>
  t("client.notKallisti", { address });

type ShortcutApi = {
  register: (key: string, handler: (e: { state: "Pressed" | "Released" }) => void) => Promise<void>;
  unregister: (key: string) => Promise<void>;
};

// Push-to-talk while the app is in the background. The key is reserved
// system-wide while registered. Returns a function that unregisters it.
export const registerGlobalPtt = async (
  key: string,
  onChange: (down: boolean) => void,
  api?: ShortcutApi,
): Promise<() => void> => {
  const shortcuts: ShortcutApi = api ?? (await import("@tauri-apps/plugin-global-shortcut"));
  await shortcuts.register(key, (e) => onChange(e.state === "Pressed"));
  return () => {
    onChange(false);
    void shortcuts.unregister(key);
  };
};

// The bridge refuses unknown or changed server certificates (trust on first
// use) with these close reasons.
export type CertificateProblem = { kind: "unknown" | "changed"; fingerprint: string };

export const certificateProblem = (reason: string): CertificateProblem | null => {
  const m = /^certificate-(unknown|changed) (\S+)$/.exec(reason);
  return m ? { kind: m[1] as CertificateProblem["kind"], fingerprint: m[2]! } : null;
};

// "sha256:ab12cd" becomes "AB:12:CD", easier to compare with the server admin.
export const formatFingerprint = (fp: string) =>
  (fp.replace(/^sha256:/, "").toUpperCase().match(/../g) ?? []).join(":");

export const trustServer = async (server: ServerAddress, fingerprint: string) => {
  const { invoke } = await import("@tauri-apps/api/core");
  await invoke("trust_server", { host: server.host, port: server.port, fingerprint });
};

// A Kallisti server: the desktop app works like the web app with it (see
// isKallisti). Tried at the address's HTTPS origin (a Mumble port does not
// matter), or at the origin typed with its scheme (development: http).
export const kallistiOrigin = (input: string): string | null => {
  const text = input.trim();
  if (/^https?:\/\//i.test(text)) {
    try {
      return new URL(text).origin;
    } catch {
      return null;
    }
  }
  const address = parseAddress(text);
  return address ? `https://${address.host.includes(":") ? `[${address.host}]` : address.host}` : null;
};

// Kallisti servers answer /api/me with their JSON, logged in or not.
// "other": no Kallisti there (no web server at all: a Mumble server);
// "unreachable": no answer in time or offline (try again, do not decide).
export const isKallisti = async (origin: string, fetchFn: typeof fetch = fetch, timeoutMs = 4000) => {
  try {
    const res = await fetchFn(`${origin}/api/me`, { credentials: "omit", redirect: "error", signal: AbortSignal.timeout(timeoutMs) });
    const body = (await res.json().catch(() => null)) as { error?: unknown; name?: unknown } | null;
    const kallisti = res.status === 401 ? body?.error === "not logged in" : res.ok && typeof body?.name === "string";
    return kallisti ? "kallisti" : "other";
  } catch (err) {
    const timeout = err instanceof DOMException && (err.name === "TimeoutError" || err.name === "AbortError");
    return timeout || globalThis.navigator?.onLine === false ? "unreachable" : "other";
  }
};

// The server's WebSocket (voice and text), with the session token as a
// subprotocol (browsers cannot send headers there; see the server's auth).
// With voice: through the app's bridge, which sends voice over UDP to the
// server's Mumble port while UDP works (better on bad networks).
export const kallistiSocket = (origin: string, token: string | null, voice?: { bridge: BridgeInfo; udpPort: number }) => {
  const url = `${origin.replace(/^http/, "ws")}/ws`;
  const protocols = token ? ["kallisti", `kallisti.session.${token}`] : [];
  if (!voice) return { url, protocols };
  const params = new URLSearchParams({ secret: voice.bridge.secret, kallisti: url, udp: String(voice.udpPort) });
  // site: the server's own HTTP address (uploads, streams, pictures).
  return { url: `ws://127.0.0.1:${voice.bridge.port}/?${params}`, protocols, site: origin };
};

// The bridge could not reach the server (it has another trust store and
// proxy settings than the web view): connect straight from then on.
export const bridgeFailed = (closeReason = "") => closeReason.startsWith("cannot reach ");
