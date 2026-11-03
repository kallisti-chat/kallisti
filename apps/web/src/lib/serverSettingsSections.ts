import type { Category } from "./dialogCategories";

// The server settings' categories (see ModerationDialog, CategoryDialog).

export type ServerSectionId = "registration" | "server" | "emoji" | "stickers" | "threads" | "rules" | "users" | "bans" | "guests" | "connections" | "recordings";
type Group = "server" | "members" | "monitoring";

const ALL: (Category<ServerSectionId, Group> & { needs: keyof ServerRights })[] = [
  { id: "server", title: "Overview", group: "server", needs: "server" },
  { id: "emoji", title: "Emoji", group: "server", needs: "emoji" },
  { id: "stickers", title: "Stickers", group: "server", needs: "emoji" },
  { id: "threads", title: "Threads", group: "server", needs: "threads" },
  { id: "rules", title: "Message rules", group: "server", needs: "rules" },
  { id: "registration", title: "Registration", group: "members", needs: "registration" },
  { id: "users", title: "Registered users", group: "members", needs: "users" },
  { id: "bans", title: "Bans", group: "members", needs: "bans" },
  { id: "guests", title: "Guests", group: "members", needs: "guests" },
  { id: "connections", title: "Connections", group: "monitoring", needs: "connections" },
  { id: "recordings", title: "Recordings", group: "monitoring", needs: "recordings" },
];

export const SERVER_GROUP_TITLES: Record<Group, string> = { server: "Server", members: "Members", monitoring: "Monitoring" };

// What the user may see (the server's name: admins; emoji: emoji and stickers).
export type ServerRights = Record<"server" | "bans" | "users" | "emoji" | "guests" | "threads" | "rules" | "connections" | "recordings", boolean> & { registration?: boolean };

export const serverSections = (may: ServerRights): Category<ServerSectionId, Group>[] =>
  ALL.filter((s) => may[s.needs]).map(({ needs: _, ...s }) => s);
