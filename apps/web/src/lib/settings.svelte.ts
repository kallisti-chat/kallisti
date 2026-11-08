import { parseCollapsedGroups, toggleGroup } from "./memberGroups";
import { deviceListOf, parseDeviceLists, setDeviceList, type ChannelList } from "./channelList";
import { bindUserPreferences, parseUserPreferences, type UserPreferences } from "./userPreferences";
import { collapsedIn, parseCollapsed, toggleCollapsed } from "./categories";
import { parseChannelPrefs, setChannelPref, type ChannelPref, type ChannelPrefs } from "./channelPrefs";
import { directPrefsOf, parseDirectPrefs, setDirectPrefs, type DirectPrefs } from "./directList";
import { persisted } from "./persisted.svelte";
import { parseSettings, type Settings } from "./settings";
import { parseShare } from "./stream/stageSize";

const voiceSettings = persisted("voiceSettings", parseSettings);

export const settings = {
  get value() {
    return voiceSettings.value;
  },
  update: (patch: Partial<Settings>) => voiceSettings.set({ ...voiceSettings.value, ...patch }),
};

// The key from when only mutes were kept (parseChannelPrefs reads both).
const channelStore = persisted("mutedChannels", parseChannelPrefs);

export const channelPrefs = {
  get value() {
    return channelStore.value;
  },
  set: (server: string, id: number, patch: ChannelPref) => channelStore.set(setChannelPref(channelStore.value, server, id, patch)),
  update: (change: (prefs: ChannelPrefs) => ChannelPrefs) => channelStore.set(change(channelStore.value)),
};

// Collapsed text categories, per server (see lib/categories).
const collapsedStore = persisted("collapsedCategories", parseCollapsed);

export const collapsedCategories = {
  of: (server: string) => collapsedIn(collapsedStore.value, server),
  toggle: (server: string, id: number) => collapsedStore.set(toggleCollapsed(collapsedStore.value, server, id)),
};

// Own channel lists without an account (with one, the server keeps it).
const channelListStore = persisted("channelLists", parseDeviceLists);

export const channelLists = {
  of: (server: string) => deviceListOf(channelListStore.value, server),
  set: (server: string, list: ChannelList) => channelListStore.set(setDeviceList(channelListStore.value, server, list)),
};

// The member list's collapsed groups (see lib/memberGroups).
const memberGroupsStore = persisted("collapsedMemberGroups", parseCollapsedGroups);
export const collapsedMemberGroups = {
  get value() {
    return memberGroupsStore.value;
  },
  toggle: (key: string) => memberGroupsStore.set(toggleGroup(memberGroupsStore.value, key)),
};

const directStore = persisted("directChats", parseDirectPrefs);

export const directPrefs = {
  of: (server: string) => directPrefsOf(directStore.value, server),
  update: (server: string, fn: (p: DirectPrefs) => DirectPrefs) => {
    const before = directPrefsOf(directStore.value, server);
    const next = fn(before);
    if (next !== before) directStore.set(setDirectPrefs(directStore.value, server, next));
  },
};

const preferenceStore = persisted("serverUserPreferences", (raw) => ({ version: 1, servers: parseUserPreferences(raw) }));

export const userPreferences = {
  forServer: (scope: string) => bindUserPreferences({
    get value() { return preferenceStore.value.servers; },
    set: (servers: UserPreferences) => preferenceStore.set({ version: 1, servers }),
  }, scope),
};

// The stream view's height (see stream/stageSize.ts; null: the default).
export const streamStage = persisted("streamStage", parseShare);
