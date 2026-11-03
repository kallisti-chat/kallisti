import { parseChannelList, type ChannelList } from "./channelList";
import { escapeHtml, type ChatMessage } from "@kallisti/protocol";
import { markdown } from "./markdown";
import { spoilerUpload } from "./media";
import type { Reaction } from "./reactions";
import type { LinkPreview } from "./linkPreview";
import type { Speaker } from "./listenOnly";
import type { Roles } from "./roles";
import { isMentionAll, type MentionAll } from "./mentions";
import type { SearchQuery } from "./search";
import { parsePresence, type PresenceView } from "./presence";
import { parseCustomEmoji, type CustomEmoji } from "./customEmoji";
import { isMessageSticker, parseStickers, type MessageSticker, type Sticker } from "./stickers";
import { parsePersonalEmoji, parseRemovedEmoji, type PersonalEmoji } from "./personalEmoji";
import type { Invite } from "./invites";
import { isWebhook, type Webhook } from "./webhooks";
import type { Members, Online } from "./members";
import { parseConnections, type Connection } from "./connections";
import { isCaptchaLevel, type CaptchaLevel } from "./captcha";
import { parseMessageRules, type MessageRules } from "./messageRules";
import { parseHomeServers, type HomeServers } from "./homeServers";
import { parseRecordings, type FinishedRecording } from "./recording";
import type { TextCategory } from "./categories";
import { isPoll, withPoll, type Poll, type PollBody } from "./polls";
import { followedThreads, isThread, isThreads, withThreads, type Thread, type Threads } from "./threads";

// Text channels come from the Kallisti server (JSON over WebSocket text
// frames). Against a plain Mumble server, the state stays unavailable.

// mumble: the Mumble channel that mirrors it for Mumble clients;
// slowMode: seconds between a user's posts (see lib/slowMode); category:
// see lib/categories (none: at the top).
export type TextChannel = { id: number; name: string; mumble?: number; topic?: string; slowMode?: number; category?: number };
// width, height: pixels (as shown); duration: milliseconds. Missing when
// the server does not know them.
// preview: a smaller picture (see lib/media chatSrc), once the server made one;
// previewType: its type (video/mp4: a large GIF's video, see chatVideo).
// description: the alt text. A spoiler's name starts with "SPOILER_" (see
// lib/media spoilerUpload).
export type Attachment = {
  url: string;
  name: string;
  type: string;
  size: number;
  width?: number;
  height?: number;
  duration?: number;
  preview?: string;
  previewType?: string;
  description?: string;
};
export type TextMessage = {
  id: number;
  channel: number;
  author: string;
  text: string;
  at: number;
  // The author was registered on the Mumble server (a verified name),
  // with this user id (unknown for old messages).
  verified?: boolean;
  authorId?: number;
  uploads?: Attachment[]; // in order
  upload?: Attachment; // older servers: the one file (see messageFiles)
  // A sticker message (text: its name, for what shows only text).
  sticker?: MessageSticker;
  edited?: number; // Unix milliseconds
  pinned?: number; // Unix milliseconds
  // The message it answers (author empty: deleted since).
  reply?: Quote;
  reactions?: Reaction[];
  mentionAll?: MentionAll;
  noPing?: boolean; // a reply that does not ping
  webhook?: number; // posted by this webhook (see lib/webhooks)
  thread?: Thread; // the thread started from it (see lib/threads)
  poll?: Poll; // a poll message (text: the poll as text, see lib/polls)
};
// webhook: the quoted post is a webhook's (see lib/mentions replyPing).
export type Quote = { id: number; author: string; text: string; webhook?: boolean };
// A chat line for display: a text channel message can carry an attachment
// and a verified mark.
// source: the stored message (id and plain text), for editing.
export type DisplayMessage = ChatMessage & {
  uploads?: (Attachment & { spoiler?: true })[];
  poll?: Poll;
  sticker?: MessageSticker;
  verified?: boolean;
  authorId?: number;
  edited?: number;
  pinned?: number;
  source?: { id: number; text: string };
  reply?: Quote;
  reactions?: Reaction[];
  mentionAll?: MentionAll;
  noPing?: boolean; // a reply that does not ping
  webhook?: number; // see TextMessage
};

export type ListenOnlyChannel = { channel: number; speakers: Speaker[] };
// A private channel (by Mumble channel) and, for staff, its members.
// inherited: members by its private category (not its own).
export type PrivateChannel = { channel: number; members?: Speaker[]; inherited?: Speaker[] };
// The other user of a direct chat.
export type Peer = { id: number; name: string };
export type DirectChat = { channel: number; peer: Peer };
// A screen share in a voice channel, by its streamer's session.
export type LiveStream = { session: number; channel: number; preview?: string };

export type SessionStatus = PresenceView & { session: number };
// A running time-out (built-in server): of a registered user (user id) or
// a guest (session); until: Unix milliseconds.
export type Timeout = { user?: number; session?: number; until: number };
// A time-out in a Mumble channel and its subchannels: no writing or
// speaking there.
export type ChannelTimeout = Timeout & { channel: number };

// parent: a thread's text channel.
export type SummaryEntry = { channel: number; voice?: number; parent?: number; latest: number; mentions: number };

