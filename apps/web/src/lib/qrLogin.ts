import { encode } from "uqr";
import type { QrState } from "./accounts";
import { isDesktop } from "./desktop";
import { t } from "./i18n.svelte";

// Log in with a QR code (see the server's auth/qr.go): the code is a link
// <server>/qr/<token>; a phone's camera opens it in the browser, the
// apps scan it themselves. The devices are named for the question "Log in
// on <device>?".

export const qrLink = (origin: string, token: string) => `${origin}/qr/${token}`;

// A code's server and token; null: not a QR login link (http only on this
// computer, as for invite links).
export const parseQrLink = (url: string): { origin: string; token: string } | null => {
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return null;
  }
  const m = /^\/qr\/([0-9a-f]{64})$/.exec(u.pathname);
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(u.hostname);
  return m && (u.protocol === "https:" || (u.protocol === "http:" && local)) ? { origin: u.origin, token: m[1]! } : null;
};

// "Firefox on Linux", "Kallisti app on iPhone": from the user agent
// (touch: its touch points; iPadOS says "Macintosh").
export const deviceLabel = (ua: string, app: boolean, touch = 0): string => {
  const system = /iPhone/.test(ua)
    ? "iPhone"
    : /iPad/.test(ua) || (/Macintosh/.test(ua) && touch > 1)
      ? "iPad"
      : /Android/.test(ua)
        ? "Android"
        : /Windows/.test(ua)
          ? "Windows"
          : /Mac OS X|Macintosh/.test(ua)
            ? "macOS"
            : /Linux|X11/.test(ua)
              ? "Linux"
              : "";
  const browser = app
    ? t("login.kallistiApp")
    : /Edg\//.test(ua)
      ? "Edge"
      : /Firefox\//.test(ua)
        ? "Firefox"
        : /Chrome\//.test(ua)
          ? "Chrome"
          : /Safari\//.test(ua)
            ? "Safari"
            : t("login.someBrowser");
  return system ? t("login.deviceOn", { browser, system }) : browser;
};

// A code as an SVG path of its dark cells (a light border of 4 cells, as
// the standard asks), size cells a side.
export const qrPath = (text: string): { size: number; path: string } => {
  const { data, size } = encode(text, { border: 4 });
  const cells: string[] = [];
  data.forEach((row, y) => row.forEach((dark, x) => dark && cells.push(`M${x} ${y}h1v1h-1z`)));
  return { size, path: cells.join("") };
};

// This device, as the other one sees it.
export const thisDevice = () => deviceLabel(navigator.userAgent, isDesktop(), navigator.maxTouchPoints);

// Asks a code's state every ms until onState says to stop (false); a
// failed ask is asked again (a lost request), until the deadline (Unix ms):
// then it is expired. Returns the way out.
export const pollState = (wait: () => Promise<QrState>, onState: (s: QrState) => boolean, ms = 1500, until = Date.now() + 150_000): (() => void) => {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const tick = () => {
    timer = setTimeout(async () => {
      const left = until - Date.now();
      if (left <= 0) return void (stopped || onState({ state: "expired" }));
      let st: QrState;
      try {
        // A request that hangs ends at the deadline too.
        st = await Promise.race([wait(), new Promise<QrState>((done) => setTimeout(() => done({ state: "expired" }), left))]);
      } catch {
        return stopped || tick();
      }
      if (!stopped && onState(st) && Date.now() < until) tick();
    }, ms);
  };
  tick();
  return () => {
    stopped = true;
    clearTimeout(timer);
  };
};
