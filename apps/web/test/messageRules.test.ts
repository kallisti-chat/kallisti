import { describe, expect, it } from "vitest";
import {
  formatList,
  formatReplacements,
  minutesError,
  parseList,
  parseMessageRules,
  parseReplacements,
  rulesFrom,
} from "../src/lib/messageRules";
import { initialTextState, manageRequest, parseServerEvent, reduceText } from "../src/lib/textChannels";

const replacements = [{ phrase: "nice ass", with: ["beautiful behind", "lovely posterior"] }];
const rules = { newMinutes: 10, words: ["spam", "buy now"], domains: ["junk.example"], replacements };

describe("parseMessageRules", () => {
  it("takes the server's rules", () => {
    expect(parseMessageRules(rules)).toEqual(rules);
    expect(parseMessageRules({ ...rules, words: null })).toBeNull();
    expect(parseMessageRules({ ...rules, words: [1] })).toBeNull();
    expect(parseMessageRules({ ...rules, newMinutes: "10" })).toBeNull();
    expect(parseMessageRules({ ...rules, replacements: [{ phrase: "x" }] })).toBeNull();
    expect(parseMessageRules({ ...rules, replacements: undefined })).toEqual({ ...rules, replacements: [] }); // an older server
    expect(parseMessageRules(null)).toBeNull();
  });
});

describe("lists", () => {
  it("are one entry per line, without empty lines", () => {
    expect(parseList(" spam \n\n  buy now\r\n")).toEqual(["spam", "buy now"]);
    expect(parseList("")).toEqual([]);
    expect(formatList(["spam", "buy now"])).toBe("spam\nbuy now");
  });
});

describe("replacements", () => {
  it("are one phrase per line with its replacements", () => {
    const text = "nice ass => beautiful behind | lovely posterior";
    expect(parseReplacements(text)).toEqual({ replacements, errors: [] });
    expect(formatReplacements(replacements)).toBe(text);
    expect(parseReplacements("  nice   ass=>beautiful  behind|  |lovely posterior \n\n").replacements).toEqual(replacements);
    expect(parseReplacements("butt => 🍑 | it's a bottom, really!").errors).toEqual([]);
    expect(parseReplacements("")).toEqual({ replacements: [], errors: [] });
  });
  it("says what is wrong on which line", () => {
    const { replacements: ok, errors } = parseReplacements(
      ["good => fine", "no arrow", " => x", "x =>  | ", "c++ => c", "x => **bold**", "x => @everyone", "x => a => b"].join("\n"),
    );
    expect(ok).toEqual([{ phrase: "good", with: ["fine"] }]);
    expect(errors.map((e) => e.split(":")[0])).toEqual(["Line 2", "Line 3", "Line 4", "Line 5", "Line 6", "Line 7", "Line 8"]);
    expect(errors[3]).toBe("Line 5: the phrase: up to 100 letters and digits (with ' - . _ between).");
    expect(errors[4]).toBe(`Line 6: "**bold**": up to 100 letters, digits, emoji, spaces and ' . , ! ? -.`);
  });
});

describe("minutesError", () => {
  it("allows 0 (off) to a week of whole minutes", () => {
    for (const ok of ["0", "10", " 10080 "]) expect(minutesError(ok)).toBeNull();
    for (const bad of ["10081", "-1", "1.5", "abc", ""]) expect(minutesError(bad)).not.toBeNull();
  });
});

describe("rulesFrom", () => {
  it("makes the rules of the form's fields", () => {
    expect(rulesFrom(" 10 ", "spam\nbuy now", "junk.example\n", replacements)).toEqual(rules);
  });
});

describe("message rules events and requests", () => {
  it("keeps the rules an admin asked for", () => {
    const s = reduceText(initialTextState, parseServerEvent(JSON.stringify({ type: "message-rules", rules }))!);
    expect(s.messageRules).toEqual(rules);
    expect(parseServerEvent('{"type":"message-rules"}')).toBeNull();
    expect(JSON.parse(manageRequest({ messageRules: true }))).toEqual({ type: "message-rules" });
    expect(JSON.parse(manageRequest({ setMessageRules: rules }))).toEqual({ type: "set-message-rules", rules });
  });
});
