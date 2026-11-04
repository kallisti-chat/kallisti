import { t } from "./i18n.svelte";
import { safeGet, safeSet } from "./storage";

// Other Kallisti servers in the rail (several servers in one client): the
// home account keeps the list (origin, and the name logged in there; see
// lib/accounts servers), this device the tokens of the ones logged in here.

const KEY = "added-sessions";

// An http(s) origin, and only that (no path, no other scheme).
const isOrigin = (v: unknown): v is string => {
  if (typeof v !== "string") return false;
  try {
    const u = new URL(v);
    return (u.protocol === "https:" || u.protocol === "http:") && u.origin === v;
  } catch {
    return false;
  }
};

export type SavedServer = { origin: string; name: string };

const isSaved = (v: unknown): v is SavedServer =>
  !!v && typeof v === "object" && isOrigin((v as SavedServer).origin) && typeof (v as SavedServer).name === "string" && (v as SavedServer).name !== "";

// The valid entries of a list from the server.
export const savedServers = (v: unknown): SavedServer[] => (Array.isArray(v) ? v.filter(isSaved).map(({ origin, name }) => ({ origin, name })) : []);

// The tokens by origin, each with the name it is logged in as there (the
// list's name may be another device's login), and whose they are: the home
// login that made them (another login here does not use them, see
// lib/serverHub).
// unsaved: added, not in the home account's list yet (a list loaded
// meanwhile, in any tab, does not end it).
export type Login = { token: string; name: string; unsaved?: true };
export type Tokens = { owner: string; tokens: Record<string, Login> };

const isLogin = (v: unknown): v is Login =>
  !!v && typeof v === "object" && typeof (v as Login).token === "string" && (v as Login).token !== "" && typeof (v as Login).name === "string";

export const loadTokens = (storage: Storage | undefined): Tokens => {
  try {
    const v = JSON.parse(safeGet(storage, KEY) ?? "null") as { owner?: unknown; tokens?: unknown } | null;
    const entries = Object.entries(v?.tokens && typeof v.tokens === "object" ? v.tokens : {});
    return {
      owner: typeof v?.owner === "string" ? v.owner : "",
      tokens: Object.fromEntries(entries.filter(([o, l]) => isOrigin(o) && isLogin(l)).map(([o, l]) => [o, { token: l.token, name: l.name, ...(l.unsaved === true && { unsaved: true as const }) }])),
    };
  } catch {
    return { owner: "", tokens: {} };
  }
};

export const saveTokens = (storage: Storage | undefined, owner: string, tokens: Record<string, Login>) =>
  void safeSet(storage, KEY, JSON.stringify({ owner, tokens }));

const local = (host: string) => host === "localhost" || host === "127.0.0.1" || host === "[::1]";

// A host without a trailing dot (one spelling of a name).
const host = (url: URL) => url.hostname.replace(/\.$/, "");

// The origin of another server from what the user typed: https (http only
// on this computer, for development), its port kept, any path dropped.
// Not the home server, and (homeCookie: the web app, whose home login is a
// cookie) not another port of its host: browsers send the home cookie to
// every port of a host.
export const serverOrigin = (input: string, home: string, homeCookie = true): { origin: string } | { error: string } => {
  const text = input.trim();
  const invalid = { error: t("login.addressInvalid") };
  let url: URL;
  try {
    url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? text : `https://${text}`);
  } catch {
    return invalid;
  }
  if (!text || !host(url) || (url.protocol !== "https:" && !(url.protocol === "http:" && local(host(url))))) return invalid;
  const origin = `${url.protocol}//${host(url)}${url.port ? `:${url.port}` : ""}`;
  let homeUrl: URL | null = null;
  try {
    homeUrl = new URL(home);
  } catch {
    // No home origin to compare.
  }
  const homeOrigin = homeUrl && `${homeUrl.protocol}//${host(homeUrl)}${homeUrl.port ? `:${homeUrl.port}` : ""}`;
  if (origin === homeOrigin) return { error: t("login.isHomeServer") };
  if (homeCookie && homeUrl && host(homeUrl) === host(url)) return { error: t("login.homePort") };
  return { origin };
};
