import { describe, expect, it } from "vitest";
import { initialState, Perm, type ServerState, type User } from "@kallisti/protocol";
import { adminAction, mayDeleteUser, moderatorAction, roleOf } from "../src/lib/roles";
import { initialTextState, manageRequest, type TextState } from "../src/lib/textChannels";

const text: TextState = { ...initialTextState, voiceChats: true, roles: { admins: [3], moderators: [5] } };
const admin: ServerState = { ...initialState, rootPermissions: Perm.Write | Perm.MakeChannel };
const user = (userId: number | null) => ({ session: 9, name: "x", userId }) as User;

describe("moderatorAction", () => {
  it("lets admins make and unmake moderators", () => {
    expect(moderatorAction(admin, text, user(6))).toBe("make");
    expect(moderatorAction(admin, text, user(5))).toBe("remove");
  });

  it("offers nothing for guests, admins, or without the right", () => {
    expect(moderatorAction(admin, text, user(null))).toBeNull();
    expect(moderatorAction(admin, text, user(3))).toBeNull();
    expect(moderatorAction(admin, text, user(0))).toBeNull();
    expect(moderatorAction({ ...admin, rootPermissions: Perm.MakeChannel }, text, user(6))).toBeNull();
    expect(moderatorAction(admin, { ...text, voiceChats: false }, user(6))).toBeNull();
  });
});

describe("adminAction", () => {
  // I am session 1, with this user id.
  const as = (userId: number): ServerState => ({ ...admin, session: 1, users: { 1: user(userId) } });

  it("lets admins make admins, and only SuperUser remove them", () => {
    expect(adminAction(as(3), text, user(6))).toBe("make");
    expect(adminAction(as(3), text, user(5))).toBe("make"); // a moderator
    expect(adminAction(as(3), text, user(4))).toBe("make");
    expect(adminAction(as(0), text, user(3))).toBe("remove");
    expect(adminAction(as(3), { ...text, roles: { admins: [3, 4], moderators: [] } }, user(4))).toBeNull();
  });

  it("offers nothing for SuperUser, guests, or without the right", () => {
    expect(adminAction(as(0), text, user(0))).toBeNull();
    expect(adminAction(as(0), text, user(null))).toBeNull();
    expect(adminAction({ ...as(3), rootPermissions: Perm.MakeChannel }, text, user(6))).toBeNull();
    expect(adminAction(as(0), { ...text, voiceChats: false }, user(6))).toBeNull();
  });

  it("asks the server", () => {
    expect(JSON.parse(manageRequest({ admin: 6, on: true }))).toEqual({ type: "set-admin", user: 6, admin: true });
  });
});

describe("roleOf", () => {
  it("name a user's role", () => {
    const roles = { admins: [3], moderators: [5] };
    expect(roleOf(roles, 0)).toBe("admin"); // SuperUser
    expect(roleOf(roles, 3)).toBe("admin");
    expect(roleOf(roles, 5)).toBe("moderator");
    expect(roleOf(roles, 6)).toBeNull();
    expect(roleOf(roles, null)).toBeNull(); // guests
  });
});

describe("mayDeleteUser", () => {
  const as = (userId: number): ServerState => ({ ...admin, session: 1, users: { 1: user(userId) } });

  it("keeps SuperUser, and admins for SuperUser", () => {
    expect(mayDeleteUser(as(3), text, 6)).toBe(true);
    expect(mayDeleteUser(as(3), text, 5)).toBe(true); // a moderator
    expect(mayDeleteUser(as(3), text, 3)).toBe(false);
    expect(mayDeleteUser(as(0), text, 3)).toBe(true);
    expect(mayDeleteUser(as(0), text, 0)).toBe(false);
  });
});
