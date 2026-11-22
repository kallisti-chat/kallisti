import { isFieldSet } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import * as pb from "../src/gen/Mumble_pb";
import { decodeFrames } from "../src/framing";
import { decodeMessage } from "../src/messages";
import {
  createChannel,
  editChannel,
  escapeHtml,
  joinChannel,
  moderateUser,
  setVoiceTarget,
  removeUser,
  queryPermissions,
  queryBans,
  queryRegistered,
  registerSelf,
  renameRegistered,
  setBans,
  unregister,
  removeChannel,
  requestDescriptions,
  requestTextures,
  sendText,
  setSelfState,
  setSelfComment,
  setSelfTexture,
  textMessageInit,
  textToHtml,
  requestComments,
} from "../src/actions";

const decodeOne = (bytes: Uint8Array) => {
  const { frames } = decodeFrames(bytes);
  expect(frames).toHaveLength(1);
  return decodeMessage(frames[0]!);
};

describe("escapeHtml", () => {
  it("escapes characters that have meaning in HTML", () => {
    expect(escapeHtml(`<b>"Tom" & 'Jerry'</b>`)).toBe(
      "&lt;b&gt;&quot;Tom&quot; &amp; &#39;Jerry&#39;&lt;/b&gt;",
    );
  });
});

describe("sendText", () => {
  it("sends escaped text to one channel", () => {
    const msg = decodeOne(sendText({ channelId: 4 }, "a < b"));
    if (msg.type !== "TextMessage") throw new Error();
    expect(msg.message.channelId).toEqual([4]);
    expect(msg.message.session).toEqual([]);
    expect(msg.message.message).toBe("a &lt; b");
  });

  it("sends a private message to one user", () => {
    const msg = decodeOne(sendText({ session: 9 }, "psst"));
    if (msg.type !== "TextMessage") throw new Error();
    expect(msg.message.session).toEqual([9]);
    expect(msg.message.channelId).toEqual([]);
  });
});

describe("joinChannel", () => {
  it("moves the own session to a channel", () => {
    const msg = decodeOne(joinChannel(9, 0));
    if (msg.type !== "UserState") throw new Error();
    expect(msg.message).toMatchObject({ session: 9, channelId: 0 });
  });
});

describe("setSelfState", () => {
  it("sends self mute and self deaf", () => {
    const msg = decodeOne(setSelfState(9, { selfMute: true, selfDeaf: false }));
    if (msg.type !== "UserState") throw new Error();
    expect(msg.message).toMatchObject({ session: 9, selfMute: true, selfDeaf: false });
  });
});

describe("requestDescriptions", () => {
  it("asks for channel description blobs", () => {
    const msg = decodeOne(requestDescriptions([0, 4]));
    if (msg.type !== "RequestBlob") throw new Error();
    expect(msg.message.channelDescription).toEqual([0, 4]);
  });
});

describe("registerSelf", () => {
  it("asks Murmur to register the own session", () => {
    const msg = decodeOne(registerSelf(12));
    if (msg.type !== "UserState") throw new Error();
    expect(msg.message).toMatchObject({ session: 12, userId: 0 });
  });
});

describe("channel management", () => {
  it("creates a channel below a parent", () => {
    const msg = decodeOne(createChannel(0, "Games", { temporary: true }));
    if (msg.type !== "ChannelState") throw new Error();
    expect(msg.message.parent).toBe(0);
    expect(msg.message.name).toBe("Games");
    expect(msg.message.temporary).toBe(true);
    expect(msg.message.channelId).toBe(0); // not set: a new channel
  });

  it("edits only the given fields", () => {
    const msg = decodeOne(editChannel(5, { name: "Lounge", position: 2 }));
    if (msg.type !== "ChannelState") throw new Error();
    expect(msg.message.channelId).toBe(5);
    expect(msg.message.name).toBe("Lounge");
    expect(msg.message.position).toBe(2);
    expect(msg.message.description).toBe("");
  });

  it("removes a channel and asks for permissions", () => {
    const rm = decodeOne(removeChannel(5));
    expect(rm.type === "ChannelRemove" && rm.message.channelId).toBe(5);
    const q = decodeOne(queryPermissions(5));
    expect(q.type === "PermissionQuery" && q.message.channelId).toBe(5);
  });
});

describe("textures", () => {
  it("asks for avatar blobs and sets our own", () => {
    const ask = decodeOne(requestTextures([3, 4]));
    if (ask.type !== "RequestBlob") throw new Error();
    expect(ask.message.sessionTexture).toEqual([3, 4]);
    const set = decodeOne(setSelfTexture(7, new Uint8Array([1, 2])));
    if (set.type !== "UserState") throw new Error();
    expect(set.message.session).toBe(7);
    expect([...set.message.texture]).toEqual([1, 2]);
  });
});