export type ServerEvent =
  // voiceChats: voice channels have stored chats (the built-in server).
  | {
      type: "ready";
      channels: TextChannel[];
      categories?: TextCategory[];
      voiceChats?: boolean;
      uploadMax?: number; // bytes per upload
      maxUploads?: number; // files per message
      polls?: boolean; // the server knows polls
      // Online users' pictures are at /api/avatars/<name> too (see
      // lib/avatar onlinePicture).
      avatars?: boolean;
      roles?: Roles;
      listenOnly?: ListenOnlyChannel[];
      private?: PrivateChannel[];
      timeouts?: Timeout[];
      channelTimeouts?: ChannelTimeout[];
      // Guests may only read (see lib/guests).
      guestsReadOnly?: boolean;
      // The browser check for new accounts (see lib/captcha).
      captcha?: CaptchaLevel;
      inviteOnly?: boolean;
      logo?: string;
      // Registered users' direct chats (with messages).
      directs?: DirectChat[];
      streams?: LiveStream[];
      // The recorded voice channels (absent: the server does not record).
      recording?: number[];
      // Registered users: read marks, and per channel the newest message
      // and unread mentions (voice: the voice channel of a voice chat).
      reads?: Record<number, number>;
      summary?: SummaryEntry[];
      // Web clients that are not plainly online, and our chosen status.
      statuses?: SessionStatus[];
      ownStatus?: PresenceView;
      emoji?: CustomEmoji[];
      stickers?: Sticker[];
      // Our personal emoji, and the images moderators removed.
      myEmoji?: PersonalEmoji[];
      removedEmoji?: number[];
      // Registered users but guest accounts (see lib/members); picture: a
      // saved one at /api/avatars/<name>.
      members?: (Peer & { picture?: boolean })[];
      online?: Online;
      // Active threads of the channels we read, those we follow, and how
      // long threads stay active without messages (seconds).
      threads?: Thread[];
      follows?: number[];
      threadArchive?: number;
      // An account's own channel list (see lib/channelList).
      channelList?: ChannelList;
    }
  // A registered user was added or renamed (name), or removed (gone).
  | { type: "member"; id: number; name?: string }
  // A registered user's saved picture was set or removed.
  | { type: "member"; id: number; picture: boolean }
  // A member came online in any channel (session), or went offline.
  | { type: "online"; id: number; session?: number }
  // A registered user's saved bio (asked for, see lib/members).
  | { type: "bio"; user: number; text: string }
  // A web client's status (see lib/presence); plain online: none.
  | ({ type: "status" } & SessionStatus)
  // Our chosen status changed (here or on another device).
  | ({ type: "own-status" } & PresenceView)
  // The custom emoji changed.
  | { type: "emoji"; emoji: CustomEmoji[] }
  | { type: "stickers"; stickers: Sticker[] }
  // Our personal emoji changed (here or on another device); a moderator
  // removed an image (all removed ones).
  | { type: "my-emoji"; emoji: PersonalEmoji[] }
  | { type: "removed-emoji"; ids: number[] }
  | ({ type: "roles" } & Roles)
  // A direct chat with a user (asked for, or a new one's first message).
  | ({ type: "dm" } & DirectChat)
  // The live screen shares changed.
  | { type: "streams"; streams: LiveStream[] }
  // A screen share's new thumbnail (a JPEG data URL).
  | { type: "stream-preview"; session: number; image: string }
  // A token for the stream connection (asked for by the connection).
  | { type: "stream-token"; token: string }
  // The recorded voice channels changed; the finished recordings (staff,
  // asked for); a token for one download (see lib/recording).
  | { type: "recording"; channels: number[] }
  | { type: "recordings"; recordings: FinishedRecording[] }
  | { type: "recording-token"; token: string }
  // The listen-only voice channels changed.
  | { type: "listen-only"; channels: ListenOnlyChannel[] }
  // The private channels we see changed (or their members).
  | { type: "private"; channels: PrivateChannel[] }
  // A private channel's invite links (for staff), a new link's token
  // (only to its maker, once), and the channel a redeemed link opens
  // (channel: a text channel; else the voice channel).
  | { type: "invites"; voice: number; invites: Invite[] }
  | { type: "invite-created"; voice: number; token: string }
  | { type: "invite-joined"; voice: number; channel?: number; category?: boolean }
  // A text channel's webhooks (for staff), and a new one's token (only to
  // its maker, once: see lib/webhooks).
  | { type: "webhooks"; channel: number; webhooks: Webhook[] }
  | { type: "webhook-created"; channel: number; id: number; token: string }
  // A webhook's picture changed (set or removed), for everyone.
  | { type: "webhook-picture"; webhook: number; version: number }
  // The running time-outs changed.
  | { type: "timeouts"; timeouts: Timeout[]; channelTimeouts: ChannelTimeout[] }
  // Admins changed whether guests may only read.
  | { type: "guests-read-only"; on: boolean }
  | { type: "captcha"; level: CaptchaLevel }
  | { type: "invite-only"; on: boolean }
  | { type: "registration-invites"; invites: Invite[] }
  | { type: "registration-invite-created"; token: string }
  // Admins renamed the server (the state has it as the root channel's
  // name: the confirmation only).
  | { type: "server-name"; name: string }
  // The server's logo changed ("": removed; see lib/serverLogo).
  | { type: "logo"; version: string }
  // Every connection (admins, asked for: see lib/connections).
  | { type: "connections"; connections: Connection[] }
  // The message rules (admins, asked for or saved: see lib/messageRules).
  | { type: "message-rules"; rules: MessageRules }
  // Which other servers' accounts log in (admins, asked for or saved: see
  // lib/homeServers).
  | { type: "home-servers"; homes: HomeServers }
  // The channel that holds a voice channel's chat.
  | { type: "voice-chat"; voice: number; channel: number }
  // The text channels changed (an admin managed them).
  | { type: "channels"; channels: TextChannel[]; categories?: TextCategory[]; threads?: Thread[] }
  // A thread was made or changed (replies, name, archived); deleted; the
  // threads we follow; a page of a channel's archived threads (asked for);
  // the archive time changed; a thread asked for by id is not there.
  | { type: "thread"; thread: Thread }
  | { type: "thread-deleted"; channel: number; id: number }
  | { type: "follows"; threads: number[] }
  // The account's own channel list, changed (also on another device).
  | { type: "channel-list"; list: ChannelList }
  | { type: "archived-threads"; channel: number; threads: Thread[]; more: boolean }
  | { type: "thread-archive"; seconds: number }
  | { type: "no-thread"; id: number }
  | { type: "history"; channel: number; messages: TextMessage[]; more: boolean }
  // nonce: on our own message, the one we sent it with (see sendAnswer).
  | { type: "message"; message: TextMessage; nonce?: string }
  | { type: "message-edited"; message: TextMessage }
  // A message was pinned or unpinned (its pinned field says which).
  | { type: "message-pinned"; message: TextMessage }
  // A channel's pinned messages, the newest pin first.
  | { type: "pins"; channel: number; messages: TextMessage[] }
  // Messages of a channel were deleted (the server sends one as
  // "message-deleted").
  | { type: "messages-deleted"; channel: number; ids: number[] }
  | { type: "search-results"; text: string; messages: TextMessage[]; threads?: Thread[] }
  // A session of our account (also this one) read a chat up to a message.
  | { type: "read"; channel: number; message: number }
  // about "manage": the answer to a channel or role change; slow: a post
  // refused by slow mode, and when the wait ends.
  // returned: a refused send's text, for the composer of its chat.
  // about "rate": a background request over the server's rate limit (not
  // shown as an error; a send comes back as "refused").
  // nonce: the refused send's (see sendAnswer).
  | {
      type: "error";
      message: string;
      about?: "manage" | "rate";
      slow?: { channel: number; until: number };
      returned?: { channel: number; text: string };
      nonce?: string;
    }
  | { type: "upload-token"; token: string; tokens?: string[] } // tokens: for several files
  // A message's reactions changed (all of them).
  | { type: "reactions"; channel: number; message: number; reactions: Reaction[] }
  | { type: "poll"; channel: number; message: number; poll: Poll }
  // A message's link preview (null: none).
  | { type: "preview"; message: number; preview: LinkPreview | null }
  // Someone types in a channel (see lib/typing).
  | { type: "typing"; channel: number; name: string }
  // The server dropped messages for us: reload what is shown.
  | { type: "resync" };

export type TextState = {
  available: boolean;
  channels: TextChannel[];
  // The categories with a channel we read, and those we manage.
  categories: readonly TextCategory[];
  voiceChats: boolean;
  uploadMax?: number; // the server's size limit per upload
  maxUploads: number; // files per message (older servers: 1)
  polls: boolean; // the server knows polls (see lib/polls)
  avatars: boolean; // see ready
  // Voice channel id -> the channel that holds its chat.
  voiceChannels: Readonly<Record<number, number>>;
  messages: Readonly<Record<number, TextMessage[]>>; // per channel, by id
  loaded: Readonly<Record<number, boolean>>;
  more: Readonly<Record<number, boolean>>;
  lastError: string | null;
  // The last error answered a background request over the rate limit:
  // it still lets failed requests be asked again, but is not shown.
  quietError?: boolean;
  roles: Roles;
  // Listen-only voice channels: their speakers.
  listenOnly: Readonly<Record<number, Speaker[]>>;
  // Private channels we see, by Mumble channel (a voice channel, or a
  // text channel's mirror): their members (for admins and moderators;
  // others get none).
  private: Readonly<Record<number, Speaker[]>>;
  // A private channel's members by its private category (see readersOf).
  inherited: Readonly<Record<number, Speaker[]>>;
  // Invite links of private channels (staff), once asked for.
  invites: Readonly<Record<number, Invite[]>>;
  // Webhooks of text channels (staff), once asked for.
  webhooks: Readonly<Record<number, Webhook[]>>;
  // Versions of webhook pictures changed since the start (see lib/webhooks).
  webhookPictures: Readonly<Record<number, number>>;
  timeouts: readonly Timeout[];
  channelTimeouts: readonly ChannelTimeout[];
  guestsReadOnly: boolean;
  // Slow mode: when the server said our wait ends, by channel.
  slowUntil: Readonly<Record<number, number>>;
  // The text of the last refused send (n counts them, see ChatPane).
  returned?: { channel: number; text: string; n: number };
  captcha: CaptchaLevel;
  inviteOnly: boolean | null; // null: older server or external Murmur
  registrationInvites: readonly Invite[] | null;
  // The server's logo's version (null: none, undefined: not known before
  // ready; see lib/serverLogo).
  logo: string | null | undefined;
  // Every connection, once asked for (admins).
  connections: Connection[] | null;
  // The message rules, once asked for (admins).
  messageRules: MessageRules | null;
  // Which other servers' accounts log in, once asked for (admins).
  homeServers: HomeServers | null;
  // Direct chats: chat -> the other user.
  directs: Readonly<Record<number, Peer>>;
  // Live screen shares, and their thumbnails by streamer session.
  streams: readonly LiveStream[];
  streamPreviews: Readonly<Record<number, string>>;
  // Recorded voice channels (null: the server does not record), and the
  // finished recordings once asked for (staff).
  recording: readonly number[] | null;
  recordings: FinishedRecording[] | null;
  // From ready (registered users): read marks, and the newest message and
  // unread mentions per channel at that time.
  reads: Readonly<Record<number, number>>;
  summary: Readonly<Record<number, { latest: number; mentions: number }>>;
  // Errors about channel and role changes show as a notice (n counts them), not in
  // the chat.
  notice: { text: string; n: number } | null;
  // The latest search answer.
  search: { text: string; messages: TextMessage[] } | null;
  // Link previews by message (null: none).
  previews: Readonly<Record<number, LinkPreview | null>>;
  // Pinned messages per channel, once asked for (the newest pin first).
  pins: Readonly<Record<number, TextMessage[]>>;
  // Counts resync events, so views can reload.
  resyncs: number;
  // Statuses of web clients by session (plain online: none), and ours as
  // chosen.
  statuses: Readonly<Record<number, PresenceView>>;
  ownStatus: PresenceView;
  // Custom server emoji (image URLs relative to the server).
  emoji: readonly CustomEmoji[];
  // The server's stickers (see lib/stickers).
  stickers: readonly Sticker[];
  // Our personal emoji, and the removed images (by id).
  myEmoji: readonly PersonalEmoji[];
  removedEmoji: readonly number[];
  // Registered users, and the bios of offline ones once asked for (by
  // user id).
  members: Members;
  bios: Readonly<Record<number, string>>;
  // Members with a saved picture (by user id), see lib/avatar.
  pictures: ReadonlySet<number>;
  // Members online, whether or not we see them (see lib/members).
  online: Online;
  // Threads we know (the active ones of the channels we read, and others
  // seen since), those we follow, a channel's archived threads once asked
  // for (pages so far), and the archive time (seconds).
  threads: Threads;
  follows: ReadonlySet<number>;
  // The own channel list kept with the account (null: none, without an
  // account; then the device keeps it).
  channelList: ChannelList | null;
  archived: Readonly<Record<number, { threads: Thread[]; more: boolean }>>;
  threadArchive: number;
};

