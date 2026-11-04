import { may, Perm, selfOf, type ServerState, type User } from "@kallisti/protocol";
import type { TextState } from "./textChannels";

// Registered user ids by role (built-in server). SuperUser (id 0) is an
// admin too. channelMods: each Mumble channel's own moderators (of the
// channels we see).
export type Roles = { admins: number[]; moderators: number[]; channelMods?: Readonly<Record<number, readonly number[]>> };
export type Role = "admin" | "moderator";
// A member list's tag: a server role, or a moderator of that channel.
export type Badge = Role | "channelmod";

// A registered user's role (null: none, or a guest).
export const roleOf = (roles: Roles, userId: number | null): Role | null =>
  userId === null
    ? null
    : userId === 0 || roles.admins.includes(userId)
      ? "admin"
      : roles.moderators.includes(userId)
        ? "moderator"
        : null;

// A user online, or offline (see lib/members): only the user id counts.
type Who = Pick<User, "userId">;

// Admins change roles of registered users (built-in server); SuperUser
// stays SuperUser.
const mayChangeRole = (server: ServerState, text: TextState, user: Who) =>
  text.voiceChats && may(server, 0, Perm.Write) && user.userId !== null && user.userId !== 0;

// What an admin may do about a user's role: make a registered user a
// moderator, or not. Admins stay admins.
export const moderatorAction = (server: ServerState, text: TextState, user: Who): "make" | "remove" | null => {
  if (!mayChangeRole(server, text, user)) return null;
  const role = roleOf(text.roles, user.userId);
  return role === "admin" ? null : role === "moderator" ? "remove" : "make";
};

// Admins make admins; only SuperUser removes them (the server checks).
export const adminAction = (server: ServerState, text: TextState, user: Who): "make" | "remove" | null => {
  if (!mayChangeRole(server, text, user)) return null;
  if (roleOf(text.roles, user.userId) !== "admin") return "make";
  return selfOf(server)?.userId === 0 ? "remove" : null;
};

// Whom an admin may delete in the users list: not SuperUser, and admins
// only as SuperUser (the server checks).
export const mayDeleteUser = (server: ServerState, text: TextState, userId: number): boolean =>
  userId !== 0 && (roleOf(text.roles, userId) !== "admin" || selfOf(server)?.userId === 0);

// Our registered user id (null: a guest, or not connected).
export const myUserId = (server: ServerState): number | null => selfOf(server)?.userId ?? null;

// Staff: admins and moderators (by role or by Write in root).
export const amStaff = (server: ServerState, text: TextState): boolean =>
  roleOf(text.roles, myUserId(server)) !== null || (text.voiceChats && may(server, 0, Perm.Write));

// A Mumble channel and the ones above it, nearest first (none: null).
export const channelsUp = (server: ServerState, channel: number | null): number[] => {
  const out: number[] = [];
  for (let c = channel; c !== null && server.channels[c] && !out.includes(c); c = server.channels[c]?.parent ?? null) out.push(c);
  return out;
};

// A registered user moderates a Mumble channel (null: none): it is among
// the moderators of the channel or of one above it, as Mumble groups
// inherit.
export const channelModIn = (server: ServerState, roles: Roles, channel: number | null, userId: number | null): boolean =>
  userId !== null && channelsUp(server, channel).some((c) => roles.channelMods?.[c]?.includes(userId));

// The tag in the member list of a Mumble channel (a voice channel, or a
// text channel's mirror).
export const badgeOf = (server: ServerState, roles: Roles, channel: number | null, userId: number | null): Badge | null =>
  roleOf(roles, userId) ?? (channelModIn(server, roles, channel, userId) ? "channelmod" : null);

// What to ask for after the moderators dialog: one request per change.
export const channelModActions = (channel: number, before: readonly number[], after: readonly number[]) => [
  ...after.filter((id) => !before.includes(id)).map((id) => ({ channelModerator: id, channel, on: true })),
  ...before.filter((id) => !after.includes(id)).map((id) => ({ channelModerator: id, channel, on: false })),
];
