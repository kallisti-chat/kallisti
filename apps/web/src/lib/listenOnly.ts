import type { ServerState } from "@kallisti/protocol";

// Listen-only voice channels (built-in server): only the speakers may
// speak. Underneath, Mumble ACL entries (the server writes them).

export type Speaker = { id: number; name: string };
export type ListenOnly = { on: boolean; speakers: Speaker[] };

const byName = (a: Speaker, b: Speaker) => a.name.localeCompare(b.name);

// Who may become a speaker: registered users who are online, by name.
// SuperUser (id 0) always may.
export const speakerCandidates = (server: ServerState): Speaker[] =>
  Object.values(server.users)
    .flatMap((u) => (u.userId ? [{ id: u.userId, name: u.name }] : []))
    .sort(byName);

const speakerIds = (v: ListenOnly) => (v.on ? v.speakers.map((s) => s.id).sort((a, b) => a - b) : []);

// The users to send after the dialog (null: no change); also for
// read-only text channels.
export const changedUsers = (before: ListenOnly, after: ListenOnly) => {
  const ids = speakerIds(after);
  return before.on === after.on && speakerIds(before).join() === ids.join() ? null : ids;
};

// What to ask the server for after the dialog (null: no change).
export const listenOnlyAction = (channel: number, before: ListenOnly, after: ListenOnly) => {
  const users = changedUsers(before, after);
  return users && { listenOnly: channel, on: after.on, users };
};

// Who is in a listen-only channel: whoever speaks there is a speaker, and
// gets the music buffer (see voice/buffer.ts).
export const listenOnlySessions = (server: ServerState, listenOnly: Readonly<Record<number, unknown>>): ReadonlySet<number> =>
  new Set(Object.values(server.users).flatMap((u) => (u.channelId in listenOnly ? [u.session] : [])));
