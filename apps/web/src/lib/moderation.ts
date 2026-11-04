import {
  channelTree,
  isLobby,
  joinChannel,
  may,
  moderateUser,
  Perm,
  removeUser,
  type Channel,
  type Bytes,
  type ChannelNode,
  type ServerState,
  type User,
} from "@kallisti/protocol";
import { canModerate } from "./channelManagement";
import { mirrorIds, withoutMirrors } from "./mirrors";
import { amStaff, channelModIn, channelsUp, roleOf } from "./roles";
import type { Key } from "../i18n/en";
import { i18n } from "./i18n.svelte";
import type { ChannelTimeout, ManageAction, TextState, Timeout, UserTarget } from "./textChannels";

// What the server settings may show (the server checks again): the ban
// list with Ban in root, changing it with Write too (admins); registered
// users, to delete them, with Register and Write (admins).
export const moderationRights = (server: ServerState) => {
  const admin = may(server, 0, Perm.Write);
  const bans = may(server, 0, Perm.Ban);
  const users = may(server, 0, Perm.Register) && admin;
  return { any: bans || users, bans, editBans: bans && admin, users };
};

// Time choices in seconds: bans (0: for ever) and time-outs.
// label: a text key (shown with t()).
type TimeChoice = { readonly label: Key; readonly seconds: number };
export const BAN_TIMES: readonly TimeChoice[] = [
  { label: "admin.timeOneHour", seconds: 3600 },
  { label: "admin.timeOneDay", seconds: 86400 },
  { label: "admin.timeSevenDays", seconds: 7 * 86400 },
  { label: "admin.timeForever", seconds: 0 },
];
export const TIMEOUT_TIMES: readonly TimeChoice[] = [
  { label: "admin.timeFiveMinutes", seconds: 300 },
  { label: "admin.timeOneHour", seconds: 3600 },
  { label: "admin.timeOneDay", seconds: 86400 },
  { label: "admin.timeOneWeek", seconds: 7 * 86400 },
];
// How far back a user's messages are deleted.
export const DELETE_TIMES: readonly TimeChoice[] = [
  { label: "admin.timeOneHour", seconds: 3600 },
  { label: "admin.time24Hours", seconds: 86400 },
  { label: "admin.timeSevenDays", seconds: 7 * 86400 },
];

// What we may do to another user (the server checks again).
// timedBans: bans for a time (built-in server; else only for ever).
// moveTo: the voice channels we may move the user to; disconnect: to the
// lobby. channelTimeout: the Mumble channel we may time the user out in.
export type UserModeration = {
  kick: boolean;
  ban: boolean;
  timedBans: boolean;
  mute: boolean;
  moveTo: Channel[];
  disconnect: boolean;
  timeout: boolean;
  channelTimeout: number | null;
  deleteMessages: boolean;
};

const flatten = (n: ChannelNode): Channel[] => [n.channel, ...n.children.flatMap(flatten)];

// Voice channels in tree order: without text channel mirrors, and without
// root where it is a lobby.
const voiceChannels = (server: ServerState, text: TextState): Channel[] => {
  const tree = withoutMirrors(channelTree(server), mirrorIds(server, text.channels, text.categories));
  return tree ? flatten(tree).filter((c) => c.id !== 0 || !isLobby(server)) : [];
};

// Mumble's rules: nothing on oneself or SuperUser; kick, ban (Kick or Ban
// in root) and time-outs (MuteDeafen in root: a mute everywhere) on
// admins and moderators only by admins (Write in root); server mute in
// the user's channel; moves with Move there, into channels where we may
// Move or Enter. Who is no staff does not act on staff nor on the
// channel's moderators. Channel time-outs: in the channel we look at
// (view), else the user's, where we moderate. Null: nothing.
export const userModeration = (server: ServerState, text: TextState, user: User, view: number | null = null): UserModeration | null => {
  if (user.session === server.session || user.userId === 0) return null;
  const overStaff = actsOn(server, text, user.userId);
  const staff = amStaff(server, text);
  const shields = (channel: number) => !staff && (roleOf(text.roles, user.userId) !== null || channelModIn(server, text.roles, channel, user.userId));
  const here = !shields(user.channelId);
  const ban = overStaff && may(server, 0, Perm.Ban);
  const move = here && may(server, user.channelId, Perm.Move);
  const m: UserModeration = {
    kick: overStaff && (ban || may(server, 0, Perm.Kick)),
    ban,
    timedBans: ban && text.voiceChats,
    mute: here && may(server, user.channelId, Perm.MuteDeafen),
    moveTo: move
      ? voiceChannels(server, text).filter((c) => c.id !== user.channelId && (may(server, c.id, Perm.Move) || may(server, c.id, Perm.Enter)))
      : [],
    disconnect: move && isLobby(server) && user.channelId !== 0,
    timeout: overStaff && text.voiceChats && may(server, 0, Perm.MuteDeafen),
    channelTimeout:
      [view, user.channelId].find((c) => c !== null && c !== 0 && overStaff && !shields(c) && canModerate(server, text, c)) ?? null,
    deleteMessages: mayDeleteMessages(server, text, user.userId),
  };
  return m.kick || m.mute || m.moveTo.length || m.disconnect || m.timeout || m.channelTimeout !== null || m.deleteMessages ? m : null;
};

