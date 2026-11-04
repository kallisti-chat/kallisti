// The Kallisti server's account API. The session is an HttpOnly cookie, so
// every call includes credentials (the API may be on another origin in
// development). The desktop app (session given) cannot use the cookie (it
// is third-party there): it keeps the server's token and sends it.
import type { Challenge, Proof } from "./captcha";
import type { FediLink } from "./fedi";
import { type SavedServer, savedServers } from "./serverList";
import { parseFriends, type Friend } from "./friends";
import { t } from "./i18n.svelte";

// A logged-in account; temporary: a guest account without a password (the
// server deletes it after a day without a visit). password false: made
// with a fediverse login, no password yet (undefined: not known). fedi:
// the linked fediverse account.
// session: a short name of this login (see the server's sessionID).
export type Account = { name: string; temporary: boolean; password?: boolean; fedi?: FediLink | null; session?: string };

// An answer the server refused, with its status.
class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message);
  }
}

// QR login answers: a new code (offer: this logged-in device shows it;
// request: this logged-out one), a scan (offer: wait with the claim;
// request: confirm), a state.
// (code: not "token", which the API keeps as the session.)
export type QrStart = { kind: "offer" | "request"; code: string; owner: string; expires: number };
export type QrScan = { kind: "offer"; claim: string; device: string } | { kind: "request"; device: string };
export type QrState = { state: "waiting" | "scanned" | "approved" | "refused" | "expired" | "done"; device?: string; name?: string };

export type SessionStore = { get: () => string | null; set: (token: string | null) => void };

// A token kept in memory only.
export const memoryStore = (token: string | null = null): SessionStore => ({ get: () => token, set: (next) => void (token = next) });