export const initialTextState: TextState = {
  available: false,
  channels: [],
  categories: [],
  voiceChats: false,
  maxUploads: 1,
  polls: false,
  avatars: false,
  roles: { admins: [], moderators: [] },
  listenOnly: {},
  private: {},
  inherited: {},
  invites: {},
  webhooks: {},
  webhookPictures: {},
  timeouts: [],
  channelTimeouts: [],
  guestsReadOnly: false,
  slowUntil: {},
  captcha: "off",
  inviteOnly: null,
  registrationInvites: null,
  logo: undefined,
  connections: null,
  messageRules: null,
  homeServers: null,
  directs: {},
  streams: [],
  streamPreviews: {},
  recording: null,
  recordings: null,
  reads: {},
  summary: {},
  voiceChannels: {},
  messages: {},
  loaded: {},
  more: {},
  lastError: null,
  notice: null,
  search: null,
  previews: {},
  pins: {},
  resyncs: 0,
  statuses: {},
  ownStatus: { status: "online" },
  emoji: [],
  stickers: [],
  myEmoji: [],
  removedEmoji: [],
  members: {},
  pictures: new Set(),
  online: {},
  bios: {},
  threads: {},
  follows: new Set(),
  channelList: null,
  archived: {},
  threadArchive: 3 * 24 * 3600,
};

const isNum = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);
const isSummaryEntry = (v: unknown): v is SummaryEntry =>
  typeof v === "object" &&
  v !== null &&
  isNum((v as SummaryEntry).channel) &&
  isNum((v as SummaryEntry).latest) &&
  isNum((v as SummaryEntry).mentions) &&
  ((v as SummaryEntry).voice === undefined || isNum((v as SummaryEntry).voice));
const isIds = (v: unknown): v is number[] => Array.isArray(v) && v.every(isNum);
// Channel moderators come as a list, [{channel, users}] (older servers:
// none).
const parseChannelMods = (v: unknown): Pick<Roles, "channelMods"> =>
  Array.isArray(v)
    ? { channelMods: Object.fromEntries(v.flatMap((m) => (isObj(m) && isNum(m.channel) && isIds(m.users) ? [[m.channel, m.users]] : []))) }
    : {};
const parseRoles = (v: unknown): Roles | null =>
  typeof v === "object" && v !== null && isIds((v as Roles).admins) && isIds((v as Roles).moderators)
    ? { admins: (v as Roles).admins, moderators: (v as Roles).moderators, ...parseChannelMods((v as Record<string, unknown>).channelModerators) }
    : null;

const isStr = (v: unknown): v is string => typeof v === "string";
const parseSessionStatus = (v: unknown): SessionStatus | null => {
  const p = parsePresence(v);
  return p && isNum((v as SessionStatus).session) ? { ...p, session: (v as SessionStatus).session } : null;
};
const statusMap = (list: readonly SessionStatus[]) =>
  Object.fromEntries(list.map(({ session, ...v }) => [session, v])) as Record<number, PresenceView>;
const isPlain = (v: PresenceView) => v.status === "online" && !v.emoji && !v.text;
const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object";
const isSpeaker = (v: unknown): v is Speaker => isObj(v) && isNum(v.id) && isStr(v.name);
const isListenOnly = (v: unknown): v is ListenOnlyChannel[] =>
  Array.isArray(v) && v.every((c) => isObj(c) && isNum(c.channel) && Array.isArray(c.speakers) && c.speakers.every(isSpeaker));
const listenOnlyMap = (cs: ListenOnlyChannel[]) => Object.fromEntries(cs.map((c) => [c.channel, c.speakers]));
const isSpeakers = (v: unknown) => v === undefined || (Array.isArray(v) && v.every(isSpeaker));
const isPrivateList = (v: unknown): v is PrivateChannel[] =>
  Array.isArray(v) && v.every((c) => isObj(c) && isNum(c.channel) && isSpeakers(c.members) && isSpeakers(c.inherited));
const isInvite = (v: unknown): v is Invite =>
  isObj(v) &&
  isNum(v.id) &&
  isStr(v.creator) &&
  isNum(v.created) &&
  isNum(v.uses) &&
  (v.expires === undefined || isNum(v.expires)) &&
  (v.maxUses === undefined || isNum(v.maxUses));
const privateMap = (cs: PrivateChannel[]) => Object.fromEntries(cs.map((c) => [c.channel, c.members ?? []]));
const inheritedMap = (cs: PrivateChannel[]) => Object.fromEntries(cs.flatMap((c) => (c.inherited?.length ? [[c.channel, c.inherited]] : [])));
const isTimeouts = (v: unknown): v is Timeout[] =>
  Array.isArray(v) && v.every((t) => isObj(t) && isNum(t.until) && (isNum(t.user) !== isNum(t.session)));
const isChannelTimeouts = (v: unknown): v is ChannelTimeout[] => isTimeouts(v) && v.every((t) => isNum((t as ChannelTimeout).channel));
// Direct chats follow their peer's renames (a removed peer keeps its name).
const renamePeer = (directs: TextState["directs"], id: number, name: string) =>
  Object.fromEntries(Object.entries(directs).map(([ch, p]) => [ch, p.id === id ? { id, name } : p]));
const toggled = (ids: ReadonlySet<number>, id: number, on: boolean) => {
  const out = new Set(ids);
  if (on) out.add(id);
  else out.delete(id);
  return out;
};
const isStreamPreview = (v: unknown): v is string => isStr(v) && v.startsWith("data:image/jpeg;base64,");
const isStreams = (v: unknown): v is LiveStream[] =>
  Array.isArray(v) && v.every((s) => isObj(s) && isNum(s.session) && isNum(s.channel) && (s.preview === undefined || isStreamPreview(s.preview)));
// Thumbnails of the streams that are still live.
const keepPreviews = (previews: TextState["streamPreviews"], streams: readonly LiveStream[]) =>
  Object.fromEntries(streams.flatMap((s) => (s.preview ?? previews[s.session] ? [[s.session, s.preview ?? previews[s.session]!]] : [])));
const isDirect = (v: unknown): v is DirectChat =>
  isObj(v) && isNum(v.channel) && isObj(v.peer) && isNum(v.peer.id) && isStr(v.peer.name);

const isChannel = (v: unknown): v is TextChannel =>
  isObj(v) &&
  isNum(v.id) &&
  isStr(v.name) &&
  (v.mumble === undefined || isNum(v.mumble)) &&
  (v.topic === undefined || typeof v.topic === "string") &&
  (v.slowMode === undefined || isNum(v.slowMode)) &&
  (v.category === undefined || isNum(v.category));
const isCategories = (v: unknown): v is TextCategory[] =>
  Array.isArray(v) && v.every((c) => isObj(c) && isNum(c.id) && isStr(c.name) && (c.mumble === undefined || isNum(c.mumble)));
// Categories, if the event has valid ones (older servers: none).
const withCategories = (v: Record<string, unknown>) => (isCategories(v.categories) ? { categories: v.categories } : {});
const optStr = (v: unknown) => v === undefined || isStr(v);
const optSize = (v: unknown) => v === undefined || (isNum(v) && v > 0);
const isAttachment = (v: unknown): v is Attachment =>
  isObj(v) &&
  isStr(v.url) &&
  isStr(v.name) &&
  isStr(v.type) &&
  isNum(v.size) &&
  optSize(v.width) &&
  optSize(v.height) &&
  optSize(v.duration) &&
  optStr(v.preview) &&
  optStr(v.previewType) &&
  optStr(v.description);
const isPreview = (v: unknown): v is LinkPreview =>
  isObj(v) &&
  isStr(v.url) &&
  /^https?:\/\//.test(v.url) &&
  isStr(v.title) &&
  optStr(v.description) &&
  optStr(v.site) &&
  (v.image === undefined || (isStr(v.image) && v.image.startsWith("/api/link-image/"))) &&
  optSize(v.imageWidth) &&
  optSize(v.imageHeight);

const isReactions = (v: unknown): v is Reaction[] =>
  Array.isArray(v) &&
  v.every(
    (r) =>
      isObj(r) &&
      isStr(r.emoji) &&
      Array.isArray(r.users) &&
      r.users.every(isStr) &&
      Array.isArray(r.ids) &&
      r.ids.every(isNum) &&
      r.ids.length === r.users.length,
  );

