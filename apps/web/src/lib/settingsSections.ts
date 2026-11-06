import type { Key } from "../i18n/en";
import type { Category } from "./dialogCategories";

// The user settings' categories (see SettingsDialog, lib/dialogCategories);
// titles are text keys (t() where they show).

export type SectionId = "profile" | "account" | "voice" | "notifications" | "appearance" | "translation";
export type Section = Category<SectionId, "user" | "app"> & { title: Key };

const ALL: Section[] = [
  { id: "profile", title: "settings.sectionProfile", group: "user" },
  { id: "account", title: "settings.sectionAccount", group: "user" },
  { id: "voice", title: "settings.sectionVoice", group: "app" },
  { id: "notifications", title: "settings.sectionNotifications", group: "app" },
  { id: "appearance", title: "settings.sectionAppearance", group: "app" },
  { id: "translation", title: "settings.sectionTranslation", group: "app" },
];

export const GROUP_TITLES = { user: "settings.groupUser", app: "settings.groupApp" } as const satisfies Record<Section["group"], Key>;

// The categories there is something for (no account: no Account).
export const sectionsFor = (has: { profile: boolean; account: boolean }): Section[] =>
  ALL.filter((s) => (s.id === "profile" ? has.profile : s.id === "account" ? has.account : true));
