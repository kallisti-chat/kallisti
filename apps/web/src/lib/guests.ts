import { may, Perm, selfOf, type ServerState } from "@kallisti/protocol";
import { roleOf } from "./roles";
import { t } from "./i18n.svelte";
import type { TextState } from "./textChannels";

// Guests read only (built-in server): while it is on, guests (without a
// registration, or with a guest account) read but do not write, react or
// upload. Staff are never guests. The server checks again.
export const mayWrite = (server: ServerState, text: Pick<TextState, "guestsReadOnly" | "roles">, guestAccount: boolean) => {
  const userId = selfOf(server)?.userId ?? null;
  return !text.guestsReadOnly || (userId !== null && (!guestAccount || roleOf(text.roles, userId) !== null || may(server, 0, Perm.Write)));
};

// Admins change the setting.
export const mayChangeGuests = (server: ServerState, text: TextState) => text.voiceChats && may(server, 0, Perm.Write);

// How a guest gets to write: a guest account gets a password (in the
// settings), others log in or make an account.
export const loginAction = (guestAccount: boolean) => t(guestAccount ? "client.setPassword" : "client.logIn");
export const readOnlyNote = (guestAccount: boolean) =>
  t(guestAccount ? "client.setPasswordToWrite" : "client.logInToWrite");