const isMessage = (v: unknown): v is TextMessage =>
  isObj(v) &&
  isNum(v.id) &&
  isNum(v.channel) &&
  isStr(v.author) &&
  isStr(v.text) &&
  isNum(v.at) &&
  (v.verified === undefined || typeof v.verified === "boolean") &&
  (v.authorId === undefined || isNum(v.authorId)) &&
  (v.edited === undefined || isNum(v.edited)) &&
  (v.pinned === undefined || isNum(v.pinned)) &&
  (v.reply === undefined ||
    (isObj(v.reply) && isNum(v.reply.id) && isStr(v.reply.author) && isStr(v.reply.text) && (v.reply.webhook === undefined || typeof v.reply.webhook === "boolean"))) &&
  (v.upload === undefined || isAttachment(v.upload)) &&
  (v.uploads === undefined || (Array.isArray(v.uploads) && v.uploads.every(isAttachment))) &&
  (v.sticker === undefined || isMessageSticker(v.sticker)) &&
  (v.poll === undefined || isPoll(v.poll)) &&
  (v.reactions === undefined || isReactions(v.reactions)) &&
  (v.mentionAll === undefined || isMentionAll(v.mentionAll)) &&
  (v.noPing === undefined || typeof v.noPing === "boolean") &&
  (v.webhook === undefined || isNum(v.webhook)) &&
  (v.thread === undefined || isThread(v.thread));

// Network input: anything unexpected is dropped.
export const parseServerEvent = (raw: string): ServerEvent | null => {
  let v: unknown;
  try {
    v = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isObj(v)) return null;
  switch (v.type) {
    case "ready": {
      if (!Array.isArray(v.channels) || !v.channels.every(isChannel)) return null;
      const roles = parseRoles(v.roles);
      const reads = isObj(v.reads) && Object.values(v.reads).every(isNum) ? (v.reads as Record<number, number>) : undefined;
      const summary = Array.isArray(v.summary) ? v.summary.filter(isSummaryEntry) : undefined;
      const statuses = Array.isArray(v.statuses) ? v.statuses.flatMap((s) => parseSessionStatus(s) ?? []) : undefined;
      const ownStatus = parsePresence(v.ownStatus);
      const emoji = parseCustomEmoji(v.emoji);
      const stickers = parseStickers(v.stickers);
      const myEmoji = parsePersonalEmoji(v.myEmoji);
      const removedEmoji = parseRemovedEmoji(v.removedEmoji);
      return {
        type: "ready",
        channels: v.channels,
        ...withCategories(v),
        ...(v.voiceChats === true ? { voiceChats: true } : {}),
        ...(isNum(v.uploadMax) && v.uploadMax > 0 ? { uploadMax: v.uploadMax } : {}),
        ...(isNum(v.maxUploads) && v.maxUploads >= 1 ? { maxUploads: Math.floor(v.maxUploads) } : {}),
        ...(v.polls === true ? { polls: true } : {}),
        ...(v.avatars === true ? { avatars: true } : {}),
        ...(roles ? { roles } : {}),
        ...(isListenOnly(v.listenOnly) ? { listenOnly: v.listenOnly } : {}),
        ...(isPrivateList(v.private) ? { private: v.private } : {}),
        ...(isTimeouts(v.timeouts) ? { timeouts: v.timeouts } : {}),
        ...(isChannelTimeouts(v.channelTimeouts) ? { channelTimeouts: v.channelTimeouts } : {}),
        ...(v.guestsReadOnly === true ? { guestsReadOnly: true } : {}),
        ...(typeof v.inviteOnly === "boolean" ? { inviteOnly: v.inviteOnly } : {}),
        ...(isCaptchaLevel(v.captcha) ? { captcha: v.captcha } : {}),
        ...(typeof v.logo === "string" && v.logo ? { logo: v.logo } : {}),
        ...(Array.isArray(v.directs) ? { directs: v.directs.filter(isDirect) } : {}),
        ...(isStreams(v.streams) ? { streams: v.streams } : {}),
        ...(Array.isArray(v.recording) && v.recording.every(isNum) ? { recording: v.recording } : {}),
        ...(reads ? { reads: Object.fromEntries(Object.entries(reads).map(([k, n]) => [Number(k), n])) } : {}),
        ...(summary ? { summary } : {}),
        ...(statuses ? { statuses } : {}),
        ...(ownStatus ? { ownStatus } : {}),
        ...(emoji ? { emoji } : {}),
        ...(stickers ? { stickers } : {}),
        ...(myEmoji ? { myEmoji } : {}),
        ...(removedEmoji ? { removedEmoji } : {}),
        ...(Array.isArray(v.members) && v.members.every(isSpeaker) ? { members: v.members } : {}),
        ...(isObj(v.online) && Object.values(v.online).every(isNum) ? { online: v.online as Online } : {}),
        ...(isThreads(v.threads) ? { threads: v.threads } : {}),
        ...(isIds(v.follows) ? { follows: v.follows } : {}),
        ...(isObj(v.channelList) ? { channelList: parseChannelList(v.channelList) } : {}),
        ...(isNum(v.threadArchive) ? { threadArchive: v.threadArchive } : {}),
      };
    }
    case "member":
      if (!isNum(v.id)) return null;
      if (typeof v.picture === "boolean") return { type: "member", id: v.id, picture: v.picture };
      return isStr(v.name) ? { type: "member", id: v.id, name: v.name } : v.gone === true ? { type: "member", id: v.id } : null;
    case "online":
      return isNum(v.id) ? { type: "online", id: v.id, ...(isNum(v.session) ? { session: v.session } : {}) } : null;
    case "bio":
      return isNum(v.user) && isStr(v.text) ? { type: "bio", user: v.user, text: v.text } : null;
    case "emoji": {
      const emoji = parseCustomEmoji(v.emoji);
      return emoji && { type: "emoji", emoji };
    }
    case "stickers": {
      const stickers = parseStickers(v.stickers);
      return stickers && { type: "stickers", stickers };
    }
    case "my-emoji": {
      const emoji = parsePersonalEmoji(v.emoji);
      return emoji && { type: "my-emoji", emoji };
    }
    case "removed-emoji": {
      const ids = parseRemovedEmoji(v.ids);
      return ids && { type: "removed-emoji", ids };
    }
    case "roles": {
      const roles = parseRoles(v);
      return roles && { type: "roles", ...roles };
    }
    case "listen-only":
      return isListenOnly(v.channels) ? { type: "listen-only", channels: v.channels } : null;
    case "private":
      return isPrivateList(v.channels) ? { type: "private", channels: v.channels } : null;
    case "invites":
      return isNum(v.voice) && Array.isArray(v.invites) && v.invites.every(isInvite) ? { type: "invites", voice: v.voice, invites: v.invites } : null;
    case "invite-only":
      return typeof v.on === "boolean" ? { type: "invite-only", on: v.on } : null;
    case "registration-invites":
      return Array.isArray(v.invites) && v.invites.every(isInvite) ? { type: "registration-invites", invites: v.invites } : null;
    case "registration-invite-created":
      return isStr(v.token) ? { type: "registration-invite-created", token: v.token } : null;
    case "invite-created":
      return isNum(v.voice) && isStr(v.token) ? { type: "invite-created", voice: v.voice, token: v.token } : null;
    case "webhooks":
      return isNum(v.channel) && Array.isArray(v.webhooks) && v.webhooks.every(isWebhook) ? { type: "webhooks", channel: v.channel, webhooks: v.webhooks } : null;
    case "webhook-created":
      return isNum(v.channel) && isNum(v.id) && isStr(v.token) ? { type: "webhook-created", channel: v.channel, id: v.id, token: v.token } : null;
    case "webhook-picture":
      return isNum(v.webhook) && isNum(v.version) ? { type: "webhook-picture", webhook: v.webhook, version: v.version } : null;
    case "invite-joined":
      if (!isNum(v.voice) || (v.channel !== undefined && !isNum(v.channel))) return null;
      return { type: "invite-joined", voice: v.voice, ...(v.channel !== undefined && { channel: v.channel }), ...(v.category === true && { category: true }) };
    case "status": {
      const s = parseSessionStatus(v);
      return s && { type: "status", ...s };
    }
    case "own-status": {
      const p = parsePresence(v);
      return p && { type: "own-status", ...p };
    }
    case "guests-read-only":
      return typeof v.on === "boolean" ? { type: "guests-read-only", on: v.on } : null;
    case "captcha":
      return isCaptchaLevel(v.level) ? { type: "captcha", level: v.level } : null;
    case "server-name":
      return typeof v.name === "string" ? { type: "server-name", name: v.name } : null;
    case "logo":
      return typeof v.version === "string" ? { type: "logo", version: v.version } : null;
    case "connections": {
      const connections = parseConnections(v.connections);
      return connections && { type: "connections", connections };
    }
    case "message-rules": {
      const rules = parseMessageRules(v.rules);
      return rules && { type: "message-rules", rules };
    }
    case "home-servers": {
      const homes = parseHomeServers(v.homes);
      return homes && { type: "home-servers", homes };
    }
    case "timeouts":
      return isTimeouts(v.timeouts)
        ? { type: "timeouts", timeouts: v.timeouts, channelTimeouts: isChannelTimeouts(v.channelTimeouts) ? v.channelTimeouts : [] }
        : null;
    case "streams":
      return isStreams(v.streams) ? { type: "streams", streams: v.streams } : null;
    case "stream-token":
      return isStr(v.token) ? { type: "stream-token", token: v.token } : null;
    case "recording":
      return Array.isArray(v.channels) && v.channels.every(isNum) ? { type: "recording", channels: v.channels } : null;
    case "recordings": {
      const recordings = parseRecordings(v.recordings);
      return recordings && { type: "recordings", recordings };
    }
    case "recording-token":
      return isStr(v.token) ? { type: "recording-token", token: v.token } : null;
    case "stream-preview": // "": the preview was turned off
      return isNum(v.session) && (v.image === "" || isStreamPreview(v.image)) ? { type: "stream-preview", session: v.session, image: v.image as string } : null;
    case "dm":
      return isDirect(v) ? { type: "dm", channel: v.channel, peer: { id: v.peer.id, name: v.peer.name } } : null;
    case "voice-chat":
      return isNum(v.voice) && isNum(v.channel) ? { type: "voice-chat", voice: v.voice, channel: v.channel } : null;
    case "channels":
      if (v.categories !== undefined && !isCategories(v.categories)) return null;
      return Array.isArray(v.channels) && v.channels.every(isChannel)
        ? { type: "channels", channels: v.channels, ...withCategories(v), ...(isThreads(v.threads) ? { threads: v.threads } : {}) }
        : null;
    case "thread":
      return isThread(v.thread) ? { type: "thread", thread: v.thread } : null;
    case "thread-deleted":
      return isNum(v.channel) && isNum(v.id) ? { type: "thread-deleted", channel: v.channel, id: v.id } : null;
    case "follows":
      return isIds(v.threads) ? { type: "follows", threads: v.threads } : null;
    case "channel-list":
      return isObj(v.list) ? { type: "channel-list", list: parseChannelList(v.list) } : null;
    case "archived-threads":
      return isNum(v.channel) && isThreads(v.threads) ? { type: "archived-threads", channel: v.channel, threads: v.threads, more: v.more === true } : null;
    case "thread-archive":
      return isNum(v.seconds) ? { type: "thread-archive", seconds: v.seconds } : null;
    case "no-thread":
      return isNum(v.id) ? { type: "no-thread", id: v.id } : null;
    case "history":
      return isNum(v.channel) && Array.isArray(v.messages) && v.messages.every(isMessage)
        ? { type: "history", channel: v.channel, messages: v.messages, more: v.more === true }
        : null;
    case "message":
      return isMessage(v.message) ? { type: v.type, message: v.message, ...(isStr(v.nonce) ? { nonce: v.nonce } : {}) } : null;
    case "message-edited":
    case "message-pinned":
      return isMessage(v.message) ? { type: v.type, message: v.message } : null;
    case "pins":
      return isNum(v.channel) && Array.isArray(v.messages) && v.messages.every(isMessage)
        ? { type: "pins", channel: v.channel, messages: v.messages }
        : null;
    case "message-deleted":
      return isNum(v.channel) && isNum(v.id) ? { type: "messages-deleted", channel: v.channel, ids: [v.id] } : null;
    case "messages-deleted":
      return isNum(v.channel) && Array.isArray(v.ids) && v.ids.every(isNum) ? { type: "messages-deleted", channel: v.channel, ids: v.ids } : null;
    case "search-results":
      return isStr(v.text) && Array.isArray(v.messages) && v.messages.every(isMessage)
        ? { type: "search-results", text: v.text, messages: v.messages, ...(isThreads(v.threads) ? { threads: v.threads } : {}) }
        : null;
    case "read":
      return isNum(v.channel) && isNum(v.message) ? { type: "read", channel: v.channel, message: v.message } : null;
    case "error": {
      if (!isStr(v.message)) return null;
      const error = { type: "error" as const, message: v.message, ...(isStr(v.nonce) ? { nonce: v.nonce } : {}) };
      const returned = isNum(v.channel) && isStr(v.text) ? { returned: { channel: v.channel, text: v.text } } : {};
      if (v.about === "slow-mode" && isNum(v.channel) && isNum(v.until)) return { ...error, slow: { channel: v.channel, until: v.until }, ...returned };
      if (v.about === "refused") return { ...error, ...returned };
      return v.about === "manage" || v.about === "rate" ? { ...error, about: v.about } : error;
    }
    case "upload-token":
      return isStr(v.token)
        ? { type: "upload-token", token: v.token, ...(Array.isArray(v.tokens) && v.tokens.length && v.tokens.every(isStr) && { tokens: v.tokens }) }
        : null;
    case "resync":
      return { type: "resync" };
    case "typing":
      return isNum(v.channel) && isStr(v.name) ? { type: "typing", channel: v.channel, name: v.name } : null;
    case "preview":
      if (!isNum(v.message)) return null;
      if (v.preview === null) return { type: "preview", message: v.message, preview: null };
      return isPreview(v.preview) ? { type: "preview", message: v.message, preview: v.preview } : null;
    case "reactions":
      return isNum(v.channel) && isNum(v.message) && isReactions(v.reactions)
        ? { type: "reactions", channel: v.channel, message: v.message, reactions: v.reactions }
        : null;
    case "poll":
      return isNum(v.channel) && isNum(v.message) && isPoll(v.poll) ? { type: "poll", channel: v.channel, message: v.message, poll: v.poll } : null;
    default:
      return null;
  }
};

