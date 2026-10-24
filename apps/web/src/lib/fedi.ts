// Fediverse logins (see the server's auth/fedi.go): after the instance,
// the server sends the browser back to the app with ?fedi=<result> and,
// for errors, a reason code. Only known codes become text: the address
// can be made by anyone.

export type FediResult = { kind: "register" } | { kind: "linked" | "reauth" | "error"; text: string };

const REASONS: Record<string, string> = {
  expired: "The fediverse login expired or was started elsewhere. Please try again.",
  denied: "The fediverse server did not allow the login.",
  unreachable: "Could not check the login with the fediverse server. Please try again.",
  taken: "This fediverse account is linked to another account already.",
  other: "That is not the fediverse account linked to your account.",
  closed: "This server does not take new accounts.",
};
const FAILED = "The fediverse login did not work. Please try again.";

export const fediResult = (search: string): FediResult | null => {
  const q = new URLSearchParams(search);
  switch (q.get("fedi")) {
    case "register":
      return { kind: "register" };
    case "linked":
      return { kind: "linked", text: "Fediverse account linked: you can log in with it now." };
    // A fresh login for the first password (see SettingsDialog).
    case "reauth":
      return { kind: "reauth", text: "Confirmed: set your password now." };
    case "error":
      return { kind: "error", text: REASONS[q.get("reason") ?? ""] ?? FAILED };
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

// A linked fediverse account, as /api/me tells it.
export type FediLink = { handle: string; public: boolean };

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
