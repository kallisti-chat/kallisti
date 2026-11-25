import { selfOf, type ServerState } from "@kallisti/protocol";
import { changedUsers, type ListenOnly } from "./listenOnly";
import { roleOf } from "./roles";
import type { TextState } from "./textChannels";

// Read-only text channels (built-in server): only staff and the chosen
// users post there. Underneath, Mumble ACL entries in the mirror (the
// server writes them and checks every post).

// mayPost: we may post in the channel of this mirror (null: none). Staff
// by the admin and moderator groups (and SuperUser), as the server's
// entries; an admin by a user entry in root has no Write in mirrors.
export const mayPost = (server: ServerState, text: Pick<TextState, "readOnly" | "roles">, mirror: number | null) => {
  const posters = mirror === null ? undefined : text.readOnly[mirror];
  const me = selfOf(server)?.userId ?? null;
  return !posters || (me !== null && (roleOf(text.roles, me) !== null || posters.some((p) => p.id === me)));
};

// What to ask the server for after the dialog (null: no change).
export const readOnlyAction = (mirror: number, before: ListenOnly, after: ListenOnly) => {
  const users = changedUsers(before, after);
  return users && { readOnly: mirror, on: after.on, users };
};
