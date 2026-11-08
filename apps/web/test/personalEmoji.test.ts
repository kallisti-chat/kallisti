// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { escapeHtml } from "@kallisti/protocol";
import { customEmojiSet, jumboEmoji, knownEmoji, parseRef, personalRef, refsAsNames } from "../src/lib/customEmoji";
import {
  composerEmoji,
  emojiActions,
  freeName,
  fromRefs,
  parsePersonalEmoji,
  parseRemovedEmoji,
  personalEntries,
  personalFormError,
  sameEmoji,
  toRefs,
  type PersonalEmoji,
} from "../src/lib/personalEmoji";
import { sanitize } from "../src/lib/sanitize";
import { markdown, previewText } from "../src/lib/markdown";
import { normalEmoji } from "../src/lib/emojiPicker";
import {
  addUserEmojiRequest,
  deleteUserEmojiRequest,
  initialTextState,
  parseServerEvent,
  reduceText,
  removeEmojiImageRequest,
  renameUserEmojiRequest,
  userEmojiTokenRequest,
} from "../src/lib/textChannels";

const mine: PersonalEmoji[] = [
  { name: "blob", id: 7 },
  { name: "party", id: 8 }, // a server emoji's name (added before the server's)
];
const set = customEmojiSet([{ name: "party", url: "/api/emoji/1" }], "https://chat.example", mine, [9]);

describe("personal emoji references", () => {
  it("are <:name:id>", () => {
    expect(personalRef("blob", 7)).toBe("<:blob:7>");
    expect(parseRef("<:blob:7>")).toEqual({ name: "blob", id: 7 });
    for (const bad of ["<:blob:0>", "<:blob:07>", "<:Blob:7>", "<:b:7>", ":blob:", "<:blob:7> ", "x<:blob:7>"]) expect(parseRef(bad)).toBeNull();
  });
  it("show as :name: in plain text", () => {
    expect(refsAsNames("hi <:blob:7> <b> <:x:1>")).toBe("hi :blob: <b> <:x:1>");
    expect(previewText("||secret|| <:blob:7>")).toBe("▒▒▒▒ :blob:");
  });
  it("stay as they are in the server's emoji form", () => {
    expect(normalEmoji("<:top10:3>")).toBe("<:top10:3>");
  });
  it("are the same reaction when the image is the same", () => {
    expect(sameEmoji("<:blob:7>", "<:other:7>")).toBe(true);
    expect(sameEmoji("<:blob:7>", "<:blob:8>")).toBe(false);
    expect(sameEmoji("👍", "👍")).toBe(true);
    expect(sameEmoji(":blob:", "<:blob:7>")).toBe(false);
  });
});

describe("personal emoji from the server", () => {
  it("parse our list and the removed images", () => {
    expect(parsePersonalEmoji(mine)).toEqual(mine);
    expect(parsePersonalEmoji([{ name: "X", id: 1 }, { name: "ok", id: "1" }, { name: "ok", id: 2 }])).toEqual([{ name: "ok", id: 2 }]);
    expect(parsePersonalEmoji("no")).toBeNull();
    expect(parseRemovedEmoji([3, "4", 5])).toEqual([3, 5]);
  });
  it("come with ready and as events", () => {
    const ready = parseServerEvent(JSON.stringify({ type: "ready", channels: [], myEmoji: mine, removedEmoji: [9] }));
    let state = reduceText(initialTextState, ready!);
    expect(state.myEmoji).toEqual(mine);
    expect(state.removedEmoji).toEqual([9]);
    state = reduceText(state, parseServerEvent(JSON.stringify({ type: "my-emoji", emoji: [mine[0]] }))!);
    expect(state.myEmoji).toEqual([mine[0]]);
    state = reduceText(state, parseServerEvent(JSON.stringify({ type: "removed-emoji", ids: [9, 7] }))!);
    expect(state.removedEmoji).toEqual([9, 7]);
  });
  it("have requests", () => {
    expect(JSON.parse(userEmojiTokenRequest("blob"))).toEqual({ type: "user-emoji-token", name: "blob" });
    expect(JSON.parse(addUserEmojiRequest("<:blob:7>", "mine"))).toEqual({ type: "add-user-emoji", emoji: "<:blob:7>", name: "mine" });
    expect(JSON.parse(renameUserEmojiRequest("blob", "blub"))).toEqual({ type: "rename-user-emoji", name: "blob", to: "blub" });
    expect(JSON.parse(deleteUserEmojiRequest("blob"))).toEqual({ type: "delete-user-emoji", name: "blob" });
    expect(JSON.parse(removeEmojiImageRequest("<:blob:7>"))).toEqual({ type: "remove-emoji-image", emoji: "<:blob:7>" });
  });
});