// A channel's pin list after f, if it is loaded.
const patchPins = (pins: TextState["pins"], channel: number, f: (ms: TextMessage[]) => TextMessage[]) =>
  pins[channel] ? { ...pins, [channel]: f(pins[channel]) } : pins;

// A pin list after a message was pinned or unpinned: the newest pin first.
const withPin = (list: readonly TextMessage[], m: TextMessage) =>
  [...list.filter((x) => x.id !== m.id), ...(m.pinned ? [m] : [])].sort((x, y) => (y.pinned ?? 0) - (x.pinned ?? 0));

// b's messages over a's (a poll keeps our votes and its end, see keepMine).
const merge = (a: readonly TextMessage[] = [], b: readonly TextMessage[]) => {
  const byId = new Map(a.map((m) => [m.id, m]));
  for (const m of b) byId.set(m.id, keepMine(byId.get(m.id), m));
  return [...byId.values()].sort((x, y) => x.id - y.id);
};

// The threads messages started (their quote: the message).
const threadsIn = (ms: readonly TextMessage[]) =>
  ms.flatMap((m) => (m.thread ? [{ ...m.thread, quote: m.thread.quote ?? { id: m.id, author: m.author, text: m.text } }] : []));

// Threads after a channel list came: its active ones, and the archived
// ones we knew of channels in it.
const keptThreads = (threads: Threads, channels: readonly TextChannel[], active: readonly Thread[]) =>
  withThreads(Object.fromEntries(Object.entries(threads).filter(([, t]) => t.archived && channels.some((c) => c.id === t.channel))), active);

// A message again from the server: our poll votes stay where it sent none.
const keepMine = (old: TextMessage | undefined, m: TextMessage) => (old && m.poll ? withPoll({ ...m, poll: old.poll }, m.poll) : m);

const patchSearch = (search: TextState["search"], f: (ms: TextMessage[]) => TextMessage[]) =>
  search && { ...search, messages: f(search.messages) };

// A search is on its way: the old results go.
export const startSearch = (state: TextState): TextState => ({ ...state, search: null });