// Nothing on SuperUser; on admins and moderators only admins act.
const actsOn = (server: ServerState, text: TextState, userId: number | null) =>
  userId !== 0 && (roleOf(text.roles, userId) === null || may(server, 0, Perm.Write));

// Deleting a user's recent messages at once (built-in server): admins
// and moderators (MakeChannel in root, as single deletes); online or
// offline (userId), guests (null).
export const mayDeleteMessages = (server: ServerState, text: TextState, userId: number | null) =>
  text.voiceChats && may(server, 0, Perm.MakeChannel) && actsOn(server, text, userId);

// Channels whose permissions the user menu needs and does not know: the
// user's channel and the voice channels (to move to).
export const permissionsToAsk = (server: ServerState, text: TextState, user: User): number[] =>
  [...new Set([user.channelId, ...voiceChannels(server, text).map((c) => c.id)])].filter(
    (id) => id !== 0 && server.channelPermissions[id] === undefined,
  );

// When a user's time-out ends (Unix milliseconds), or null: registered
// users by user id, guests by session.
export const timedOutUntil = (timeouts: readonly Timeout[], user: Pick<User, "session" | "userId">): number | null =>
  timeouts.find((t) => (user.userId !== null ? t.user === user.userId : t.session === user.session))?.until ?? null;

// When a user's time-out in a Mumble channel (or one above it) ends, or
// null.
export const channelTimedOutUntil = (
  server: ServerState,
  timeouts: readonly ChannelTimeout[],
  user: Pick<User, "session" | "userId">,
  channel: number | null,
): number | null => {
  const above = channelsUp(server, channel);
  return timedOutUntil(timeouts.filter((t) => above.includes(t.channel)), user);
};

// For people: the end of a time-out.
export const untilText = (until: number) => new Date(until).toLocaleString(i18n.locale, { dateStyle: "medium", timeStyle: "short" });

// An action from the user menu or its confirmation dialog. deleteSeconds:
// also delete the user's messages of that time; delete: only that.
export type ModerationAction =
  | { kind: "kick"; reason: string; deleteSeconds?: number }
  | { kind: "ban"; reason: string; seconds: number; deleteSeconds?: number } // 0: for ever
  | { kind: "timeout"; seconds: number; deleteSeconds?: number } // 0: end it
  | { kind: "channelTimeout"; channel: number; seconds: number } // 0: end it
  | { kind: "mute" | "deaf"; on: boolean }
  | { kind: "move"; channel: number }
  | { kind: "delete"; seconds: number };
type Request = { mumble: Bytes } | { manage: ManageAction };
// The actions the user menu asks about first.
export type ConfirmKind = "kick" | "ban" | "timeout" | "delete";

// What an action sends: a Mumble message, or a request to the Kallisti
// server (timed bans and time-outs: Mumble has neither).
export const moderationRequest = (session: number, a: Exclude<ModerationAction, { kind: "delete" }>): Request => {
  switch (a.kind) {
    case "kick":
      return { mumble: removeUser(session, a.reason) };
    case "ban":
      return a.seconds
        ? { manage: { ban: session, reason: a.reason, seconds: a.seconds } }
        : { mumble: removeUser(session, a.reason, { ban: true }) };
    case "timeout":
      return { manage: { timeout: session, seconds: a.seconds } };
    case "channelTimeout":
      return { manage: { channelTimeout: session, channel: a.channel, seconds: a.seconds } };
    case "mute":
      return { mumble: moderateUser(session, { mute: a.on }) };
    case "deaf":
      return { mumble: moderateUser(session, { deaf: a.on }) };
    case "move":
      return { mumble: joinChannel(session, a.channel) };
  }
};

// All an action sends, in order: the messages first (a guest's are known
// by its connection, which a kick ends), then the action (only on a user
// who is online).
export const moderationRequests = (who: UserTarget, a: ModerationAction): Request[] => {
  const seconds = a.kind === "delete" ? a.seconds : "deleteSeconds" in a ? (a.deleteSeconds ?? 0) : 0;
  const purge: Request[] = seconds ? [{ manage: { deleteMessages: who, seconds } }] : [];
  return a.kind === "delete" || !("session" in who) ? purge : [...purge, moderationRequest(who.session, a)];
};
