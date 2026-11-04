<script lang="ts">
  import ContextMenu from "./ContextMenu.svelte";
  import { t } from "../lib/i18n.svelte";
  import type { Key } from "../i18n/en";
  import { isMuted, levelOf, LEVELS, MUTE_FOR, mutedUntilText, muteUntil, type ChannelPref } from "../lib/channelPrefs";

  // Right-click menu of a channel, or of a direct chat (direct). Items show
  // only when the user may use them (the server checks again).
  let {
    name,
    x,
    y,
    canEdit = false,
    canDelete = false,
    canMoveUp = false,
    canMoveDown = false,
    canMembers = false,
    canViewMembers = false,
    onCopyLink,
    onCopyInvite,
    canModerators = false,
    canCreateSub = false,
    recording,
    onRecord,
    notify,
    onNotify,
    onEdit,
    onCreateSub,
    onMove,
    onDelete,
    onMembers,
    onViewMembers,
    onModerators,
    onWebhooks,
    direct,
    thread,
    own,
    category = false,
    onClose,
  }: {
    name: string;
    x: number;
    y: number;
    canEdit?: boolean;
    canDelete?: boolean;
    canMoveUp?: boolean;
    canMoveDown?: boolean;
    // A private channel's members (staff and its moderators); the
    // channel's moderators (staff).
    canMembers?: boolean;
    // Who has access, read-only (a private channel's members).
    canViewMembers?: boolean;
    onViewMembers?: () => void;
    // A link to the channel (a one-use invite to a private one, for who
    // manages it).
    onCopyLink?: () => void;
    // A one-use invite link (a private channel or category, for who
    // manages it).
    onCopyInvite?: () => void;
    canModerators?: boolean;
    // Voice channels: a subchannel in it (MakeChannel there).
    canCreateSub?: boolean;
    // Voice channels (staff): being recorded or not; undefined: may not
    // record.
    recording?: boolean;
    onRecord?: (on: boolean) => void;
    // Text channels and direct chats: notification settings (see
    // lib/channelPrefs; direct chats only mute); undefined: none. desktop:
    // desktop notifications are on.
    notify?: { pref: ChannelPref; desktop: boolean };
    onNotify?: (patch: ChannelPref) => void;
    onEdit?: () => void;
    onCreateSub?: () => void;
    onMove?: (step: -1 | 1) => void;
    onDelete?: () => void;
    onMembers?: () => void;
    onModerators?: () => void;
    onWebhooks?: () => void; // a text channel's webhooks (staff)
    // A direct chat (see lib/directList).
    direct?: { pinned: boolean; onProfile: () => void; onRead?: () => void; onPin: () => void; onHide: () => void };
    // A thread (see lib/threads): follow it, archive it (or open it again;
    // undefined: may not), take its channel's notification settings again
    // (undefined: it does).
    thread?: { following: boolean; onFollow: () => void; archived: boolean; onArchive?: () => void; onInherit?: () => void };
    // Text and voice channels: the user's own list (see lib/channelList).
    own?: { hidden: boolean; onHide: () => void; canUp: boolean; canDown: boolean; onMove: (step: -1 | 1) => void };
    // A text category (see lib/categories).
    category?: boolean;
    onClose: () => void;
  } = $props();

  const act = (fn: () => void) => () => {
    fn();
    onClose();
  };
  const id = $props.id();
  type Noun = "conversation" | "thread" | "category" | "channel";
  const noun: Noun = $derived(direct ? "conversation" : thread ? "thread" : category ? "category" : "channel");
  // Whole texts per noun. Combinations the menu never shows (a direct
  // chat has no edit, no hide) take the channel's text.
  const TEXTS: Record<"show" | "hide" | "mute" | "unmute" | "edit" | "delete", Record<Noun, Key>> = {
    show: { channel: "sidebar.showChannel", category: "sidebar.showCategory", thread: "sidebar.showChannel", conversation: "sidebar.showChannel" },
    hide: { channel: "sidebar.hideChannel", category: "sidebar.hideCategory", thread: "sidebar.hideChannel", conversation: "sidebar.hideChannel" },
    mute: { channel: "sidebar.muteChannel", category: "sidebar.muteChannel", thread: "sidebar.muteThread", conversation: "sidebar.muteConversation" },
    unmute: { channel: "sidebar.unmuteChannel", category: "sidebar.unmuteChannel", thread: "sidebar.unmuteThread", conversation: "sidebar.unmuteConversation" },
    edit: { channel: "sidebar.editChannel", category: "sidebar.editCategory", thread: "sidebar.editThread", conversation: "sidebar.editChannel" },
    delete: { channel: "sidebar.deleteChannel", category: "sidebar.deleteCategory", thread: "sidebar.deleteThread", conversation: "sidebar.deleteChannel" },
  };
  const text = (action: keyof typeof TEXTS) => t(TEXTS[action][noun]);
  const changes = $derived(canEdit || canCreateSub || canMoveUp || canMoveDown || canMembers || canModerators);
  // The part below the notifications: changes, or the read-only members.
  const listed = $derived(changes || (canViewMembers && !!onViewMembers));
</script>