export const reduceText = (state: TextState, event: ServerEvent): TextState => {
  switch (event.type) {
    case "ready":
      return {
        ...state,
        available: true,
        channels: event.channels,
        categories: event.categories ?? [],
        voiceChats: event.voiceChats === true,
        uploadMax: event.uploadMax,
        maxUploads: event.maxUploads ?? 1,
        polls: event.polls === true,
        avatars: event.avatars === true,
        roles: event.roles ?? initialTextState.roles,
        emoji: event.emoji ?? [],
        stickers: event.stickers ?? [],
        myEmoji: event.myEmoji ?? [],
        removedEmoji: event.removedEmoji ?? [],
        members: Object.fromEntries((event.members ?? []).map((p) => [p.id, p.name])),
        pictures: new Set((event.members ?? []).flatMap((p) => (p.picture === true ? [p.id] : []))),
        online: event.online ?? {},
        bios: {},
        listenOnly: listenOnlyMap(event.listenOnly ?? []),
        private: privateMap(event.private ?? []),
        inherited: inheritedMap(event.private ?? []),
        timeouts: event.timeouts ?? [],
        channelTimeouts: event.channelTimeouts ?? [],
        guestsReadOnly: event.guestsReadOnly === true,
        captcha: event.captcha ?? "off",
        inviteOnly: event.inviteOnly ?? null,
        registrationInvites: null,
        logo: event.logo ?? null,
        directs: { ...state.directs, ...Object.fromEntries((event.directs ?? []).map((d) => [d.channel, d.peer])) },
        streams: event.streams ?? [],
        streamPreviews: keepPreviews({}, event.streams ?? []),
        recording: event.recording ?? null,
        reads: event.reads ?? {},
        statuses: statusMap(event.statuses ?? []),
        ownStatus: event.ownStatus ?? initialTextState.ownStatus,
        summary: Object.fromEntries((event.summary ?? []).map((e) => [e.channel, { latest: e.latest, mentions: e.mentions }])),
        // As a channel list (a ready after a resync must not close an open
        // archived thread).
        threads: keptThreads(state.threads, event.channels, event.threads ?? []),
        follows: new Set(event.follows ?? []),
        channelList: event.channelList ?? null,
        archived: {},
        threadArchive: event.threadArchive ?? initialTextState.threadArchive,
        voiceChannels: {
          ...state.voiceChannels,
          ...Object.fromEntries((event.summary ?? []).flatMap((e) => (e.voice === undefined ? [] : [[e.voice, e.channel]]))),
        },
      };
    case "roles":
      return { ...state, roles: { admins: event.admins, moderators: event.moderators, ...(event.channelMods ? { channelMods: event.channelMods } : {}) } };
    case "emoji":
      return { ...state, emoji: event.emoji };
    case "stickers":
      return { ...state, stickers: event.stickers };
    case "my-emoji":
      return { ...state, myEmoji: event.emoji };
    case "removed-emoji":
      return { ...state, removedEmoji: event.ids };
    case "member": {
      if ("picture" in event) return { ...state, pictures: toggled(state.pictures, event.id, event.picture) };
      const { [event.id]: _, ...rest } = state.members;
      if (event.name === undefined) return { ...state, members: rest, pictures: toggled(state.pictures, event.id, false) };
      return { ...state, members: { ...rest, [event.id]: event.name }, directs: renamePeer(state.directs, event.id, event.name) };
    }
    case "online": {
      const { [event.id]: _, ...rest } = state.online;
      return { ...state, online: event.session === undefined ? rest : { ...rest, [event.id]: event.session } };
    }
    case "bio":
      return { ...state, bios: { ...state.bios, [event.user]: event.text } };
    case "listen-only":
      return { ...state, listenOnly: listenOnlyMap(event.channels) };
    case "private":
      return { ...state, private: privateMap(event.channels), inherited: inheritedMap(event.channels) };
    case "invites":
      return { ...state, invites: { ...state.invites, [event.voice]: event.invites } };
    case "webhooks":
      return { ...state, webhooks: { ...state.webhooks, [event.channel]: event.webhooks } };
    case "webhook-picture":
      return { ...state, webhookPictures: { ...state.webhookPictures, [event.webhook]: event.version } };
    case "registration-invite-created": // only the requester shows its token
    case "invite-created": // the client shows it once (lib/invites)
    case "invite-joined":
    case "webhook-created": // the same (lib/webhooks)
      return state;
    case "status": {
      const { type: _, session, ...v } = event;
      const { [session]: __, ...rest } = state.statuses;
      return { ...state, statuses: isPlain(v) ? rest : { ...rest, [session]: v } };
    }
    case "own-status": {
      const { type: _, ...v } = event;
      return { ...state, ownStatus: v };
    }
    case "timeouts":
      return { ...state, timeouts: event.timeouts, channelTimeouts: event.channelTimeouts };
    case "guests-read-only":
      return { ...state, guestsReadOnly: event.on };
    case "captcha":
      return { ...state, captcha: event.level };
    case "invite-only":
      return { ...state, inviteOnly: event.on };
    case "registration-invites":
      return { ...state, registrationInvites: event.invites };
    case "server-name":
      return state; // the root channel's name (Mumble)
    case "logo":
      return { ...state, logo: event.version || null };
    case "connections":
      return { ...state, connections: event.connections };
    case "message-rules":
      return { ...state, messageRules: event.rules };
    case "home-servers":
      return { ...state, homeServers: event.homes };
    case "streams":
      return { ...state, streams: event.streams, streamPreviews: keepPreviews(state.streamPreviews, event.streams) };
    case "stream-preview": {
      const { [event.session]: _, ...rest } = state.streamPreviews;
      return { ...state, streamPreviews: event.image ? { ...rest, [event.session]: event.image } : rest };
    }
    case "stream-token": // answered by the connection, not state
    case "recording-token":
      return state;
    case "recording":
      return { ...state, recording: event.channels };
    case "recordings":
      return { ...state, recordings: event.recordings };
    case "dm":
      return state.directs[event.channel]?.name === event.peer.name
        ? state
        : { ...state, directs: { ...state.directs, [event.channel]: event.peer } };
    case "voice-chat":
      return { ...state, voiceChannels: { ...state.voiceChannels, [event.voice]: event.channel } };
    case "channels":
      return { ...state, channels: event.channels, categories: event.categories ?? [], threads: keptThreads(state.threads, event.channels, event.threads ?? []) };
    case "thread": {
      const t = event.thread;
      const list = state.archived[t.channel];
      return {
        ...state,
        threads: withThreads(state.threads, [t]),
        // A newly archived one is the newest there.
        ...(list && { archived: { ...state.archived, [t.channel]: { ...list, threads: [...(t.archived && !list.threads.some((x) => x.id === t.id) ? [t] : []), ...list.threads.map((x) => (x.id === t.id ? t : x))] } } }),
      };
    }
    case "thread-deleted": {
      const { [event.id]: _, ...threads } = state.threads;
      const { [event.id]: __, ...messages } = state.messages;
      const list = state.archived[event.channel];
      return {
        ...state,
        threads,
        messages,
        follows: new Set([...state.follows].filter((id) => id !== event.id)),
        ...(list && { archived: { ...state.archived, [event.channel]: { ...list, threads: list.threads.filter((t) => t.id !== event.id) } } }),
      };
    }
    case "follows":
      return { ...state, follows: new Set(event.threads) };
    case "channel-list":
      return { ...state, channelList: event.list };
    case "archived-threads": {
      // Pages come in order: a page's threads go after those we have.
      const known = (state.archived[event.channel]?.threads ?? []).filter((t) => !event.threads.some((x) => x.id === t.id));
      return {
        ...state,
        threads: withThreads(state.threads, event.threads),
        archived: { ...state.archived, [event.channel]: { threads: [...known, ...event.threads], more: event.more } },
      };
    }
    case "thread-archive":
      return { ...state, threadArchive: event.seconds };
    case "no-thread":
      return { ...state, notice: { text: "You cannot open this message here", n: (state.notice?.n ?? 0) + 1 } };
    case "history":
      return {
        ...state,
        lastError: null,
        messages: { ...state.messages, [event.channel]: merge(state.messages[event.channel], event.messages) },
        threads: withThreads(state.threads, threadsIn(event.messages)),
        loaded: { ...state.loaded, [event.channel]: true },
        more: { ...state.more, [event.channel]: event.more },
      };
    case "message":
    case "message-edited": {
      const ch = event.message.channel;
      const replace = (ms: TextMessage[]) => ms.map((m) => (m.id === event.message.id ? keepMine(m, event.message) : m));
      const search = event.type === "message-edited" ? patchSearch(state.search, replace) : state.search;
      const pins = event.type === "message-edited" ? patchPins(state.pins, ch, replace) : state.pins;
      // An edit of a message not loaded here stays unloaded.
      if (event.type === "message-edited" && !state.messages[ch]?.some((m) => m.id === event.message.id)) return { ...state, search, pins };
      // An edit may change the link: its preview is asked again.
      const { [event.message.id]: _, ...previews } = state.previews;
      return {
        ...state,
        search,
        pins,
        previews,
        threads: withThreads(state.threads, threadsIn([event.message])),
        messages: { ...state.messages, [ch]: merge(state.messages[ch], [keepMine(state.messages[ch]?.find((m) => m.id === event.message.id), event.message)]) },
      };
    }
    case "message-pinned": {
      const m = event.message;
      const msgs = state.messages[m.channel];
      const list = state.pins[m.channel];
      return {
        ...state,
        ...(msgs?.some((x) => x.id === m.id) && { messages: { ...state.messages, [m.channel]: msgs.map((x) => (x.id === m.id ? keepMine(x, m) : x)) } }),
        ...(list && { pins: { ...state.pins, [m.channel]: withPin(list, keepMine(list.find((x) => x.id === m.id) ?? msgs?.find((x) => x.id === m.id), m)) } }),
      };
    }
    case "pins":
      return { ...state, pins: { ...state.pins, [event.channel]: event.messages } };
    case "search-results":
      return { ...state, search: { text: event.text, messages: event.messages }, threads: withThreads(state.threads, event.threads ?? []) };
    case "read":
      return { ...state, reads: { ...state.reads, [event.channel]: Math.max(state.reads[event.channel] ?? 0, event.message) } };
    case "preview":
      return { ...state, previews: { ...state.previews, [event.message]: event.preview } };
    case "reactions": {
      const msgs = state.messages[event.channel];
      const i = msgs?.findIndex((m) => m.id === event.message) ?? -1;
      if (!msgs || i < 0) return state;
      const next = msgs.slice();
      next[i] = { ...msgs[i]!, reactions: event.reactions };
      return { ...state, messages: { ...state.messages, [event.channel]: next } };
    }
    case "poll": {
      const patch = (ms: TextMessage[]) => ms.map((m) => (m.id === event.message ? withPoll(m, event.poll) : m));
      const msgs = state.messages[event.channel];
      return {
        ...state,
        ...(msgs && { messages: { ...state.messages, [event.channel]: patch(msgs) } }),
        pins: patchPins(state.pins, event.channel, patch),
        search: patchSearch(state.search, patch),
      };
    }
    case "messages-deleted": {
      const gone = new Set(event.ids);
      const drop = (ms: TextMessage[]) => ms.filter((m) => !gone.has(m.id));
      const search = patchSearch(state.search, drop);
      const pins = patchPins(state.pins, event.channel, drop);
      const msgs = state.messages[event.channel];
      if (!msgs) return { ...state, search, pins };
      return { ...state, search, pins, messages: { ...state.messages, [event.channel]: drop(msgs) } };
    }
    case "error":
      return event.about === "manage"
        ? { ...state, notice: { text: event.message, n: (state.notice?.n ?? 0) + 1 } }
        : {
            ...state,
            lastError: event.message,
            quietError: event.about === "rate",
            ...(event.slow && { slowUntil: { ...state.slowUntil, [event.slow.channel]: event.slow.until } }),
            ...(event.returned && { returned: { ...event.returned, n: (state.returned?.n ?? 0) + 1 } }),
          };
    case "upload-token": // answered by the connection, not state
    case "typing": // short-lived: the client keeps it (lib/typing)
      return state;
    case "resync":
      return { ...state, messages: {}, loaded: {}, more: {}, pins: {}, resyncs: state.resyncs + 1 };
  }
};

