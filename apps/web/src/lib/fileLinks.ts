import { messageFiles, type TextMessage } from "./textChannels";

// Private-room media (see the server's filelink): a message's file links
// carry their expiry (ex, Unix seconds) and work for a day. The open
// chat's messages whose links expire within the hour are asked again; they
// come back as edits, with fresh links.

const soon = 3_600_000;

const expiry = (url: string | undefined): number | null => {
  const ex = url === undefined ? null : new URL(url, "http://x").searchParams.get("ex");
  return ex ? Number(ex) * 1000 : null;
};

const links = (m: TextMessage) => messageFiles(m).flatMap((f) => [f.url, f.preview]);

// A message's links in this hour, so each is asked once an hour (again
// only when the server did not answer, for example over its rate limit).
export const linkKey = (m: TextMessage, now: number) => `${Math.floor(now / soon)} ${m.id} ${links(m).join(" ")}`;

export const fileLinksToAsk = (msgs: readonly TextMessage[], asked: ReadonlySet<string>, now: number): TextMessage[] =>
  msgs.filter((m) => !asked.has(linkKey(m, now)) && links(m).some((url) => (expiry(url) ?? Infinity) - now < soon));

export const fileLinksRequest = (messages: number[]) => JSON.stringify({ type: "file-links", messages });