<ContextMenu {x} {y} label={t("sidebar.actionsFor", { name })} {onClose}>
  {#if (onCopyLink || onCopyInvite) && !direct && !thread}
    {#if onCopyLink}<button onclick={act(onCopyLink)}>{t("sidebar.copyLink")}</button>{/if}
    {#if onCopyInvite}
      <button onclick={act(onCopyInvite)}>{t("sidebar.copyInvite")}<span class="sub">{t("sidebar.inviteValidity")}</span></button>
    {/if}
    <div class="sep"></div>
  {/if}
  {#if own}
    <button onclick={act(own.onHide)}>{text(own.hidden ? "show" : "hide")}</button>
    {#if own.canUp}<button onclick={act(() => own.onMove(-1))}>{t("sidebar.moveUpForMe")}</button>{/if}
    {#if own.canDown}<button onclick={act(() => own.onMove(1))}>{t("sidebar.moveDownForMe")}</button>{/if}
    <div class="sep"></div>
  {/if}
  {#if thread}
    <button onclick={act(thread.onFollow)}>{t(thread.following ? "sidebar.unfollowThread" : "sidebar.followThread")}</button>
    {#if thread.onArchive}<button onclick={act(thread.onArchive)}>{t(thread.archived ? "sidebar.reopenThread" : "sidebar.archiveThread")}</button>{/if}
    <div class="sep"></div>
  {/if}
  {#if direct}
    <button onclick={act(direct.onProfile)}>{t("sidebar.profile")}</button>
    {#if direct.onRead}<button onclick={act(direct.onRead)}>{t("sidebar.markRead")}</button>{/if}
    <div class="sep"></div>
  {/if}
  {#if notify && onNotify}
    {#if isMuted(notify.pref, Date.now())}
      {@const end = mutedUntilText(notify.pref.mutedUntil)}
      <button onclick={act(() => onNotify({ mutedUntil: undefined }))}>{text("unmute")}</button>
      {#if end}<p class="none">{t("sidebar.mutedUntil", { time: end })}</p>{/if}
    {:else}
      <div role="group" aria-labelledby="{id}-mute">
        <p class="heading" id="{id}-mute">{text("mute")}</p>
        {#each MUTE_FOR as [label, minutes] (minutes)}
          <button onclick={act(() => onNotify({ mutedUntil: muteUntil(minutes, Date.now()) }))}>{t(label)}</button>
        {/each}
      </div>
    {/if}
  {/if}
  {#if direct}
    <div class="sep"></div>
    <button onclick={act(direct.onPin)}>{t(direct.pinned ? "sidebar.unpin" : "sidebar.pinToTop")}</button>
    <button onclick={act(direct.onHide)}>{t("sidebar.closeConversation")}</button>
  {:else if notify && onNotify}
    <div class="sep"></div>
    <div role="group" aria-labelledby="{id}-level">
      <p class="heading" id="{id}-level">{t("sidebar.notifications")}</p>
      {#each LEVELS as [level, label] (level)}
        <button class="choice" aria-pressed={levelOf(notify.pref) === level} onclick={act(() => onNotify({ level }))}>{t(label)}</button>
      {/each}
      {#if thread?.onInherit}<button onclick={act(thread.onInherit)}>{t("sidebar.sameAsChannel")}</button>{/if}
      {#if !notify.desktop}<p class="none">{t("sidebar.desktopNotificationsOff")}</p>{/if}
    </div>
    {#if listed}<div class="sep"></div>{/if}
  {/if}
  {#if recording !== undefined && onRecord}
    {#if recording}
      <button class="danger" onclick={act(() => onRecord(false))}>{t("sidebar.stopRecording")}</button>
    {:else}
      <button onclick={act(() => onRecord(true))}>{t("sidebar.recordChannel")}</button>
    {/if}
    {#if listed}<div class="sep"></div>{/if}
  {/if}
  {#if listed}
    {#if canViewMembers && onViewMembers}<button onclick={act(onViewMembers)}>{t("sidebar.membersMenu")}</button>{/if}
    {#if canEdit && onEdit}<button onclick={act(onEdit)}>{text("edit")}</button>{/if}
    {#if canCreateSub && onCreateSub}<button onclick={act(onCreateSub)}>{t("sidebar.createSubchannel")}</button>{/if}
    {#if canMembers && onMembers}<button onclick={act(onMembers)}>{t("sidebar.membersMenu")}</button>{/if}
    {#if canModerators && onModerators}<button onclick={act(onModerators)}>{t("sidebar.moderatorsMenu")}</button>{/if}
    {#if onWebhooks}<button onclick={act(onWebhooks)}>{t("sidebar.webhooksMenu")}</button>{/if}
    {#if canMoveUp}<button onclick={act(() => onMove?.(-1))}>{t("sidebar.moveUp")}</button>{/if}
    {#if canMoveDown}<button onclick={act(() => onMove?.(1))}>{t("sidebar.moveDown")}</button>{/if}
    {#if canDelete && onDelete}
      <div class="sep"></div>
      <button class="danger" onclick={act(onDelete)}>{text("delete")}</button>
    {/if}
  {:else if !notify && recording === undefined && !direct && !thread && !own}
    <p class="none">{t("sidebar.mayNotChange")}</p>
  {/if}
</ContextMenu>

<style>
  .sep {
    height: 1px;
    margin: 4px 6px;
    background: var(--border-subtle);
  }
  button {
    display: block;
    width: 100%;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    text-align: left;
    color: var(--text-muted);
  }
  button:hover {
    background: var(--accent-strong);
    color: white;
  }
  .danger {
    color: var(--danger);
  }
  .danger:hover {
    background: var(--danger);
  }
  .heading {
    margin: 0;
    padding: 4px 8px;
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .choice[aria-pressed="true"]::after {
    content: "✓" / "";
    float: right;
  }
  .none {
    margin: 0;
    padding: 8px;
    font-size: 14px;
    color: var(--text-muted);
  }
  /* A second line in an item (part of it: the hover covers both). */
  .sub {
    display: block;
    font-size: 12px;
    opacity: 0.7;
  }
</style>