// A user left: its status goes (Murmur reuses session numbers).
export const forgetStatus = (state: TextState, session: number): TextState => {
  if (!state.statuses[session]) return state;
  const { [session]: _, ...statuses } = state.statuses;
  return { ...state, statuses };
};

// A request for older messages (before: the oldest id shown) is answered
// once older messages are here or the channel has no more; a resync that
// drops the history ends it too.
export const pageAnswered = (state: TextState, page: { channel: number; before: number }) => {
  const oldest = state.messages[page.channel]?.[0]?.id;
  return oldest === undefined || oldest < page.before || state.more[page.channel] === false;
};

export const historyRequest = (channel: number, before?: number) =>
  JSON.stringify(before === undefined ? { type: "history", channel } : { type: "history", channel, before });

// nonce: our id for the send, the same on a retry (the server stores the
// message once and confirms it with the nonce, see sendAnswer).
// sticker: a sticker message (no text, no upload). One file without a
// description goes as "upload", which older servers know too.
export type OutgoingFile = { key: string; description?: string };
const oldForm = (uploads: readonly OutgoingFile[]) => uploads.length === 1 && !uploads[0]!.description;
export const sendRequest = (
  channel: number,
  text: string,
  uploads: readonly OutgoingFile[] = [],
  reply?: number,
  noPing?: boolean,
  nonce?: string,
  sticker?: number,
  poll?: PollBody,
) =>
  JSON.stringify({
    type: "send",
    channel,
    text,
    ...(oldForm(uploads) ? { upload: uploads[0]!.key } : uploads.length ? { uploads } : {}),
    ...(reply ? { reply } : {}),
    ...(reply && noPing ? { noPing } : {}),
    ...(nonce ? { nonce } : {}),
    ...(sticker ? { sticker } : {}),
    ...(poll ? { poll } : {}),
  });

// The server's answer to a send with this nonce: our message with it
// confirms it (from servers without nonces: the message with our upload,
// its key is in the file's URL), an error with it refuses it; null: not an
// answer (errors of other requests; servers without nonces: it times out).
export const sendAnswer = (ev: ServerEvent, nonce: string, upload?: string): { error?: string } | null => {
  if (ev.type === "error") return ev.nonce === nonce ? { error: ev.message } : null;
  if (ev.type !== "message") return null;
  const ours =
    ev.nonce === undefined ? !!upload && messageFiles(ev.message).some((f) => f.url.startsWith(`/files/${upload}/`)) : ev.nonce === nonce;
  return ours ? {} : null;
};

// count: the tokens for the files of one message, in one request.
export const uploadTokenRequest = (count = 1) => JSON.stringify(count > 1 ? { type: "upload-token", count } : { type: "upload-token" });
// Custom emoji (admins and moderators; the server checks): a token to
// upload one, and deleting one.
export const emojiTokenRequest = (name: string) => JSON.stringify({ type: "emoji-token", name });
// The server's logo (admins): a token for one upload.
export const logoTokenRequest = () => JSON.stringify({ type: "logo-token" });
export const webhookPictureTokenRequest = (webhook: number) => JSON.stringify({ type: "webhook-picture-token", webhook });
export const deleteEmojiRequest = (name: string) => JSON.stringify({ type: "delete-emoji", name });
// Personal emoji (registered users): a token to upload a new one, one
// from a message (":name:" or "<:name:id>") under our name, rename and
// delete; moderators remove an image everywhere.
export const userEmojiTokenRequest = (name: string) => JSON.stringify({ type: "user-emoji-token", name });
export const addUserEmojiRequest = (emoji: string, name: string) => JSON.stringify({ type: "add-user-emoji", emoji, name });
export const renameUserEmojiRequest = (name: string, to: string) => JSON.stringify({ type: "rename-user-emoji", name, to });
export const deleteUserEmojiRequest = (name: string) => JSON.stringify({ type: "delete-user-emoji", name });
export const removeEmojiImageRequest = (emoji: string) => JSON.stringify({ type: "remove-emoji-image", emoji });

export const voiceChatRequest = (voice: number) => JSON.stringify({ type: "voice-chat", voice });

// Text channel management (admins; the server checks).
export type ManageAction =
  | { create: string; private?: boolean; category?: number }
  // Categories (staff); a text channel into a category (0: none).
  | { createCategory: string; private?: boolean }
  | { renameCategory: number; name: string }
  | { removeCategory: number }
  | { orderCategories: number[] }
  | { setCategory: number; category: number }
  // A private voice channel below parent (root if not given).
  | { createPrivateVoice: string; parent?: number }
  // A Mumble channel (see TextState.private) private with these members,
  // or public.
  | { setPrivate: number; on: boolean; users: number[] }
  // Invite links of a private Mumble channel (see lib/invites): list,
  // make (seconds 0: for ever; uses 0: unlimited), delete, redeem.
  | { invites: number }
  | { createInvite: number; seconds: number; uses: number }
  | { deleteInvite: number }
  | { redeemInvite: string }
  // A text channel's webhooks (staff, see lib/webhooks): list, make,
  // rename, delete.
  | { webhooks: number }
  | { createWebhook: number; name: string }
  | { renameWebhook: number; name: string }
  | { deleteWebhook: number }
  | { removeWebhookPicture: number }
  | { rename: number; name: string }
  | { remove: number }
  | { order: number[] }
  | { moderator: number; on: boolean }
  | { admin: number; on: boolean }
  // A registered user a moderator of a Mumble channel, or not (staff).
  | { channelModerator: number; channel: number; on: boolean }
  | { listenOnly: number; on: boolean; users: number[] }
  | { topic: number; text: string }
  | { slowMode: number; seconds: number }
  // Moderation, by voice server session: a ban for some seconds (0: for
  // ever), a time-out (0: end it).
  | { ban: number; reason: string; seconds: number }
  | { timeout: number; seconds: number }
  // A time-out in a Mumble channel (staff and its moderators).
  | { channelTimeout: number; channel: number; seconds: number }
  // A user's messages of the last seconds (staff): see UserTarget.
  | { deleteMessages: UserTarget; seconds: number }
  | { guestsReadOnly: boolean }
  | { captcha: CaptchaLevel }
  | { inviteOnly: boolean }
  | { registrationInvites: true }
  | { createRegistrationInvite: true; seconds: number; uses: number }
  | { deleteRegistrationInvite: number }
  | { serverName: string }
  | { removeLogo: true }
  // Every connection (admins).
  | { connections: true }
  // The message rules (admins): ask for them, or save new ones.
  | { messageRules: true }
  | { setMessageRules: MessageRules }
  // Which other servers' accounts log in (admins): ask, or save new lists.
  | { homeServers: true }
  | { setHomeServers: HomeServers }
  // Recording a voice channel (staff): start or stop; the finished ones.
  | { record: number; on: boolean }
  | { recordings: true };

// A user for the Kallisti server: by voice server session if online, else
// a registered user by id.
export type UserTarget = { session: number } | { user: number };

