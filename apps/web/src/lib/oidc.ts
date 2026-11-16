import type { Key } from "../i18n/en";
import { t } from "./i18n.svelte";
import { safeSet, tabStorage } from "./storage";

export type OidcLink = { provider: string; enabled?: boolean };
export type OidcProvider = { enabled: boolean; name: string };
export type OidcResult = { kind: "login" | "register" } | { kind: "linked" | "reauth" | "error"; text: string };

const reasons: Record<string, Key> = {
  expired: "login.oidcExpired",
  denied: "login.oidcDenied",
  taken: "login.oidcTaken",
  other: "login.oidcOther",
};

// only known codes become messages; callback parameters are untrusted input.
export const oidcResult = (search: string): OidcResult | null => {
  const query = new URLSearchParams(search);
  switch (query.get("oidc")) {
    case "login": return { kind: "login" };
    case "register": return { kind: "register" };
    case "linked": return { kind: "linked", text: t("login.oidcLinked") };
    case "reauth": return { kind: "reauth", text: t("login.fediReauthDone") };
    case "error": {
      const reason = query.get("reason") ?? "";
      return { kind: "error", text: t(Object.hasOwn(reasons, reason) ? reasons[reason]! : "login.oidcFailed") };
    }
    default: return null;
  }
};

export const withoutOidcResult = (path: string): string => {
  const url = new URL(path, "http://x");
  url.searchParams.delete("oidc");
  url.searchParams.delete("reason");
  return url.pathname + url.search + url.hash;
};

// use the same one-shot return storage as the other external login flow.
export const keepOidcInvite = (invite: string | undefined, returnTo: string) => {
  if (!safeSet(tabStorage(), "kallisti-account-invite", invite ?? "") && invite)
    throw new Error(t("login.oidcStorage"));
  if (!safeSet(tabStorage(), "kallisti-invite", returnTo) && returnTo)
    throw new Error(t("login.oidcStorage"));
};
