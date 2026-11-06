import { describe, expect, it } from "vitest";
import { serverSections } from "../src/lib/serverSettingsSections";

const all = { server: true, bans: true, users: true, emoji: true, guests: true, threads: true, rules: true, connections: true, recordings: true };

describe("server settings sections", () => {
  it("groups them as server, members and monitoring", () => {
    expect(serverSections(all).map((s) => `${s.group}:${s.id}`)).toEqual([
      "server:server",
      "server:quality",
      "server:emoji",
      "server:stickers",
      "server:threads",
      "server:rules",
      "members:users",
      "members:bans",
      "members:guests",
      "monitoring:connections",
      "monitoring:recordings",
    ]);
  });

  it("shows only those the user may use (emoji rights: emoji and stickers)", () => {
    const moderator = { ...all, server: false, users: false, rules: false, connections: false, recordings: false };
    expect(serverSections(moderator).map((s) => s.id)).toEqual(["emoji", "stickers", "threads", "bans", "guests"]);
    expect(serverSections({ ...moderator, emoji: false }).map((s) => s.id)).toEqual(["threads", "bans", "guests"]);
  });
});
