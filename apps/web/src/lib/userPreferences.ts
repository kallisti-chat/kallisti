import { isBlocked, parseBlocked, toggleBlocked, type Blocked } from "./blocked";
import { parseUserAudio, updateUserAudio, userAudioFor, type UserAudio, type UserAudioPrefs } from "./userAudio";

// Kallisti uses its public origin, including when voice uses a local bridge.
// Plain Mumble uses the requested endpoint, never the bridge's HTTP address.
export const preferenceScope = (socket: string, site?: string): string => {
  const url = new URL(site ?? socket);
  if (!site) {
    const kallisti = url.searchParams.get("kallisti");
    if (kallisti) return preferenceScope(kallisti);
    const host = url.searchParams.get("host");
    if (host) {
      const port = Number(url.searchParams.get("port") ?? 64738);
      if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error("Invalid Mumble preference port");
      const hostname = host.includes(":") && !host.startsWith("[") ? `[${host}]` : host;
      return `mumble://${new URL(`http://${hostname}`).hostname}:${port}`;
    }
  }
  if (url.protocol === "wss:") url.protocol = "https:";
  if (url.protocol === "ws:") url.protocol = "http:";
  if (url.protocol !== "https:" && url.protocol !== "http:") throw new Error("Invalid preference origin");
  return url.origin;
};

type ServerPreferences = { blocked: Blocked; audio: UserAudioPrefs };
export type UserPreferences = Readonly<Record<string, ServerPreferences>>;
const empty: ServerPreferences = { blocked: [], audio: {} };

// New key and version: old name-only entries have no server identity. Do not
// guess from the first server opened or apply them to every server. Leave the
// old keys untouched for recovery; users must set scoped preferences again.
export const parseUserPreferences = (raw: string | null): UserPreferences => {
  try {
    const data = JSON.parse(raw ?? "null");
    if (data?.version !== 1 || !data.servers || typeof data.servers !== "object" || Array.isArray(data.servers)) return {};
    return Object.fromEntries(Object.entries(data.servers).flatMap(([scope, entry]) => {
      if (!entry || typeof entry !== "object" || Array.isArray(entry)) return [];
      const value = entry as Record<string, unknown>;
      return [[scope, { blocked: parseBlocked(JSON.stringify(value.blocked)), audio: parseUserAudio(JSON.stringify(value.audio)) }]];
    }));
  } catch {
    return {};
  }
};

export const bindUserPreferences = (
  store: { readonly value: UserPreferences; set: (value: UserPreferences) => void },
  scope: string,
) => {
  const current = () => Object.hasOwn(store.value, scope) ? store.value[scope]! : empty;
  const set = (value: ServerPreferences) => store.set({ ...store.value, [scope]: value });
  const has = (name: string | null) => isBlocked(current().blocked, name);
  return {
    get list() { return current().blocked; },
    has,
    toggle: (name: string) => set({ ...current(), blocked: toggleBlocked(current().blocked, name) }),
    audioOf: (name: string) => userAudioFor(current().audio, name.toLowerCase()),
    updateAudio: (name: string, patch: Partial<UserAudio>) =>
      set({ ...current(), audio: updateUserAudio(current().audio, name.toLowerCase(), patch) }),
    visible: <T extends { author: string | null }>(messages: readonly T[]): T[] => messages.filter((m) => !has(m.author)),
  };
};
