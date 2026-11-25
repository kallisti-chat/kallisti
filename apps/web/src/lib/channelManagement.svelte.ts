import { createChannel, editChannel, may, Perm, queryPermissions, removeChannel } from "@kallisti/protocol";
import type { ChannelForm, ChannelKind } from "./channelForm";
import { textGroups } from "./categories";
import {
  canCreateSubchannel,
  canCreateText,
  canListenOnly,
  canManageMembers,
  canManageText,
  canModerate,
  canViewMembers,
  managesCategory,
  menuRights,
  moderatorsChannel,
  voiceSiblings,
} from "./channelManagement";
import { channelModActions } from "./roles";
import { listenOnlyAction, speakerCandidates, type ListenOnly } from "./listenOnly";
import { readOnlyAction } from "./readOnly";
import { accessChannel, membersAction, membersOf, privateAction, type Member } from "./privateChannels";
import { moveInList, moveVoiceChannel } from "./channelOrder";
import type { Connection } from "./connection.svelte";
import type { ManageAction } from "./textChannels";
import { menuAnchor } from "./menu";

export type ChannelValues = {
  kind: ChannelKind;
  name: string;
  description: string;
  temporary: boolean;
  listenOnly?: ListenOnly;
  readOnly?: ListenOnly;
  private?: boolean;
  slowMode?: number;
  category?: number; // a text channel's (0: none)
};

