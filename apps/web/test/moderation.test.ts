import { describe, expect, it } from "vitest";
import { emptyUser, initialState, joinChannel, moderateUser, Perm, removeUser, type Channel, type ServerState, type User } from "@kallisti/protocol";
import { mayDeleteMessages, moderationRequest, registeredActions, moderationRequests, moderationRights, permissionsToAsk, timedOutUntil, userModeration } from "../src/lib/moderation";
import { initialTextState, manageRequest, type TextState } from "../src/lib/textChannels";

const root = (perms: number): ServerState => ({ ...initialState, rootPermissions: perms });

describe("moderationRights", () => {
  it("follows the root permissions", () => {
    expect(moderationRights(root(0))).toEqual({ any: false, bans: false, editBans: false, users: false, deleteAccounts: false });
    expect(moderationRights(root(Perm.Ban | Perm.Register))).toEqual({ any: true, bans: true, editBans: false, users: true, deleteAccounts: false });
    expect(moderationRights(root(Perm.Ban | Perm.Register | Perm.Write))).toEqual({
      any: true,
      bans: true,
      editBans: true,
      users: true,
      deleteAccounts: true,
    });
  });
});

const ch = (id: number, parent: number | null, position = 0): Channel => ({
  id, parent, name: `c${id}`, description: "", descriptionHash: null, position, temporary: false,
});
const user = (session: number, userId: number | null, channelId = 1): User => ({ ...emptyUser(session), name: `u${session}`, userId, channelId });
// Root is a lobby (no Speak); channel 4 mirrors a text channel.
const MOD = Perm.Kick | Perm.Ban | Perm.MuteDeafen | Perm.Move | Perm.Enter;
const server = (rootPerms: number, perms: Record<number, number> = {}): ServerState => ({
  ...initialState,
  session: 1,
  rootPermissions: rootPerms,
  channelPermissions: perms,
  channels: Object.fromEntries([ch(0, null), ch(1, 0, 1), ch(2, 0, 0), ch(3, 2), ch(4, 0, 2)].map((c) => [c.id, c])),
  users: { 1: user(1, 5), 2: user(2, null), 3: user(3, 6), 4: user(4, 0) },
});
const text: TextState = {
  ...initialTextState,
  voiceChats: true,
  channels: [{ id: 10, name: "general", mumble: 4 }],
  roles: { admins: [], moderators: [5, 6] },
};
const all = { 1: MOD, 2: MOD, 3: MOD, 4: MOD };

describe("userModeration", () => {
  it("offers what the permissions allow", () => {
    const m = userModeration(server(MOD, all), text, user(2, null));
    expect(m).toMatchObject({ kick: true, ban: true, timedBans: true, mute: true, timeout: true });
    // Voice channels in tree order, without the user's own, mirrors and the lobby.
    expect(m?.moveTo.map((c) => c.id)).toEqual([2, 3]);
  });

  it("needs the rights in the right channel", () => {
    expect(userModeration(server(Perm.Kick, {}), text, user(2, null))).toEqual({
      kick: true, ban: false, timedBans: false, mute: false, moveTo: [], disconnect: false, timeout: false, channelTimeout: null,
      deleteMessages: false,
    });
    // Move into a channel: Move or Enter there.
    const m = userModeration(server(0, { 1: Perm.Move, 2: Perm.Enter }), text, user(2, null));
    expect(m?.moveTo.map((c) => c.id)).toEqual([2]);
    expect(userModeration(server(0, { 2: MOD, 3: MOD }), text, user(2, null))).toBeNull();
  });

  it("leaves out timed bans and time-outs without the built-in server", () => {
    const m = userModeration(server(MOD, all), { ...text, voiceChats: false }, user(2, null));
    expect(m).toMatchObject({ ban: true, timedBans: false, timeout: false });
  });

  it("offers nothing on oneself and SuperUser, and keeps staff for admins", () => {
    expect(userModeration(server(MOD, all), text, user(1, 5))).toBeNull();
    expect(userModeration(server(MOD, all), text, user(4, 0))).toBeNull();
    // A moderator on a moderator: Mumble allows mute and move, not kick, ban or time-out.
    expect(userModeration(server(MOD, all), text, user(3, 6))).toMatchObject({ kick: false, ban: false, mute: true, timeout: false });
    expect(userModeration(server(MOD | Perm.Write, all), text, user(3, 6))).toMatchObject({ kick: true, ban: true, timeout: true });
  });
});