export const createAccountsApi = (base: string, fetchFn: typeof fetch = fetch, session?: SessionStore) => {
  type Answer = Partial<QrStart> & { state?: string; claim?: string; device?: string } & { name?: string; mumble?: boolean; temporary?: boolean; token?: string } & Partial<Challenge> &
    Partial<StreamKey> & { key?: string; url?: string; handle?: string; password?: boolean; fedi?: FediLink; session?: string };
  const clearIf = (token: string | null | undefined) => {
    if (session && session.get() === token) session.set(null);
  };
  const call = async (path: string, body?: unknown, method = body === undefined ? "GET" : "POST"): Promise<Answer> => {
    const token = session?.get();
    const headers: Record<string, string> = {
      ...(body !== undefined && { "Content-Type": "application/json" }),
      ...(session && { "X-Kallisti-Session": "token" }),
      ...(token && { Authorization: `Bearer ${token}` }),
    };
    // A web GET without Content-Type: no CORS preflight needed.
    const res = await fetchFn(`${base}${path}`, {
      credentials: session ? "omit" : "include",
      method,
      ...(Object.keys(headers).length && { headers }),
      ...(body !== undefined && { body: JSON.stringify(body) }),
    });
    const data = (await res.json().catch(() => ({}))) as Answer & { error?: string; code?: string };
    // The server no longer knows the token (not: a wrong password); only
    // if it is still the one sent (a new login may have come meanwhile).
    if (res.status === 401 && path === "/api/me") clearIf(token);
    if (!res.ok) throw new ApiError(data.error ?? t("login.requestFailed", { status: res.status }), res.status, data.code);
    if (data.token) session?.set(data.token);
    return data;
  };
  const check = async (): Promise<Account | null | "unknown"> => {
    try {
      const data = await call("/api/me");
      return data.name ? { name: data.name, temporary: data.temporary === true, password: data.password, fedi: data.fedi, session: data.session } : null;
    } catch (err) {
      return err instanceof ApiError && err.status === 401 ? null : "unknown";
    }
  };
  return {
    // The server's address (for pictures from it: see lib/serverLogo).
    base,
    // Log in with a QR code (see lib/qrLogin and the server's auth/qr.go).
    qrStart: (device: string) => call("/api/qr/start", { device }) as Promise<QrStart>,
    qrScan: (token: string, device: string) => call("/api/qr/scan", { token, device }) as Promise<QrScan>,
    qrAnswer: async (secret: string, yes: boolean) => void (await call("/api/qr/answer", { secret, yes })),
    // done: this device is logged in now (the cookie, or the token kept).
    qrWait: (secret: string) => call("/api/qr/wait", { secret }) as Promise<QrState>,
    // mumble: a Mumble registration (built-in server), not a web account. It
    // has no session: connect with the name and password.
    login: async (name: string, password: string) => {
      const data = await call("/api/login", { name, password });
      return { name: data.name ?? name, mumble: data.mumble === true };
    },
    // A challenge to solve before create and guest (see captcha), or null:
    // none needed (or a server without them).
    challenge: async (): Promise<Challenge | null> => {
      const c = await call("/api/challenge").catch(() => null);
      return c?.challenge && c.bits ? { challenge: c.challenge, bits: c.bits } : null;
    },
    create: async (name: string, password: string, proof?: Proof, invite?: string) =>
      (await call("/api/accounts", { name, password, ...proof, ...(invite && { invite }) })).name ?? name,
    // A temporary account for a guest; null: join as a classic guest (the
    // server has no guest accounts, too many right now, or no answer).
    // Name problems and a failed browser check (403) are errors.
    guest: async (name: string, proof?: Proof): Promise<Account | null> => {
      try {
        return { name: (await call("/api/guest", { name: name.trim(), ...proof })).name ?? name.trim(), temporary: true };
      } catch (err) {
        if (err instanceof ApiError && [400, 403, 409].includes(err.status)) throw err;
        return null;
      }
    },
    // false: the server did not confirm (the session cookie may remain).
    // The token goes only if it is still the one logged out. expected: the
    // login meant (its fingerprint, and the account's name: a guest has no
    // fingerprint); "other": another login is active here now (the server
    // refused to end it).
    logout: async (expected: { session?: string; name?: string } = {}): Promise<boolean | "other"> => {
      const token = session?.get();
      const body = { ...(expected.session && { session: expected.session }), ...(expected.name && { name: expected.name }) };
      return call("/api/logout", body).then(
        () => (clearIf(token), true),
        (err) => (err instanceof ApiError && err.status === 409 ? "other" : false),
      );
    },
    logoutAll: async () => {
      const token = session?.get();
      await call("/api/logout-all", {});
      clearIf(token);
    },
    changePassword: async (old: string, next: string) => {
      await call("/api/password", { old, new: next });
    },
    whipUrl: whipUrl(base),
    // Streaming from OBS: off on the server (or for guest accounts), or on
    // with or without a key.
    streamKey: async (): Promise<StreamKey> => {
      const data = await call("/api/stream-key").catch(() => ({}) as Answer);
      return { enabled: data.enabled === true, exists: data.exists === true };
    },
    // The home account's list of the servers it joined (the rail; not for
    // guests). account: the one meant (another login here now: refused).
    servers: async (account: string) =>
      savedServers(((await call(`/api/servers?account=${encodeURIComponent(account)}`)) as { servers?: unknown }).servers),
    addServer: async (server: SavedServer, account: string) => void (await call("/api/servers", { ...server, account })),
    removeServer: async (origin: string, account: string) => void (await call("/api/servers", { origin, account }, "DELETE")),
    // The home account's friends (see lib/friends; not for guests).
    friends: async (account: string) =>
      parseFriends(((await call(`/api/friends?account=${encodeURIComponent(account)}`)) as { friends?: unknown }).friends),
    addFriend: async (f: Friend, account: string) => void (await call("/api/friends", { ...f, account })),
    removeFriend: async (origin: string, user: number, account: string) => void (await call("/api/friends", { origin, user, account }, "DELETE")),
    // Another server asks who you are (Kallisti as a login provider): the
    // question for this authorize query, and the answer (where to go).
    oauthRequest: async (query: string) =>
      (await call(`/api/oauth/request?${query}`)) as { site: string; account: string; deny: string; ticket: string },
    // The answer is for exactly the request shown (its ticket).
    oauthApprove: async (ticket: string) => ((await call("/api/oauth/approve", { ticket })) as { redirect: string }).redirect,
    // The sites this account allowed to log in with it (only on its own
    // site); one taken back asks again next time.
    allowedSites: async () => ((await call("/api/oauth/grants")) as { sites?: AllowedSite[] }).sites ?? [],
    revokeSite: async (redirect: string) => void (await call("/api/oauth/grants", { redirect }, "DELETE")),
    // A new key (the old one stops working): shown this once.
    newStreamKey: async () => (await call("/api/stream-key", {})).key ?? "",
    removeStreamKey: async () => {
      await call("/api/stream-key", undefined, "DELETE");
    },
    // Whether guests may need the server password, the server's name and
    // its logo's version (null: not known, with Murmur or an older server;
    // no logo).
    serverInfo: async () => {
      const v = (await call("/api/server").catch(() => ({}))) as { password?: boolean; name?: unknown; logo?: unknown; inviteOnly?: boolean; udpPort?: unknown };
      const text = (x: unknown) => (typeof x === "string" && x ? x : null);
      // udpPort: the Mumble port, for voice over UDP in the apps.
      const port = typeof v.udpPort === "number" && Number.isInteger(v.udpPort) && v.udpPort > 0 && v.udpPort < 65536 ? v.udpPort : null;
      return {
        password: v.password !== false,
        name: text(v.name),
        logo: text(v.logo),
        ...(typeof v.inviteOnly === "boolean" && { inviteOnly: v.inviteOnly }),
        ...(port !== null && { udpPort: port }),
      };
    },
    // Fediverse logins (see lib/fedi): on for this server?
    fediEnabled: async () => (await call("/api/fedi").catch(() => ({}) as Answer & { enabled?: boolean })).enabled === true,
    // The instance's page to go to, for a login (or a link: with the
    // account's password, "" for one without). handoff: for the popup (see
    // Connect), to: its page's origin (none: the instance's).
    fediStart: async (instance: string, linkPassword?: string, handoff?: { to?: string }) => {
      const { url } = await call("/api/fedi/start", {
        instance,
        ...(linkPassword !== undefined && { link: true, password: linkPassword }),
        ...(handoff && { handoff: true, ...(handoff.to && { to: handoff.to }) }),
      });
      if (!url) throw new Error(t("login.noAnswer"));
      return url;
    },
    // After a login with an unlinked fediverse account: the suggested name
    // and the handle; null: none (expired).
    // remote: the name the account gets (name@home), unless it is taken.
    // host: the server it logs in with.
    fediSignup: async (): Promise<{ name: string; handle: string; host: string; remote: string } | null> => {
      const data = (await call("/api/fedi/signup").catch(() => null)) as (Answer & { remote?: string; host?: string }) | null;
      return data ? { name: data.name ?? "", handle: data.handle ?? "", host: data.host ?? "", remote: data.remote ?? "" } : null;
    },
    // A fresh login with the linked account, for the first password.
    fediReauth: async (instance: string) => {
      const { url } = await call("/api/fedi/start", { instance, purpose: "reauth" });
      if (!url) throw new Error(t("login.noAnswer"));
      return url;
    },
    // handoff: its token waits for the popup (see Connect), no session here.
    fediCreate: async (name: string, proof?: Proof, invite?: string) => {
      const data = (await call("/api/fedi/signup", { name, ...proof, ...(invite && { invite }) })) as Answer & { handoff?: boolean };
      return { name: data.name ?? name, handoff: data.handoff === true };
    },
    // A handoff's token, once, for its origin (see Connect); or first its
    // question (ask: its key), then the token with allow: that key.
    fediHandoff: async (allow?: string) =>
      (await call("/api/fedi/handoff", allow ? { allow } : {})) as { name: string; origin: string } & ({ ask: string } | { token: string }),
    fediUnlink: async () => {
      await call("/api/fedi/unlink", {});
    },
    fediPublic: async (on: boolean) => {
      await call("/api/fedi/public", { on });
    },
    // The logged-in account, or null (also for servers without accounts).
    me: async (): Promise<Account | null> => {
      const result = await check();
      return result === "unknown" ? null : result;
    },
    // The same, telling "no login" (null) from "unknown" (the server or the
    // network failed): only a sure answer may end what goes with a login.
    check,
  };
};

