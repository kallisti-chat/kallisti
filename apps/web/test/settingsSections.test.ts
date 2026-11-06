import { describe, expect, it } from "vitest";
import { sectionsFor } from "../src/lib/settingsSections";

describe("settings sections", () => {
  it("shows the categories there is something for", () => {
    expect(sectionsFor({ profile: true, account: true }).map((s) => s.id)).toEqual(["profile", "account", "voice", "notifications", "appearance", "translation"]);
    expect(sectionsFor({ profile: false, account: false }).map((s) => s.id)).toEqual(["voice", "notifications", "appearance", "translation"]);
  });

  it("groups them as user and app settings", () => {
    expect(sectionsFor({ profile: true, account: true }).map((s) => s.group)).toEqual(["user", "user", "app", "app", "app", "app"]);
  });
});
