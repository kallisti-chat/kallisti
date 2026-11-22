import type { AudioFormat } from "./audio";
import type { Channel, ChatMessage, ServerState, User } from "./state";

export type ChannelNode = { channel: Channel; children: ChannelNode[] };

export const byName = (a: string, b: string) => a.localeCompare(b, undefined, { sensitivity: "base" });

const byPositionThenName = (a: Channel, b: Channel) =>
  a.position - b.position || byName(a.name, b.name);

export const channelTree = (state: ServerState): ChannelNode | null => {
  const root = state.channels[0];
  if (!root) return null;
  const byParent = Map.groupBy(Object.values(state.channels), (c) => c.parent);
  // Each channel has one parent, so the only cycle reachable from root goes
  // through root itself. Skipping root as a child is enough.
  const build = (channel: Channel): ChannelNode => ({
    channel,
    children: (byParent.get(channel.id) ?? [])
      .filter((c) => c.id !== root.id)
      .sort(byPositionThenName)
      .map(build),
  });
  return build(root);
};

// A channel and the others with the same parent, in tree order. Root has
// no siblings.
export const channelSiblings = (state: ServerState, channelId: number): Channel[] => {
  const parent = state.channels[channelId]?.parent;
  if (channelId === 0 || parent === null || parent === undefined) return [];
  return Object.values(state.channels)
    .filter((c) => c.parent === parent && c.id !== 0)
    .sort(byPositionThenName);
};

export const usersSorted = (state: ServerState): User[] =>
  Object.values(state.users).sort((a, b) => byName(a.name, b.name));

export const usersIn = (state: ServerState, channelId: number): User[] =>
  usersSorted(state).filter((u) => u.channelId === channelId);

const ancestorsOf = (state: ServerState, channelId: number): Set<number> => {
  const ids = new Set<number>();
  for (let id: number | null = channelId; id !== null && !ids.has(id); ) {
    ids.add(id);
    id = state.channels[id]?.parent ?? null;
  }
  return ids;
};

// Messages sent to the channel itself, or to a channel tree that contains it.
export const messagesIn = (state: ServerState, channelId: number): ChatMessage[] => {
  const trees = ancestorsOf(state, channelId);
  return state.messages.filter(
    (m) => m.channelIds.includes(channelId) || m.treeIds.some((id) => trees.has(id)),
  );
};

export const audioFormat = ({ serverVersion: v }: ServerState): AudioFormat =>
  v && (v.major > 1 || (v.major === 1 && v.minor >= 5)) ? "protobuf" : "legacy";

// The private conversation with one person, by name.
export const privateMessagesWith = (state: ServerState, peer: string): ChatMessage[] =>
  state.messages.filter((m) => m.peer === peer);

// Channels whose long description is known only by its hash.
export const missingDescriptions = (state: ServerState): number[] =>
  Object.values(state.channels)
    .filter((c) => c.descriptionHash !== null && c.description === "")
    .map((c) => c.id);

// Long data known only by its hash, to fetch: once per id and hash.
// hashOf is undefined for ids that are gone; they are forgotten (Murmur
// reuses session numbers).
export const blobsToFetch = (
  missing: number[],
  hashOf: (id: number) => string | null | undefined,
  asked: ReadonlySet<string>,
) => {
  const key = (id: number) => `${id}:${hashOf(id)}`;
  const kept = [...asked].filter((k) => hashOf(Number(k.split(":")[0])) !== undefined);
  const ids = missing.filter((id) => !asked.has(key(id)));
  return { ids, asked: new Set([...kept, ...ids.map(key)]) };
};

// Long descriptions to fetch (all of them).
export const descriptionsToFetch = (state: ServerState, asked: ReadonlySet<string>) =>
  blobsToFetch(missingDescriptions(state), (id) => state.channels[id]?.descriptionHash, asked);