export type AccountsApi = ReturnType<typeof createAccountsApi>;

// The server wants a fresh fediverse login before the first password.
export const needsReauth = (err: unknown) => err instanceof ApiError && err.code === "reauth";
// The server is invite-only (also when its policy was not known here).
export const inviteRequired = (err: unknown) => err instanceof ApiError && err.code === "invite_required";
// An API error's HTTP status (undefined: not one).
export const apiStatus = (err: unknown) => (err instanceof ApiError ? err.status : undefined);

export type StreamKey = { enabled: boolean; exists: boolean };

// Where OBS sends a stream (WHIP), on the Kallisti server.
export const whipUrl = (base: string) => `${base}/api/whip`;

// The login screen opens on "Log in", and also when a guest tried an
// account's name; on "Guest" after other errors (a guest's name in use).
export const modeForError = (error: string | null): "guest" | "login" =>
  !error || error.includes("belongs to an account") ? "login" : "guest";

// What a logged-in user can do from the settings (temporary: a guest
// account, which gets its first password without an old one).
// A site the account allowed (Kallisti as a login provider): its origin,
// the redirect URI it was allowed for, since when (RFC 3339).
export type AllowedSite = { site: string; redirect: string; since: string };

export type AccountActions = {
  temporary: boolean;
  logout: () => Promise<void>;
  logoutAll: () => Promise<void>;
  changePassword: (old: string, next: string) => Promise<void>;
  // Streaming from OBS (see the server's whip package).
  whipUrl: string;
  streamKey: () => Promise<StreamKey>;
  newStreamKey: () => Promise<string>;
  removeStreamKey: () => Promise<void>;
  // false: an account made with a fediverse login, without a password yet
  // (it sets its first one without an old one).
  hasPassword: boolean;
  // The linked fediverse account (see lib/fedi). fediLink goes to the
  // instance (with the account's password; "" without one).
  fedi: FediLink | null;
  fediEnabled: () => Promise<boolean>;
  fediLink: (instance: string, password: string) => Promise<void>;
  fediUnlink: () => Promise<void>;
  fediPublic: (on: boolean) => Promise<void>;
  // A fresh login with the linked account (goes to the instance, and back
  // to the settings), needed for the first password.
  fediReauth: () => Promise<void>;
  // The sites allowed to log in with this account (undefined: not here,
  // only on its home server's own site).
  sites?: { list: () => Promise<AllowedSite[]>; revoke: (redirect: string) => Promise<void> };
  // Log in on another device with a QR code (see lib/qrLogin): the API,
  // the server's address (in the code), this device's name.
  qr?: QrLogin;
};

export type QrLogin = {
  api: Pick<ReturnType<typeof createAccountsApi>, "qrStart" | "qrScan" | "qrAnswer" | "qrWait">;
  origin: string;
  device: string;
  // The phone apps: scan a code with the camera (see lib/qrScan).
  scan?: () => void;
};

// The API of another server, with this login's own token (not the cookie).
export const apiWithToken = (origin: string, token: string | null, fetchFn: typeof fetch = fetch) =>
  createAccountsApi(origin, fetchFn, memoryStore(token));
