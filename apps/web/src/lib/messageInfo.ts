import { formatDay, formatTime, type TimeFormat } from "./chat";
import { t } from "./i18n.svelte";

// A message's info (see MessageInfo): when it was sent, edited and pinned.

export const longDate = (at: number, locale?: string, clock?: TimeFormat) => t("chat.dayAtTime", { day: formatDay(at, locale), time: formatTime(at, locale, clock) });

export type InfoRow = { label: string; value: string };
export const infoRows = (m: { at: number; edited?: number; pinned?: number }, locale?: string, clock?: TimeFormat): InfoRow[] => [
  { label: t("chat.infoSent"), value: longDate(m.at, locale, clock) },
  ...(m.edited ? [{ label: t("chat.infoEdited"), value: longDate(m.edited, locale, clock) }] : []),
  ...(m.pinned ? [{ label: t("chat.infoPinned"), value: longDate(m.pinned, locale, clock) }] : []),
];
