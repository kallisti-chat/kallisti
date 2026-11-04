import { byName } from "@kallisti/protocol";
import { roleOf, type Roles } from "./roles";
import type { Peer } from "./textChannels";
import { t } from "./i18n.svelte";

// Registered users by id (built-in server, without guest accounts): the
// member list shows those who are offline, like Discord.
export type Members = Readonly<Record<number, string>>;
// Members online in any channel: their session by user id. Those in
// channels we cannot see are online without a channel, like Discord.
export type Online = Readonly<Record<number, number>>;
export type OnlineMember = Peer & { session: number };

type Seen = readonly { userId: number | null }[];
const peers = (members: Members) => Object.entries(members).map(([id, name]) => ({ id: Number(id), name }));
const seenIds = (users: Seen) => new Set(users.map((u) => u.userId));

// Registered users who are not online (by user id: names can repeat
// between a guest and an account), by name like the online list.
export const offlineMembers = (members: Members, online: Online, users: Seen): Peer[] => {
  const here = seenIds(users);
  return peers(members)
    .filter((p) => !here.has(p.id) && online[p.id] === undefined)
    .sort((a, b) => byName(a.name, b.name));
};

// Members online whom we do not see (their channel is hidden from us, or
// their user state has not come yet), by name.
export const hiddenMembers = (members: Members, online: Online, users: Seen): OnlineMember[] => {
  const here = seenIds(users);
  return peers(members)
    .flatMap((p) => (here.has(p.id) || online[p.id] === undefined ? [] : [{ ...p, session: online[p.id]! }]))
    .sort((a, b) => byName(a.name, b.name));
};

// The online group: users we see and hidden members, by name.
export type OnlineRow<U> = { user: U } | { member: OnlineMember };
export const onlineRows = <U extends { name: string }>(users: readonly U[], hidden: readonly OnlineMember[]): OnlineRow<U>[] => {
  const name = (r: OnlineRow<U>) => ("user" in r ? r.user.name : r.member.name);
  return [...users.map((user) => ({ user })), ...hidden.map((member) => ({ member }))].sort((a, b) => byName(name(a), name(b)));
};

// Who reads a private channel: its members, then staff (admins and
// moderators, who see every private channel), each user once. Online
// first, then offline (faded), each by name. Names from the member list,
// the channel's list (also guest accounts) and the users we see. Null for
// us (me) if we are neither (the server sends members to readers only).
export type ReaderRow<U> = OnlineRow<U> | { offline: Peer };
export type ReaderGroup<U> = { title: "Members" | "Staff"; rows: ReaderRow<U>[] };
// A group's title to show (title itself is a stable id: collapsed groups are saved by it).
export const readerGroupLabel = (title: ReaderGroup<unknown>["title"]) => t(title === "Staff" ? "client.readersStaff" : "client.readersMembers");
export const readerGroups = <U extends { name: string; userId: number | null }>(
  members: readonly Peer[],
  roles: Roles,
  registered: Members,
  online: Online,
  users: readonly U[],
  me: number | null,
): ReaderGroup<U>[] | null => {
  const isStaff = (id: number) => roleOf(roles, id) !== null;
  if (me === null || !(isStaff(me) || members.some((m) => m.id === me))) return null;
  const names: Record<number, string> = { ...registered, ...Object.fromEntries(members.map((m) => [m.id, m.name])) };
  for (const u of users) if (u.userId !== null) names[u.userId] = u.name;
  const rows = (ids: number[]): ReaderRow<U>[] => {
    const some: Members = Object.fromEntries(ids.map((id) => [id, names[id]!]));
    const seen = users.filter((u) => u.userId !== null && u.userId in some);
    const offline = offlineMembers(some, online, users).map((p) => ({ offline: p }));
    return [...onlineRows(seen, hiddenMembers(some, online, users)), ...offline];
  };
  const groups: ReaderGroup<U>[] = [
    { title: "Members", rows: rows(members.map((m) => m.id).filter((id) => !isStaff(id))) },
    { title: "Staff", rows: rows(Object.keys(names).map(Number).filter(isStaff)) },
  ];
  return groups.filter((g) => g.rows.length);
};

// A reader row's name and key (names repeat: a guest and an account, one
// user in two sessions).
export const readerRow = <U extends { name: string; session: number }>(r: ReaderRow<U>) =>
  "user" in r ? { key: `s${r.user.session}`, name: r.user.name } : "member" in r ? { key: `u${r.member.id}`, name: r.member.name } : { key: `o${r.offline.id}`, name: r.offline.name };

// The names of members with a saved picture (see TextState.pictures).
export const pictureNames = (members: Members, pictures: ReadonlySet<number>) =>
  new Set([...pictures].flatMap((id) => (members[id] === undefined ? [] : [members[id]])));

// An offline user's bio (online ones come with their user state).
export const bioRequest = (user: number) => JSON.stringify({ type: "bio", user });

// Without this server's friends (by user id): they show in the Friends
// group on top of the member list, not twice.
export const withoutFriends = <U extends { userId: number | null }>(rows: readonly ReaderRow<U>[], friends: ReadonlySet<number>): ReaderRow<U>[] =>
  rows.filter((r) => {
    const id = "user" in r ? r.user.userId : "member" in r ? r.member.id : r.offline.id;
    return id === null || !friends.has(id);
  });
