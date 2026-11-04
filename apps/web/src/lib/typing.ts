// "bob is typing…": who typed in which channel, until when. The server
// passes a signal on at most every few seconds per user and channel.
import { t } from "./i18n.svelte";

export const TYPING_SEND_MS = 3000;
export const TYPING_SHOW_MS = 6000; // two signals, with time to spare

export type Typers = Readonly<Record<number, Readonly<Record<string, number>>>>;

export const typed = (t: Typers, channel: number, name: string, now: number): Typers => ({
  ...t,
  [channel]: { ...t[channel], [name]: now + TYPING_SHOW_MS },
});

export const stoppedTyping = (t: Typers, channel: number, name: string): Typers => {
  if (!t[channel]?.[name]) return t;
  const { [name]: _, ...rest } = t[channel];
  return { ...t, [channel]: rest };
};

export const typingNames = (t: Typers, channel: number, now: number, me: string): string[] =>
  Object.entries(t[channel] ?? {})
    .filter(([name, until]) => until > now && name !== me)
    .map(([name]) => name)
    .sort();

export const typingText = (names: readonly string[]): string => {
  if (names.length === 0) return "";
  const [first, second, third] = names;
  if (names.length === 1) return t("client.typingOne", { name: first! });
  if (names.length === 2) return t("client.typingTwo", { first: first!, second: second! });
  if (names.length === 3) return t("client.typingThree", { first: first!, second: second!, third: third! });
  return t("client.typingMany");
};

export const shouldSendTyping = (last: number | undefined, now: number) =>
  last === undefined || now - last >= TYPING_SEND_MS;
