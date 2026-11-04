// Fediverse logins (see the server's auth/fedi.go): after the instance,
// the server sends the browser back to the app with ?fedi=<result> and,
// for errors, a reason code. Only known codes become text: the address
// can be made by anyone.

import type { Key } from "../i18n/en";
import { t } from "./i18n.svelte";

// connect: a handoff's token waits (see Connect).
export type FediResult = { kind: "register" | "connect" } | { kind: "linked" | "reauth" | "error"; text: string };

const REASONS: Record<string, Key> = {
  expired: "login.fediExpiredElsewhere",
  denied: "login.fediDenied",
  unreachable: "login.fediUnreachable",
  taken: "login.fediTaken",
  other: "login.fediOther",
  blocked: "login.fediBlocked",
};

export const fediResult = (search: string): FediResult | null => {
  const q = new URLSearchParams(search);
  switch (q.get("fedi")) {
    case "register":
      return { kind: "register" };
    case "connect":
      return { kind: "connect" };
    case "linked":
      return { kind: "linked", text: t("login.fediLinked") };
    // A fresh login for the first password (see SettingsDialog).
    case "reauth":
      return { kind: "reauth", text: t("login.fediReauthDone") };
    case "error": {
      const reason = q.get("reason") ?? "";
      return { kind: "error", text: t(Object.hasOwn(REASONS, reason) ? REASONS[reason]! : "login.fediFailed") };
    }
    default:
      return null;
  }
};

// The address without the result (for history.replaceState): a reload
// must not show it again.
export const withoutFediResult = (pathAndQuery: string): string => {
  const url = new URL(pathAndQuery, "http://x");
  url.searchParams.delete("fedi");
  url.searchParams.delete("reason");
  return url.pathname + url.search + url.hash;
};

// The server of a handle (@user@host).
export const handleHost = (handle: string) => handle.slice(handle.lastIndexOf("@") + 1);

// A linked fediverse account, as /api/me tells it (host: the server it
// logs in with; none from older servers).
export type FediLink = { handle: string; public: boolean; host?: string };

// A user's verified fediverse handle, if they show it ("": none, or no
// answer). Public: no session needed.
export const fetchFediHandle = async (base: string, name: string, fetchFn: typeof fetch = fetch): Promise<string> => {
  try {
    const res = await fetchFn(`${base}/api/fedi/handle?name=${encodeURIComponent(name)}`, { credentials: "omit" });
    const data = (await res.json()) as { handle?: unknown };
    return res.ok && typeof data.handle === "string" ? data.handle : "";
  } catch {
    return "";
  }
};

// The server to confirm with: the linked handle's, or what the user typed
// for it before (with its scheme, for local test instances).
export const reauthInstance = (handle: string, saved: string | null): string => {
  const host = handleHost(handle);
  const typed = saved?.trim().replace(/^[a-z]+:\/\//i, "").replace(/\/.*$/, "").toLowerCase();
  return saved && typed === host ? saved : host;
};