// The channel menu and dialog: what they show and what they send.
export const createChannelManagement = (current: () => Connection) => {
  let opened = $state<{ kind: ChannelKind; id: number; x: number; y: number } | null>(null);
  let form = $state<{ form: ChannelForm; id: number } | null>(null);
  // The members dialog of a private channel, and the moderators dialog
  // (by the Mumble channel).
  let members = $state<{ kind: ChannelKind; id: number; access: number } | null>(null);
  let moderators = $state<{ kind: ChannelKind; id: number; access: number } | null>(null);
  // Who has access, read-only (members of a private channel).
  let viewing = $state<{ kind: ChannelKind; id: number; access: number } | null>(null);
  const connection = $derived(current());
  const server = $derived(connection.state);
  const text = $derived(connection.text);
  const canMakeVoice = $derived(may(server, 0, Perm.MakeChannel));
  const canCreate = $derived({
    text: canManageText(server, text),
    voice: canMakeVoice || may(server, 0, Perm.MakeTempChannel),
    category: canManageText(server, text),
  });
  // Staff choose a text channel's category in the dialog.
  const categoryChoice = () => (canManageText(server, text) ? { categories: text.categories.map(({ id, name }) => ({ id, name })) } : {});
  const textChannel = (id: number) => text.channels.find((c) => c.id === id);
  // A menu whose channel went away is closed.
  const menu = $derived.by(() => {
    const rights = opened && menuRights(server, text, opened.kind, opened.id);
    return opened && rights
      ? {
          ...opened,
          ...rights,
          canMembers: canManageMembers(server, text, opened.kind, opened.id),
          canViewMembers: canViewMembers(server, text, opened.kind, opened.id),
          canCreateSub: opened.kind === "voice" && canCreateSubchannel(server, text, opened.id),
          canModerators: moderatorsChannel(server, text, opened.kind, opened.id) !== null,
          name: nameOf(opened.kind, opened.id),
        }
      : null;
  });
  // The moderators dialog: their names from the registered users we know.
  const moderatorsForm = $derived.by(() =>
    moderators && server.channels[moderators.access]
      ? {
          ...moderators,
          name: nameOf(moderators.kind, moderators.id),
          members: (text.roles.channelMods?.[moderators.access] ?? []).map((id) => ({ id, name: text.members[id] ?? `#${id}` })),
        }
      : null,
  );
  // A members dialog whose channel stopped being private is closed.
  const membersForm = $derived.by(() =>
    members && members.access in text.private
      ? { ...members, name: nameOf(members.kind, members.id), members: text.private[members.access] ?? [] }
      : null,
  );

  // Closed when the channel stops being private or we are no member.
  const viewForm = $derived.by(() =>
    viewing && canViewMembers(server, text, viewing.kind, viewing.id)
      ? { ...viewing, name: nameOf(viewing.kind, viewing.id), members: membersOf(text, viewing.access) ?? [] }
      : null,
  );

  // The private switch on edit: admins and moderators, not root, and text
  // channels only with a mirror.
  const privateField = (kind: ChannelKind, id: number) => {
    const access = accessChannel(kind, id, text.channels, text.categories);
    if (!canManageText(server, text) || access === null || access === 0) return {};
    return { private: { on: access in text.private, members: text.private[access] ?? [] } };
  };

  // The read-only switch on a text channel's edit (staff, with a mirror).
  const readOnlyField = (kind: ChannelKind, id: number) => {
    const mirror = kind === "text" ? textChannel(id)?.mumble : undefined;
    if (!canManageText(server, text) || mirror === undefined) return {};
    return { readOnly: { on: mirror in text.readOnly, speakers: text.readOnly[mirror] ?? [], users: speakerCandidates(server) } };
  };

  const nameOf = (kind: ChannelKind, id: number) =>
    (kind === "text" ? textChannel(id) : kind === "category" ? text.categories.find((c) => c.id === id) : server.channels[id])?.name ?? "";

  return {
    get menu() {
      return menu;
    },
    get form() {
      return form;
    },
    get canCreate() {
      return canCreate;
    },
    // A "+" on a category: staff, and its moderators.
    canCreateIn: (category: number) => canCreateText(server, text, category),
    get members() {
      return membersForm;
    },
    get viewing() {
      return viewForm;
    },
    get moderators() {
      return moderatorsForm;
    },
    open: (kind: ChannelKind, id: number, event: MouseEvent) => {
      event.preventDefault();
      // Ask what we may do here and with the siblings (for moves). Root
      // has no siblings.
      if (kind === "voice") {
        const ids = voiceSiblings(server, text, id).map((c) => c.id);
        for (const c of ids.length ? ids : [id]) connection.send(queryPermissions(c));
      }
      opened = { kind, id, ...menuAnchor(event, (event.currentTarget as Element).getBoundingClientRect()) };
    },
    closeMenu: () => (opened = null),
    // in: a text channel's category, or a voice subchannel's parent (its
    // moderators may make private ones too).
    startCreate: (kind: ChannelKind, inside = 0) =>
      (form = {
        form:
          kind === "voice" && inside
            ? { mode: "create", kind, temporaryOnly: false, canPrivate: canModerate(server, text, inside), parent: inside }
            : {
                mode: "create",
                kind,
                temporaryOnly: kind === "voice" && !canMakeVoice,
                canPrivate: canManageText(server, text),
                ...(kind === "text" ? { canCategory: canManageText(server, text) && !inside, category: inside, ...categoryChoice() } : {}),
              },
        id: 0,
      }),
    startEdit: (kind: ChannelKind, id: number) =>
      (form = {
        form: kind === "category" ? { mode: "edit", kind, name: nameOf(kind, id), description: "", ...privateField(kind, id) } : {
          mode: "edit",
          kind,
          name: nameOf(kind, id),
          // Text channels: the topic (plain text).
          description: kind === "voice" ? (server.channels[id]?.description ?? "") : (text.channels.find((c) => c.id === id)?.topic ?? ""),
          ...(kind === "voice" && canListenOnly(server, text, id)
            ? { listenOnly: { on: id in text.listenOnly, speakers: text.listenOnly[id] ?? [], users: speakerCandidates(server) } }
            : {}),
          ...privateField(kind, id),
          ...readOnlyField(kind, id),
          ...(kind === "text" && canModerate(server, text, accessChannel(kind, id, text.channels, text.categories))
            ? { slowMode: text.channels.find((c) => c.id === id)?.slowMode ?? 0 }
            : {}),
          ...(kind === "text" && canManageText(server, text) ? { category: textChannel(id)?.category ?? 0, ...categoryChoice() } : {}),
          // A category's moderators rename its channels too.
          ...(kind === "text" && !canManageText(server, text) && !managesCategory(server, text, textChannel(id)?.category) ? { topicOnly: true } : {}),
        },
        id,
      }),
    startMembers: (kind: ChannelKind, id: number) => {
      const access = accessChannel(kind, id, text.channels, text.categories);
      if (access !== null) members = { kind, id, access };
    },
    closeMembers: () => (members = null),
    startViewMembers: (kind: ChannelKind, id: number) => {
      const access = accessChannel(kind, id, text.channels, text.categories);
      if (access !== null) viewing = { kind, id, access };
    },
    closeViewMembers: () => (viewing = null),
    startModerators: (kind: ChannelKind, id: number) => {
      const access = moderatorsChannel(server, text, kind, id);
      if (access !== null) moderators = { kind, id, access };
    },
    closeModerators: () => (moderators = null),
    // Both settle once the server confirms every change (see lib/saveFeedback).
    submitModerators: async (after: Member[]) => {
      const f = moderatorsForm;
      if (!f) return;
      await Promise.all(channelModActions(f.access, f.members.map((m) => m.id), after.map((m) => m.id)).map(connection.request));
    },
    submitMembers: async (after: Member[]) => {
      const action = membersForm && membersAction(membersForm.access, membersForm.members, after);
      if (action) await connection.request(action);
    },
    startDelete: (kind: ChannelKind, id: number) => (form = { form: { mode: "delete", kind, name: nameOf(kind, id) }, id }),
    closeForm: () => (form = null),
    move: (kind: ChannelKind, id: number, step: -1 | 1) => {
      if (kind === "category") connection.manage({ orderCategories: moveInList(text.categories.map((c) => c.id), id, step) });
      else if (kind === "text") {
        // Within its category (or among those without one).
        const group = textGroups(text.channels, text.categories).find((g) => g.channels.some((c) => c.id === id));
        if (group) connection.manage({ order: moveInList(group.channels.map((c) => c.id), id, step) });
      } else for (const c of moveVoiceChannel(voiceSiblings(server, text, id), id, step)) connection.send(editChannel(c.id, { position: c.position }));
    },
    // Settles once the server confirms the Kallisti changes (Mumble's
    // channel changes have no answer to wait for).
    submit: (v: ChannelValues) => {
      if (!form) return Promise.resolve();
      const { form: f, id } = form;
      const waits: Promise<void>[] = [];
      const ask = (a: ManageAction) => waits.push(connection.request(a));
      if (f.mode === "create") {
        if (v.kind === "category") ask({ createCategory: v.name, private: v.private });
        else if (v.kind === "text") ask({ create: v.name, private: v.private, ...(v.category ? { category: v.category } : {}) });
        else if (v.private) ask({ createPrivateVoice: v.name, parent: f.parent });
        else connection.send(createChannel(f.parent ?? 0, v.name, { temporary: v.temporary }));
      } else if (f.mode === "edit") {
        const access = accessChannel(v.kind, id, text.channels, text.categories);
        const priv = f.private && access !== null && privateAction(access, f.private.on, v.private ?? false, f.private.members);
        if (priv) ask(priv);
        if (v.kind === "category") {
          if (v.name !== f.name) ask({ renameCategory: id, name: v.name });
        } else if (v.kind === "text") {
          if (v.name !== f.name) ask({ rename: id, name: v.name });
          if (v.description.trim() !== f.description) ask({ topic: id, text: v.description.trim() });
          if (v.slowMode !== undefined && v.slowMode !== f.slowMode) ask({ slowMode: id, seconds: v.slowMode });
          if (v.category !== undefined && f.categories && v.category !== f.category) ask({ setCategory: id, category: v.category });
          const mirror = textChannel(id)?.mumble;
          const readOnly = f.readOnly && v.readOnly && mirror !== undefined && readOnlyAction(mirror, f.readOnly, v.readOnly);
          if (readOnly) ask(readOnly);
        } else {
          const changes = {
            ...(v.name !== f.name ? { name: v.name } : {}),
            ...(v.description !== f.description ? { description: v.description } : {}),
          };
          if (Object.keys(changes).length) connection.send(editChannel(id, changes));
          const listenOnly = f.listenOnly && v.listenOnly && listenOnlyAction(id, f.listenOnly, v.listenOnly);
          if (listenOnly) ask(listenOnly);
        }
      } else if (v.kind === "category") ask({ removeCategory: id });
      else if (v.kind === "text") ask({ remove: id });
      else connection.send(removeChannel(id));
      return Promise.all(waits).then(() => {});
    },
  };
};