const manageBody = (a: ManageAction) => {
  if ("create" in a) return { type: "create-channel", name: a.create, ...(a.private ? { private: true } : {}), ...(a.category ? { category: a.category } : {}) };
  if ("createCategory" in a) return { type: "create-category", name: a.createCategory, ...(a.private && { private: true }) };
  if ("renameCategory" in a) return { type: "rename-category", category: a.renameCategory, name: a.name };
  if ("removeCategory" in a) return { type: "delete-category", category: a.removeCategory };
  if ("orderCategories" in a) return { type: "order-categories", categories: a.orderCategories };
  if ("setCategory" in a) return { type: "set-category", channel: a.setCategory, category: a.category };
  if ("createPrivateVoice" in a) return { type: "create-private-voice", name: a.createPrivateVoice, ...(a.parent ? { voice: a.parent } : {}) };
  if ("setPrivate" in a) return { type: "set-private", voice: a.setPrivate, on: a.on, users: a.users };
  if ("invites" in a) return { type: "invites", voice: a.invites };
  if ("createInvite" in a) return { type: "create-invite", voice: a.createInvite, seconds: a.seconds, uses: a.uses };
  if ("deleteInvite" in a) return { type: "delete-invite", invite: a.deleteInvite };
  if ("redeemInvite" in a) return { type: "redeem-invite", token: a.redeemInvite };
  if ("webhooks" in a) return { type: "webhooks", channel: a.webhooks };
  if ("createWebhook" in a) return { type: "create-webhook", channel: a.createWebhook, name: a.name };
  if ("renameWebhook" in a) return { type: "rename-webhook", webhook: a.renameWebhook, name: a.name };
  if ("deleteWebhook" in a) return { type: "delete-webhook", webhook: a.deleteWebhook };
  if ("removeWebhookPicture" in a) return { type: "remove-webhook-picture", webhook: a.removeWebhookPicture };
  if ("rename" in a) return { type: "rename-channel", channel: a.rename, name: a.name };
  if ("remove" in a) return { type: "delete-channel", channel: a.remove };
  if ("moderator" in a) return { type: "set-moderator", user: a.moderator, moderator: a.on };
  if ("admin" in a) return { type: "set-admin", user: a.admin, admin: a.on };
  if ("channelModerator" in a) return { type: "set-channel-moderator", user: a.channelModerator, voice: a.channel, on: a.on };
  if ("listenOnly" in a) return { type: "set-listen-only", voice: a.listenOnly, on: a.on, users: a.users };
  if ("topic" in a) return { type: "set-topic", channel: a.topic, text: a.text };
  if ("slowMode" in a) return { type: "set-slow-mode", channel: a.slowMode, seconds: a.seconds };
  if ("ban" in a) return { type: "ban", session: a.ban, text: a.reason, seconds: a.seconds };
  if ("timeout" in a) return { type: "time-out", session: a.timeout, seconds: a.seconds };
  if ("channelTimeout" in a) return { type: "channel-time-out", session: a.channelTimeout, voice: a.channel, seconds: a.seconds };
  if ("deleteMessages" in a) return { type: "delete-messages", ...a.deleteMessages, seconds: a.seconds };
  if ("guestsReadOnly" in a) return { type: "set-guests-read-only", on: a.guestsReadOnly };
  if ("inviteOnly" in a) return { type: "set-invite-only", on: a.inviteOnly };
  if ("registrationInvites" in a) return { type: "registration-invites" };
  if ("createRegistrationInvite" in a) return { type: "create-registration-invite", seconds: a.seconds, uses: a.uses };
  if ("deleteRegistrationInvite" in a) return { type: "delete-registration-invite", invite: a.deleteRegistrationInvite };
  if ("captcha" in a) return { type: "set-captcha", level: a.captcha };
  if ("serverName" in a) return { type: "set-server-name", name: a.serverName };
  if ("removeLogo" in a) return { type: "remove-logo" };
  if ("connections" in a) return { type: "connections" };
  if ("messageRules" in a) return { type: "message-rules" };
  if ("setMessageRules" in a) return { type: "set-message-rules", rules: a.setMessageRules };
  if ("homeServers" in a) return { type: "home-servers" };
  if ("setHomeServers" in a) return { type: "set-home-servers", homes: a.setHomeServers };
  if ("record" in a) return { type: a.on ? "start-recording" : "stop-recording", voice: a.record };
  if ("recordings" in a) return { type: "recordings" };
  return { type: "order-channels", channels: a.order };
};

export const manageRequest = (a: ManageAction) => JSON.stringify(manageBody(a));

export const searchRequest = (q: SearchQuery) => JSON.stringify({ type: "search", ...q });
export const directRequest = (user: number) => JSON.stringify({ type: "dm", user });

// The direct chat with a user, if known.
export const directChatWith = (directs: TextState["directs"], user: number): number | null => {
  const found = Object.entries(directs).find(([, p]) => p.id === user);
  return found ? Number(found[0]) : null;
};
export const pinRequest = (message: number, on: boolean) => JSON.stringify({ type: "pin", message, on });
export const pinsRequest = (channel: number) => JSON.stringify({ type: "pins", channel });
export const typingRequest = (channel: number) => JSON.stringify({ type: "typing", channel });
// After a resync: the server sends the whole state again (a ready).
export const resyncRequest = JSON.stringify({ type: "resync" });
export const markReadRequest = (channel: number, message: number) =>
  JSON.stringify({ type: "mark-read", channel, message });
// remove: the keys of files to take off (as in Discord, none can be added).
export const editMessageRequest = (message: number, text: string, remove: readonly string[] = []) =>
  JSON.stringify(remove.length ? { type: "edit-message", message, text, remove } : { type: "edit-message", message, text });
export const deleteMessageRequest = (message: number) => JSON.stringify({ type: "delete-message", message });

// The server's HTTP origin, from the bridge WebSocket URL.
export const httpBase = (wsUrl: string) => {
  const u = new URL(wsUrl);
  u.protocol = u.protocol === "wss:" ? "https:" : "http:";
  return u.origin;
};

export const formatSize = (bytes: number) => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
};

export { linkify } from "./markdown";

// Cached per message object: the reducer keeps message objects, so the chat
// list gets stable objects and does not re-render every line.
// An attachment URL from the server must stay on the server, under /files/.
// Anything else is dropped: an image would load by itself (and leak the
// reader's IP to another host).
const fileUrl = (url: string, base: string): string | null => {
  try {
    const u = new URL(url, base);
    return u.origin === new URL(base).origin && u.pathname.startsWith("/files/") ? u.href : null;
  } catch {
    return null;
  }
};

// An attachment with absolute URLs on our server (null: it is elsewhere; a
// preview elsewhere is dropped, the attachment stays).
const ownAttachment = ({ preview, ...u }: Attachment, base: string): Attachment | null => {
  const url = fileUrl(u.url, base);
  const own = preview && fileUrl(preview, base);
  return url ? { ...u, url, ...(own && { preview: own }) } : null;
};

// The key of one of our files, from its URL (null: not one).
export const fileKey = (url: string) => /\/files\/([^/?#]+)\//.exec(url)?.[1] ?? null;

// A message's files: the list, or the one file of an older server.
export const messageFiles = (m: TextMessage): Attachment[] => m.uploads ?? (m.upload ? [m.upload] : []);

const chatCache =new WeakMap<TextMessage, { chat: DisplayMessage; channels?: ReadonlyMap<string, number> }>();

// Text channel messages are plain text: escape, then Markdown. Attachment
// URLs are relative to the server (`base`); channels: text channel ids by
// name, for "#name" links (a new map makes the line again).
export const toChatMessage = (m: TextMessage, base = "", channels?: ReadonlyMap<string, number>): DisplayMessage => {
  const cached = chatCache.get(m);
  let chat = cached?.channels === channels ? cached?.chat : undefined;
  if (!chat) {
    const uploads = messageFiles(m).flatMap((u) => ownAttachment(u, base) ?? []);
    chat = {
      actor: null,
      actorName: m.author,
      channelIds: [],
      treeIds: [],
      sessions: [],
      text: markdown(escapeHtml(m.text), channels),
      at: m.at,
      peer: null,
      verified: m.verified === true,
      ...(m.authorId !== undefined && { authorId: m.authorId }),
      source: { id: m.id, text: m.text },
      ...(m.edited !== undefined && { edited: m.edited }),
      ...(m.pinned !== undefined && { pinned: m.pinned }),
      ...(m.reply && { reply: m.reply }),
      ...(m.reactions?.length && { reactions: m.reactions }),
      ...(m.mentionAll && { mentionAll: m.mentionAll }),
      ...(m.noPing && { noPing: true }),
      ...(m.webhook && { webhook: m.webhook }),
      ...(uploads.length && { uploads: uploads.map(spoilerUpload) }),
      ...(m.sticker && { sticker: m.sticker }),
      ...(m.poll && { poll: m.poll }),
    };
    chatCache.set(m, { chat, channels });
  }
  return chat;
};

// The open chats whose new messages count as seen, with their newest
// message: only in a visible tab. A hidden one would mark them read for
// the whole account while the user reads elsewhere.
export const autoSeen = (state: TextState, open: readonly number[], read: Record<number, number>, visible: boolean): [number, number][] =>
  visible
    ? open.flatMap((id): [number, number][] => {
        const newest = state.messages[id]?.at(-1)?.id ?? 0;
        return newest > (read[id] ?? 0) ? [[id, newest]] : [];
      })
    : [];

// Channels (and active threads we follow) with a message newer than the
// newest one seen there (loaded, or known from the server's summary).
export const unreadTextIds = (state: TextState, seen: Record<number, number>): Set<number> =>
  new Set(
    [...state.channels.map((c) => c.id), ...followedThreads(state.follows, state.threads).filter((id) => !state.threads[id]!.archived)].filter(
      (id) => Math.max(state.messages[id]?.at(-1)?.id ?? 0, state.summary[id]?.latest ?? 0) > (seen[id] ?? 0),
    ),
  );
