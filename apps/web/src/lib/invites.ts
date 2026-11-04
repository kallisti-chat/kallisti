// Invite links to private channels: <server>/invite/<token>. The server
// answers the path with the app, which redeems the token once connected
// (after a login if needed: the address stays until then). Staff make
// them in the members dialog; the token is shown once (only its hash is
// kept).

import { t } from "./i18n.svelte";

export type Invite = { id: number; creator: string; created: number; expires?: number; maxUses?: number; uses: number };

export const inviteLink = (origin: string, token: string) => `${origin}/invite/${token}`;

// An invite link of any server (several servers in one client): its
// origin and token; null: none.
export const inviteOf = (url: string): { origin: string; token: string } | null => {
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return null;
  }
  const m = /^\/invite\/([\w-]{20,100})$/.exec(u.pathname);
  // http only on this computer (development), as servers may be added.
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(u.hostname);
  return (u.protocol === "https:" || (u.protocol === "http:" && local)) && m ? { origin: u.origin, token: m[1]! } : null;
};

// Where an invite of origin leads (known: the server in the rail, with a
// session here or not): "home", "show" it, its "login" first, "add" it
// first; null: a guest adds no servers (the link opens as a page).
export const inviteRoute = (origin: string, home: string, known: { session: boolean } | undefined, guest: boolean) =>
  origin === home ? "home" : known?.session ? "show" : guest ? null : known ? "login" : "add";

// null: not an invite link to this server (origin).
export const parseInviteLink = (url: string, origin: string): string | null => {
  let full: string;
  try {
    full = new URL(url, origin).href;
  } catch {
    return null;
  }
  const i = inviteOf(full);
  return i && i.origin === new URL(origin).origin ? i.token : null;
};

// Choices when making a link (0: no end, no limit); the defaults first.
// The labels are getters: read where shown, in the language in use.
export const expiryChoices = [
  { seconds: 7 * 86400, get label() { return t("login.inviteExpiry7Days"); } },
  { seconds: 86400, get label() { return t("login.inviteExpiry1Day"); } },
  { seconds: 0, get label() { return t("login.inviteExpiryNever"); } },
] as const;
export const useChoices = [
  { uses: 0, get label() { return t("login.inviteNoLimit"); } },
  { uses: 1, get label() { return t("login.inviteUses", { count: 1 }); } },
  { uses: 5, get label() { return t("login.inviteUses", { count: 5 }); } },
  { uses: 25, get label() { return t("login.inviteUses", { count: 25 }); } },
] as const;

export const inviteUses = (i: { uses: number; maxUses?: number }) =>
  i.maxUses ? t("login.inviteUsesOfMax", { uses: i.uses, count: i.maxUses }) : t("login.inviteUses", { count: i.uses });

export const inviteExpiry = (expires: number | undefined, now: number) => {
  if (expires === undefined) return t("login.inviteNeverEnds");
  const minutes = Math.floor((expires - now) / 60_000);
  if (expires <= now) return t("login.inviteEnded");
  if (minutes < 1) return t("login.inviteEndsSoon");
  if (minutes < 60) return t("login.inviteEndsMinutes", { count: minutes });
  if (minutes < 24 * 60) return t("login.inviteEndsHours", { count: Math.floor(minutes / 60) });
  return t("login.inviteEndsDays", { count: Math.floor(minutes / (24 * 60)) });
};

// Where a redeemed link leads: its text channel, else the voice channel.
export const invitePlace = (joined: { voice: number; channel?: number; category?: boolean }) =>
  joined.channel !== undefined
    ? { kind: "text" as const, id: joined.channel }
    : joined.category
      ? { kind: "category" as const }
      : { kind: "voice" as const, id: joined.voice };
