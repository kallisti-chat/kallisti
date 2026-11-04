import type { FediLink, FediResult } from "./fedi";
import { t } from "./i18n.svelte";

// The popup in which a Kallisti page adds this server (see Connect): the
// home server that asked (?kallisti-connect=, kept in the tab as mark over
// the login's pages: kept false when the tab keeps nothing), and where it
// is: "start" the login, "deliver" the token, an error, or null (the name
// step: new, or again after a reload, resumed). home null: no popup.
export type ConnectPhase = "start" | "deliver" | { error: string } | null;

export const popupLogin = (s: { asked: string | null; mark: string | null; back: FediResult | null; kept: boolean }) => {
  const resumed = !s.asked && !s.back && !!s.mark;
  const back = (s.back && ["connect", "register", "error"].includes(s.back.kind)) || resumed ? s.mark : null;
  const home = s.asked ?? back;
  const phase: ConnectPhase = !home
    ? null
    : !s.kept
      ? { error: t("login.popupNoStorage") }
      : resumed || s.back?.kind === "register"
        ? null
        : !s.back
          ? "start"
          : s.back.kind === "connect"
            ? "deliver"
            : s.back.kind === "error"
              ? { error: s.back.text }
              : null;
  return { home, resumed, phase };
};

// How the popup on another server can log in (see Connect): through the
// home server, unless the account is named after its fediverse home (the
// home server logs only its own accounts in elsewhere); with the linked
// fediverse account, if its server is known (that server asks the user
// before it gives the home's page the login).
export const connectWays = (account: { name: string; fedi?: FediLink | null }) => ({
  home: !account.name.includes("@"),
  fedi: account.fedi?.host ? { handle: account.fedi.handle, host: account.fedi.host } : null,
});

// The popup's address: the home server's page that asks, and the
// fediverse server to log in with (none: the home server).
export const connectUrl = (origin: string, home: string, via?: string) =>
  `${origin}/?kallisti-connect=${encodeURIComponent(home)}${via ? `&kallisti-via=${encodeURIComponent(via)}` : ""}`;