describe("ban list and registered users", () => {
  it("queries and changes them", () => {
    const q = decodeOne(queryBans());
    if (q.type !== "BanList") throw new Error();
    expect(q.message.query).toBe(true);
    const ban = { address: new Uint8Array(16), mask: 128, name: "x", hash: "h", reason: "r", start: "", duration: 0 };
    const set = decodeOne(setBans([ban]));
    if (set.type !== "BanList") throw new Error();
    expect(set.message.bans).toHaveLength(1);
    expect(set.message.query).toBe(false);
    const users = decodeOne(queryRegistered());
    if (users.type !== "UserList") throw new Error();
    expect(users.message.users).toHaveLength(0);
    const rename = decodeOne(renameRegistered(4, "bob"));
    if (rename.type !== "UserList") throw new Error();
    expect(rename.message.users[0]).toMatchObject({ userId: 4, name: "bob" });
    const gone = decodeOne(unregister(4));
    if (gone.type !== "UserList") throw new Error();
    expect(gone.message.users[0]?.userId).toBe(4);
    expect(isFieldSet(gone.message.users[0]!, pb.UserList_UserSchema.field.name)).toBe(false);
  });
});

describe("moderation", () => {
  it("kicks and bans with a reason", () => {
    const kick = decodeOne(removeUser(5, "spam"));
    if (kick.type !== "UserRemove") throw new Error();
    expect(kick.message).toMatchObject({ session: 5, reason: "spam" });
    expect(isFieldSet(kick.message, pb.UserRemoveSchema.field.ban)).toBe(false);
    const ban = decodeOne(removeUser(5, "", { ban: true }));
    if (ban.type !== "UserRemove") throw new Error();
    expect(ban.message.ban).toBe(true);
  });

  it("server-mutes and deafens only what is given", () => {
    const mute = decodeOne(moderateUser(5, { mute: false }));
    if (mute.type !== "UserState") throw new Error();
    expect(mute.message.session).toBe(5);
    expect(isFieldSet(mute.message, pb.UserStateSchema.field.mute)).toBe(true);
    expect(isFieldSet(mute.message, pb.UserStateSchema.field.deaf)).toBe(false);
    const deaf = decodeOne(moderateUser(5, { deaf: true }));
    if (deaf.type !== "UserState") throw new Error();
    expect(deaf.message.deaf).toBe(true);
    expect(isFieldSet(deaf.message, pb.UserStateSchema.field.mute)).toBe(false);
    const priority = decodeOne(moderateUser(5, { prioritySpeaker: true }));
    if (priority.type !== "UserState") throw new Error();
    expect(priority.message.prioritySpeaker).toBe(true);
    expect(isFieldSet(priority.message, pb.UserStateSchema.field.mute)).toBe(false);
  });

  it("sets a voice target: users, a channel, or none (which removes it)", () => {
    const users = decodeOne(setVoiceTarget(1, { sessions: [4, 7] }));
    if (users.type !== "VoiceTarget") throw new Error();
    expect(users.message.id).toBe(1);
    expect(users.message.targets.map((t) => t.session)).toEqual([[4, 7]]);
    const channel = decodeOne(setVoiceTarget(1, { channel: 9 }));
    if (channel.type !== "VoiceTarget") throw new Error();
    expect(channel.message.targets[0]?.channelId).toBe(9);
    const none = decodeOne(setVoiceTarget(1, null));
    if (none.type !== "VoiceTarget") throw new Error();
    expect(none.message.targets).toEqual([]);
  });
});

describe("textMessageInit", () => {
  it("sends line breaks as <br> (Mumble text is HTML)", () => {
    expect(textMessageInit({ session: 3 }, "a < b\nc").message).toBe("a &lt; b<br>c");
  });
});

describe("comments", () => {
  it("sends plain text as HTML: escaped, line breaks as <br>", () => {
    expect(textToHtml("a < b & 'c'\nd")).toBe("a &lt; b &amp; &#39;c&#39;<br>d");
  });

  it("sets our comment and asks for long ones", () => {
    const set = decodeOne(setSelfComment(7, "hi\nthere"));
    if (set.type !== "UserState") throw new Error();
    expect(set.message).toMatchObject({ session: 7, comment: "hi<br>there" });
    const ask = decodeOne(requestComments([3]));
    if (ask.type !== "RequestBlob") throw new Error();
    expect(ask.message.sessionComment).toEqual([3]);
  });
});