describe("permissionsToAsk", () => {
  it("asks for the user's channel and voice channels not known yet", () => {
    expect(permissionsToAsk(server(MOD, { 2: MOD }), text, user(2, null))).toEqual([1, 3]);
    expect(permissionsToAsk(server(MOD, all), text, user(2, null))).toEqual([]);
  });
});

describe("timedOutUntil", () => {
  const timeouts = [{ user: 6, until: 5000 }, { session: 2, until: 7000 }];
  it("finds registered users by user id and guests by session", () => {
    expect(timedOutUntil(timeouts, user(3, 6))).toBe(5000);
    expect(timedOutUntil(timeouts, user(2, null))).toBe(7000);
    expect(timedOutUntil(timeouts, user(2, 9))).toBeNull(); // a registered user with a guest's old session
    expect(timedOutUntil(timeouts, user(8, null))).toBeNull();
  });
});

describe("moderationRequest", () => {
  it("uses Mumble messages where Mumble has them", () => {
    expect(moderationRequest(4, { kind: "kick", reason: "no" })).toEqual({ mumble: removeUser(4, "no") });
    expect(moderationRequest(4, { kind: "ban", reason: "no", seconds: 0 })).toEqual({ mumble: removeUser(4, "no", { ban: true }) });
    expect(moderationRequest(4, { kind: "mute", on: true })).toEqual({ mumble: moderateUser(4, { mute: true }) });
    expect(moderationRequest(4, { kind: "deaf", on: false })).toEqual({ mumble: moderateUser(4, { deaf: false }) });
    expect(moderationRequest(4, { kind: "move", channel: 2 })).toEqual({ mumble: joinChannel(4, 2) });
  });

  it("asks the Kallisti server for timed bans and time-outs", () => {
    expect(moderationRequest(4, { kind: "ban", reason: "no", seconds: 60 })).toEqual({ manage: { ban: 4, reason: "no", seconds: 60 } });
    expect(moderationRequest(4, { kind: "timeout", seconds: 0 })).toEqual({ manage: { timeout: 4, seconds: 0 } });
  });
});

describe("deleting a user's recent messages", () => {
  const STAFF = MOD | Perm.MakeChannel;
  it("is for staff, on staff only for admins, never on SuperUser", () => {
    expect(mayDeleteMessages(server(STAFF), text, null)).toBe(true);
    expect(mayDeleteMessages(server(STAFF), text, 9)).toBe(true); // an offline member
    expect(mayDeleteMessages(server(MOD), text, 9)).toBe(false);
    expect(mayDeleteMessages(server(STAFF), { ...text, voiceChats: false }, 9)).toBe(false);
    expect(mayDeleteMessages(server(STAFF), text, 6)).toBe(false);
    expect(mayDeleteMessages(server(STAFF | Perm.Write), text, 6)).toBe(true);
    expect(mayDeleteMessages(server(STAFF | Perm.Write), text, 0)).toBe(false);
    expect(userModeration(server(STAFF, all), text, user(2, null))?.deleteMessages).toBe(true);
    expect(userModeration(server(Perm.MakeChannel), text, user(2, null))).toMatchObject({ kick: false, deleteMessages: true });
  });

  it("goes before the action: a guest's messages are known by its connection", () => {
    expect(moderationRequests({ session: 4 }, { kind: "kick", reason: "spam", deleteSeconds: 3600 })).toEqual([
      { manage: { deleteMessages: { session: 4 }, seconds: 3600 } },
      { mumble: removeUser(4, "spam") },
    ]);
    expect(moderationRequests({ session: 4 }, { kind: "ban", reason: "", seconds: 60, deleteSeconds: 0 })).toEqual([
      { manage: { ban: 4, reason: "", seconds: 60 } },
    ]);
    expect(moderationRequests({ user: 9 }, { kind: "delete", seconds: 86400 })).toEqual([{ manage: { deleteMessages: { user: 9 }, seconds: 86400 } }]);
    expect(JSON.parse(manageRequest({ deleteMessages: { user: 9 }, seconds: 3600 }))).toEqual({ type: "delete-messages", user: 9, seconds: 3600 });
  });
});

describe("registeredActions", () => {
  it("deletes accounts (admins only) and unregisters the others", () => {
    expect(registeredActions(false, false)).toEqual({ rename: true, remove: "Unregister" });
    expect(registeredActions(true, true)).toEqual({ rename: false, remove: "Delete account" });
    // Moderators leave accounts alone; an account keeps its name.
    expect(registeredActions(true, false)).toEqual({ rename: false, remove: null });
    // Not known yet (the built-in server's answer): nothing to click.
    expect(registeredActions(null, true)).toEqual({ rename: false, remove: null });
  });
});