describe("personal emoji in messages", () => {
  const html = (text: string) => sanitize(markdown(escapeHtml(text)), false, set);
  const img = (name: string, src: string, emoji: string) =>
    `<img class="emoji custom" draggable="false" title=":${name}:" alt=":${name}:" src="${src}" data-emoji="${emoji}">`;

  it("are images for everyone, by image id", () => {
    expect(html("hi <:blob:7> <:other:12>")).toBe(
      `hi ${img("blob", "https://chat.example/api/emoji/personal/7", "<:blob:7>")} ${img("other", "https://chat.example/api/emoji/personal/12", "<:other:12>")}`,
    );
  });
  it("next to server emoji of the same name", () => {
    expect(html(":party:<:party:8>")).toBe(
      img("party", "https://chat.example/api/emoji/1", ":party:") + img("party", "https://chat.example/api/emoji/personal/8", "<:party:8>"),
    );
  });
  it("are text when a moderator removed the image", () => {
    expect(html("<:gone:9>!")).toBe(":gone:!");
  });
  it("stay text in code and keep their name from Markdown", () => {
    expect(html("`<:blob:7>`")).toBe("<code>&lt;:blob:7&gt;</code>");
    expect(markdown(escapeHtml("<:_ok_:3> _a_"))).toBe("&lt;:_ok_:3&gt; <em>a</em>");
  });
  it("cannot be forged by message HTML", () => {
    const out = sanitize('<img class="emoji custom" src="https://chat.example/api/emoji/personal/7"><a title="<:blob:7>">x</a>', false, set);
    expect(out).not.toContain("src=");
  });
  it("count for jumbo emoji, removed ones do not", () => {
    expect(jumboEmoji("<:blob:7> 🎉", set)).toBe(true);
    expect(jumboEmoji("<:gone:9>", set)).toBe(false);
  });
  it("are known (usable) when they are ours", () => {
    expect(knownEmoji("<:blob:7>", set)).toBe(true);
    expect(knownEmoji("<:other:12>", set)).toBe(false);
  });
});

describe("typing personal emoji", () => {
  const server = set.urls;
  it("become references when sent: :name: of ours, server emoji first", () => {
    expect(toRefs(":blob: :party: :nope: <:blob:7>", mine, server)).toBe("<:blob:7> :party: :nope: <:blob:7>");
    expect(toRefs("`:blob:` https://x.org/:blob: :blob:", mine, server)).toBe("`:blob:` https://x.org/:blob: <:blob:7>");
  });
  it("show as :name: again when edited, if that gives the same emoji", () => {
    expect(fromRefs("<:blob:7> <:blob:12> <:party:8> <:x_y:3>", mine, server)).toBe(":blob: <:blob:12> <:party:8> <:x_y:3>");
  });
  it.each([
    "`<:blob:7>`",
    "```\n<:blob:7>\n```",
    "https://example.org/<:blob:7>",
    "http://example.org/?emoji=<:blob:7>",
  ])("preserves literal references when an unchanged edit contains %s", (literal) => {
    const original = `before <:blob:7> ${literal} after <:blob:12> <:party:8>`;
    const editable = fromRefs(original, mine, server);
    expect(editable).toBe(`before :blob: ${literal} after <:blob:12> <:party:8>`);
    expect(toRefs(editable, mine, server)).toBe(original);
  });
  it("go into the message box as :name: when that is unambiguous", () => {
    expect(composerEmoji("<:blob:7>", mine, server)).toBe(":blob:");
    expect(composerEmoji("<:party:8>", mine, server)).toBe("<:party:8>");
    expect(composerEmoji("👍", mine, server)).toBe("👍");
  });
  it("are picker entries with their reference", () => {
    expect(personalEntries(mine)[0]).toMatchObject({ emoji: "<:blob:7>", name: "blob" });
  });
});

describe("adding personal emoji", () => {
  const file = { size: 1000, type: "image/png" };
  it("checks the name against ours and the server's, and the limit", () => {
    expect(personalFormError("cat", file, mine, set.urls)).toBeNull();
    expect(personalFormError("blob", file, mine, set.urls)).toMatch(/You have/);
    expect(personalFormError("party", file, [], set.urls)).toMatch(/server/);
    expect(personalFormError("Cat", file, mine, set.urls)).toMatch(/lower case/);
    const full = Array.from({ length: 50 }, (_, i) => ({ name: `e${i}`, id: i + 1 }));
    expect(personalFormError("cat", file, full, set.urls)).toMatch(/50/);
    expect(personalFormError("cat", null, mine, set.urls)).toMatch(/image/);
    expect(personalFormError("cat", null, mine, set.urls, false)).toBeNull(); // from a message: no file
  });
});

describe("an emoji in a message", () => {
  const rights = { add: true, remove: false };
  it("can be added: a server emoji or someone's, under a free name", () => {
    expect(emojiActions(":party:", set, rights)).toEqual({
      name: "party",
      url: "https://chat.example/api/emoji/1",
      kind: "server",
      mine: false,
      add: "party_2", // the server's name, and ours too
      remove: null,
    });
    expect(emojiActions("<:cool:12>", set, rights)).toMatchObject({ kind: "personal", mine: false, add: "cool", remove: null });
  });
  it("is not added again when it is ours", () => {
    expect(emojiActions("<:other_name:7>", set, rights)).toMatchObject({ mine: true, add: null });
  });
  it("can be removed everywhere by moderators, personal ones only", () => {
    const mod = { add: false, remove: true };
    expect(emojiActions("<:cool:12>", set, mod)).toMatchObject({ add: null, remove: "<:cool:12>" });
    expect(emojiActions(":party:", set, mod)).toMatchObject({ remove: null });
  });
  it("has no actions when it shows no image", () => {
    expect(emojiActions("<:gone:9>", set, rights)).toBeNull();
    expect(emojiActions(":nope:", set, rights)).toBeNull();
    expect(emojiActions("👍", set, rights)).toBeNull();
  });
  it("freeName counts up, within 32 letters", () => {
    expect(freeName("cat", new Set(["cat", "cat_2"]))).toBe("cat_3");
    expect(freeName("x".repeat(32), new Set(["x".repeat(32)]))).toBe(`${"x".repeat(30)}_2`);
  });
});
