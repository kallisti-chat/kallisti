import type { Key } from "../i18n/en";
import type { Category } from "./dialogCategories";
import { t } from "./i18n.svelte";

// The server settings' categories (see ModerationDialog, CategoryDialog).

export type ServerSectionId = "registration" | "server" | "emoji" | "stickers" | "threads" | "rules" | "users" | "bans" | "guests" | "connections" | "recordings";
type Group = "server" | "members" | "monitoring";

const ALL: { id: ServerSectionId; title: Key; group: Group; needs: keyof ServerRights }[] = [
  { id: "server", title: "admin.sectionOverview", group: "server", needs: "server" },
  { id: "emoji", title: "admin.sectionEmoji", group: "server", needs: "emoji" },
  { id: "stickers", title: "admin.sectionStickers", group: "server", needs: "emoji" },
  { id: "threads", title: "admin.sectionThreads", group: "server", needs: "threads" },
  { id: "rules", title: "admin.sectionRules", group: "server", needs: "rules" },
  { id: "registration", title: "admin.sectionRegistration", group: "members", needs: "registration" },
  { id: "users", title: "admin.sectionUsers", group: "members", needs: "users" },
  { id: "bans", title: "admin.sectionBans", group: "members", needs: "bans" },
  { id: "guests", title: "admin.sectionGuests", group: "members", needs: "guests" },
  { id: "connections", title: "admin.sectionConnections", group: "monitoring", needs: "connections" },
  { id: "recordings", title: "admin.sectionRecordings", group: "monitoring", needs: "recordings" },
];

export const serverGroupTitles = (): Record<Group, string> => ({
  server: t("admin.groupServer"),
  members: t("admin.groupMembers"),
  monitoring: t("admin.groupMonitoring"),
});

// What the user may see (the server's name: admins; emoji: emoji and stickers).
export type ServerRights = Record<"server" | "bans" | "users" | "emoji" | "guests" | "threads" | "rules" | "connections" | "recordings", boolean> & { registration?: boolean };

export const serverSections = (may: ServerRights): Category<ServerSectionId, Group>[] =>
  ALL.filter((s) => may[s.needs]).map(({ needs: _, title, ...s }) => ({ ...s, title: t(title) }));
