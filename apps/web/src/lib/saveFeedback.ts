import type { TextCategory } from "./categories";
import type { ManageAction, ServerEvent, TextChannel } from "./textChannels";

// Feedback on a control that saves or applies something on the server:
// pending while it works, saved for a moment once it confirms, or its
// error (a refusal, or no answer in time). Settings stored on the device
// apply at once and need none.
export type Save = { phase: "idle" | "pending" | "saved" } | { phase: "error"; message: string };
export const idle: Save = { phase: "idle" };
export const pending: Save = { phase: "pending" };
// How long "Saved" shows, and how long a request waits for its answer.
export const SAVED_MS = 2_000;
export const ANSWER_MS = 5_000;

// An answer counts only while the control waits for one.
export const settle = (s: Save, error?: string): Save =>
  s.phase !== "pending" ? s : error === undefined ? { phase: "saved" } : { phase: "error", message: error };
export const fade = (s: Save): Save => (s.phase === "saved" ? idle : s);
export const busyLabel = (s: Save, label: string, busy = "Saving…") => (s.phase === "pending" ? busy : label);

// Requests that wait for the server's answer, oldest first. The server
// answers in order, so a refusal (a manage error, a Mumble
// PermissionDenied) goes to the oldest of its kind, and an event confirms
// the oldest it matches (one "channels" event is not the answer to three
// changes). Mumble checks look at the state: all that hold are done.
export type Via = "text" | "mumble";
export const answer = <W extends { via: Via }>(waiters: readonly W[], via: Via, refused: boolean, matches: (w: W) => boolean) => {
  const mine = waiters.filter((w) => w.via === via);
  const no = refused ? mine[0] : undefined;
  const matching = mine.filter((w) => w !== no && matches(w));
  const confirmed = via === "text" ? matching.slice(0, 1) : matching;
  return { refused: no, confirmed, rest: waiters.filter((w) => w !== no && !confirmed.includes(w)) };
};

// Names as the server writes them (see textChannelName, categoryName).
const channelName = (name: string) => name.trim().split(/\s+/).join("-").toLowerCase();
// Go lowers a final sigma differently: compared without case, accents kept.
const sameChannel = (stored: string | undefined, asked: string) => stored?.toUpperCase() === channelName(asked).toUpperCase();
const categoryName = (name: string) => name.trim().split(/\s+/).join(" ");

// ids in this order among the listed ones.
const inOrder = (listed: readonly { id: number }[], ids: readonly number[]) => {
  const got = listed.map((x) => x.id).filter((id) => ids.includes(id));
  return got.length === ids.length && got.every((id, i) => id === ids[i]);
};

// A channel list confirms a change only with the change in it: other
// changes send lists too (a privacy change in the same save does).
type Lists = { channels: TextChannel[]; categories: TextCategory[] };
const channelCheck = (a: ManageAction): ((l: Lists) => boolean) | null => {
  const channel = (l: Lists, id: number) => l.channels.find((c) => c.id === id);
  const category = (l: Lists, id: number) => l.categories.find((c) => c.id === id);
  if ("create" in a) return (l) => l.channels.some((c) => sameChannel(c.name, a.create));
  if ("rename" in a) return (l) => sameChannel(channel(l, a.rename)?.name, a.name);
  if ("remove" in a) return (l) => !channel(l, a.remove);
  if ("order" in a) return (l) => inOrder(l.channels, a.order);
  if ("topic" in a) return (l) => (channel(l, a.topic)?.topic ?? "") === a.text.trim();
  if ("slowMode" in a) return (l) => (channel(l, a.slowMode)?.slowMode ?? 0) === a.seconds;
  if ("setCategory" in a) return (l) => !!channel(l, a.setCategory) && (channel(l, a.setCategory)?.category ?? 0) === a.category;
  if ("createCategory" in a) return (l) => l.categories.some((c) => c.name === categoryName(a.createCategory));
  if ("renameCategory" in a) return (l) => category(l, a.renameCategory)?.name === categoryName(a.name);
  if ("removeCategory" in a) return (l) => !category(l, a.removeCategory);
  if ("orderCategories" in a) return (l) => inOrder(l.categories, a.orderCategories);
  return null;
};

// The event that confirms a management request; null: the server sends
// none of its own (a ban ends the connection, deleting no messages says
// nothing).
export const confirmsManage = (a: ManageAction): ((ev: ServerEvent) => boolean) | null => {
  const is = (type: ServerEvent["type"]) => (ev: ServerEvent) => ev.type === type;
  if ("guestsReadOnly" in a) return (ev) => ev.type === "guests-read-only" && ev.on === a.guestsReadOnly;
  if ("inviteOnly" in a) return (ev) => ev.type === "invite-only" && ev.on === a.inviteOnly;
  if ("registrationInvites" in a || "deleteRegistrationInvite" in a) return is("registration-invites");
  if ("createRegistrationInvite" in a) return is("registration-invite-created");
  if ("captcha" in a) return (ev) => ev.type === "captcha" && ev.level === a.captcha;
  if ("serverName" in a) return (ev) => ev.type === "server-name" && ev.name === a.serverName.trim();
  if ("removeLogo" in a) return (ev) => ev.type === "logo" && ev.version === "";
  if ("setMessageRules" in a || "messageRules" in a) return is("message-rules");
  if ("setHomeServers" in a || "homeServers" in a) return is("home-servers");
  if ("setPrivate" in a) return (ev) => ev.type === "private" && ev.channels.some((c) => c.channel === a.setPrivate) === a.on;
  if ("createPrivateVoice" in a) return is("private");
  if ("invites" in a || "deleteInvite" in a) return is("invites");
  if ("createInvite" in a) return is("invite-created");
  if ("webhooks" in a || "renameWebhook" in a || "deleteWebhook" in a || "removeWebhookPicture" in a) return is("webhooks");
  if ("createWebhook" in a) return is("webhook-created");
  if ("redeemInvite" in a) return is("invite-joined");
  if ("moderator" in a || "admin" in a || "channelModerator" in a) return is("roles");
  if ("listenOnly" in a) return is("listen-only");
  if ("timeout" in a || "channelTimeout" in a) return is("timeouts");
  if ("connections" in a) return is("connections");
  if ("recordings" in a) return is("recordings");
  if ("record" in a) return is("recording");
  const check = channelCheck(a);
  if (check) return (ev) => ev.type === "channels" && check({ channels: ev.channels, categories: ev.categories ?? [] });
  return null;
};
