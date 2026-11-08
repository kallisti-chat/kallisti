import { describe, expect, it } from "vitest";
import { parseSearch, searchPlaceFilter, type SearchPlace } from "../src/lib/search";

const chats: SearchPlace[] = [
  { name: "general", kind: "text", id: 1 },
  { name: "lounge 2", kind: "voice", id: 7 },
  { name: "general", kind: "voice", id: 8 },
  { name: "general", kind: "dm", id: 9 },
  { name: "lounge 2", kind: "dm", id: 10 },
  { name: "dm only", kind: "dm", id: 11 },
];

describe("parseSearch", () => {
  it("takes from: and in: out of the words, as in Discord", () => {
    expect(parseSearch("pizza from:bob in:#general tonight", chats)).toEqual({ text: "pizza tonight", author: "bob", channel: 1 });
    expect(parseSearch("FROM:@Bob", chats)).toEqual({ text: "", author: "Bob" });
    expect(parseSearch('in:"Lounge 2" from:"two words"', chats)).toEqual({ text: "", author: "two words", channel: 7 });
    expect(parseSearch("in:@General", chats)).toEqual({ text: "", channel: 9 });
    expect(parseSearch('in:@"Lounge 2" hi', chats)).toEqual({ text: "hi", channel: 10 });
  });

  it.each([
    ["in:#general", 1],
    ['in:#"general"', 1],
    ["in:@general", 9],
    ['in:@"general"', 9],
    ["in:voice:general", 8],
    ['in:voice:"Lounge 2"', 7],
  ])("keeps the explicit namespace in %s", (query, channel) => {
    expect(parseSearch(query, chats)).toEqual({ text: "", channel });
  });

  it("keeps bare-name priority: text, then voice, then direct chats", () => {
    expect(parseSearch("in:general", chats).channel).toBe(1);
    expect(parseSearch('in:"lounge 2"', chats).channel).toBe(7);
    expect(parseSearch('in:"dm only"', chats).channel).toBe(11);
  });

  it.each(["in:#nowhere hi", 'in:#"dm only" hi', 'in:voice:"dm only" hi', 'in:@nowhere hi'])("does not fall back to another namespace for %s", (query) => {
    expect(parseSearch(query, chats)).toEqual({ text: "hi", channel: -1 });
  });

  it("formats suggestions that resolve to their own place", () => {
    for (const place of chats) expect(parseSearch(searchPlaceFilter(place), chats).channel).toBe(place.id);
  });

  it("leaves other words with a colon alone", () => {
    expect(parseSearch("time: 10:30 https://x.org from:", chats)).toEqual({ text: "time: 10:30 https://x.org from:" });
  });
});
