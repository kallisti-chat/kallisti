import type { Ban } from "./state";
import { encodeMessage } from "./messages";

const HTML_ESCAPES: Record<string, string> = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': "&quot;",
  "'": "&#39;",
};

// Mumble text messages are HTML. Plain user input must be escaped.
export const escapeHtml = (text: string) => text.replace(/[&<>"']/g, (c) => HTML_ESCAPES[c]!);

// A channel, or one user for a private message.
export type TextTarget = { channelId: number } | { session: number };

// Plain text as Mumble's HTML: escaped, line breaks as tags.
export const textToHtml = (text: string) => escapeHtml(text).replaceAll("\n", "<br>");

export const textMessageInit = (target: TextTarget, text: string) => ({
  ...("channelId" in target ? { channelId: [target.channelId] } : { session: [target.session] }),
  message: textToHtml(text),
});

export const sendText = (target: TextTarget, text: string) =>
  encodeMessage("TextMessage", textMessageInit(target, text));

export const joinChannel = (session: number, channelId: number) =>
  encodeMessage("UserState", { session, channelId });

export const setSelfState = (
  session: number,
  state: { selfMute: boolean; selfDeaf: boolean },
) => encodeMessage("UserState", { session, ...state });

export const requestDescriptions = (channelIds: number[]) =>
  encodeMessage("RequestBlob", { channelDescription: channelIds });

// Registers the own user on the server (needs a client certificate; the
// Kallisti server provides one for accounts).
// Avatars: fetch long pictures; set ours (empty: remove it).
export const requestTextures = (sessions: number[]) => encodeMessage("RequestBlob", { sessionTexture: sessions });
export const setSelfTexture = (session: number, texture: Uint8Array) =>
  encodeMessage("UserState", { session, texture });

// Comments (the bio): fetch long ones; set ours from plain text.
export const requestComments = (sessions: number[]) => encodeMessage("RequestBlob", { sessionComment: sessions });
export const setSelfComment = (session: number, text: string) =>
  encodeMessage("UserState", { session, comment: textToHtml(text) });

export const registerSelf = (session: number) => encodeMessage("UserState", { session, userId: 0 });

// Channel management (the server checks the permissions).
export const createChannel = (parent: number, name: string, opts: { temporary?: boolean } = {}) =>
  encodeMessage("ChannelState", { parent, name, temporary: opts.temporary ?? false });

export const editChannel = (
  channelId: number,
  fields: { name?: string; description?: string; position?: number },
) => encodeMessage("ChannelState", { channelId, ...fields });

export const removeChannel = (channelId: number) => encodeMessage("ChannelRemove", { channelId });

// Moderation (the server checks the permissions): kick or ban (for ever;
// Mumble has no duration here), and server mute or deafen. Moves use
// joinChannel with the other user's session.
export const removeUser = (session: number, reason: string, opts: { ban?: boolean } = {}) =>
  encodeMessage("UserRemove", { session, reason, ...(opts.ban ? { ban: true } : {}) });
export const moderateUser = (session: number, state: { mute: boolean } | { deaf: boolean } | { prioritySpeaker: boolean }) =>
  encodeMessage("UserState", { session, ...state });

// What the own user may do in a channel; the answer is a PermissionQuery.
export const queryPermissions = (channelId: number) => encodeMessage("PermissionQuery", { channelId });

// Admin views: the ban list (Mumble replaces it as a whole) and the
// registered users (a user without a name is unregistered).
export const queryBans = () => encodeMessage("BanList", { query: true });
export const setBans = (bans: readonly Ban[]) => encodeMessage("BanList", { bans: bans.map((b) => ({ ...b })), query: false });
export const queryRegistered = () => encodeMessage("UserList", {});
export const renameRegistered = (userId: number, name: string) => encodeMessage("UserList", { users: [{ userId, name }] });
export const unregister = (userId: number) => encodeMessage("UserList", { users: [{ userId }] });

// A voice target (1 to 30): what voice sent with that target goes to,
// some users (a whisper) or all in a channel (a shout); null removes it.
export const setVoiceTarget = (id: number, to: { sessions: number[] } | { channel: number } | null) =>
  encodeMessage("VoiceTarget", { id, targets: !to ? [] : "sessions" in to ? [{ session: to.sessions }] : [{ channelId: to.channel }] });