// Long comments of some users (the ones on screen) to fetch: comments
// can be big, and they are only shown in the user menu. Missing: known
// only by the hash.
export const commentMissing = (u: User | undefined) => !!u && u.commentHash !== null && u.comment === "";

export const commentsToFetch = (state: ServerState, sessions: number[], asked: ReadonlySet<string>) => {
  const missing = sessions.filter((s) => commentMissing(state.users[s]));
  const { ids, asked: next } = blobsToFetch(missing, (s) => state.users[s]?.commentHash, asked);
  return { sessions: ids, asked: next };
};

// Users whose avatar is known only by its hash.
export const missingTextures = (state: ServerState): number[] =>
  Object.values(state.users)
    .filter((u) => u.textureHash !== null && u.texture === null)
    .map((u) => u.session);

// Mumble's permission bits (the ones the client looks at).
export const Perm = {
  Write: 0x1,
  Enter: 0x4,
  Speak: 0x8,
  MuteDeafen: 0x10,
  Move: 0x20,
  MakeChannel: 0x40,
  Whisper: 0x100,
  MakeTempChannel: 0x400,
  Kick: 0x10000,
  Ban: 0x20000,
  Register: 0x40000,
} as const;

const SPEAK = Perm.Speak;

// Whether we may do something in a channel, from the server's answers
// (false while unknown; ask with queryPermissions).
export const may = (state: ServerState, channelId: number, perm: number): boolean => {
  const known = state.channelPermissions[channelId] ?? (channelId === 0 ? state.rootPermissions : null);
  return known !== null && known !== undefined && (known & perm) === perm;
};

// Kallisti servers keep the root channel as a lobby without voice: users
// arrive there and join voice channels on purpose. Plain Mumble servers
// use root as a voice channel.
export const isLobby = (state: ServerState): boolean =>
  state.rootPermissions !== null && (state.rootPermissions & SPEAK) === 0;

// The own user (none before the sync).
export const selfOf = (state: ServerState): User | undefined => (state.session === null ? undefined : state.users[state.session]);

// Kallisti servers let an account connect several times; one session is
// in voice, the others are text-only: in this channel, which is none, and
// nobody else sees them.
export const TEXT_ONLY = 0xfffffffe;

export const isTextOnly = (state: ServerState): boolean => selfOf(state)?.channelId === TEXT_ONLY;

export const inVoice = (state: ServerState): boolean => {
  const me = selfOf(state);
  return me !== undefined && me.channelId !== TEXT_ONLY && !(isLobby(state) && me.channelId === 0);
};

// Other sessions of our account (by user id), in any channel.
const ourOthers = (state: ServerState): User[] => {
  const me = selfOf(state);
  return me?.userId == null ? [] : Object.values(state.users).filter((u) => u.session !== me.session && u.userId === me.userId);
};

// Our account's session in voice on another device, while we are
// text-only (null: none).
export const voiceElsewhere = (state: ServerState): User | null =>
  (isTextOnly(state) && ourOthers(state).find((u) => !(isLobby(state) && u.channelId === 0))) || null;

// The users to list, by name: a text-only session of ours is left out
// when the account shows elsewhere.
export const listedUsers = (state: ServerState): User[] =>
  isTextOnly(state) && ourOthers(state).length ? usersSorted(state).filter((u) => u.session !== state.session) : usersSorted(state);

// A ban's address for people: IPv4 (::ffff:a.b.c.d) as a.b.c.d/n.
export const banAddress = ({ address: a, mask }: { address: Uint8Array; mask: number }): string => {
  const v4 = a.length === 16 && a.subarray(0, 10).every((x) => x === 0) && a[10] === 0xff && a[11] === 0xff;
  if (v4) return `${a[12]}.${a[13]}.${a[14]}.${a[15]}/${mask - 96}`;
  const groups = [];
  for (let i = 0; i < a.length; i += 2) groups.push(((a[i]! << 8) | a[i + 1]!).toString(16));
  return `${groups.join(":")}/${mask}`;
};
