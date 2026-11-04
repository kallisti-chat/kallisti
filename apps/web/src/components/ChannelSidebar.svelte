<script lang="ts">
  import UserName from "./UserName.svelte";
  import { channelTree, usersIn, type ChannelNode, type ServerState } from "@kallisti/protocol";
  import { longPress } from "../lib/longPress";
  import { mirrorIds, withoutMirrors } from "../lib/mirrors";
  import { shownCollapsed, textGroups, type TextCategory } from "../lib/categories";
  import { noChannelList, ownView } from "../lib/channelList";
  import { isLocked } from "../lib/privateChannels";
  import { htmlToText } from "../lib/htmlText";
  import type { ChannelKind } from "../lib/channelForm";
  import type { Peer, TextChannel } from "../lib/textChannels";
  import type { Thread } from "../lib/threads";
  import { isCamera, streamId } from "../lib/stream/protocol";
  import Avatar from "./Avatar.svelte";
  import Badge from "./Badge.svelte";
  import Icon from "./Icon.svelte";
  import { t } from "../lib/i18n.svelte";

  let {
    server,
    currentChannel,
    lobby = false,
    speaking,
    localMuted,
    unread,
    textChannels,
    categories = [],
    collapsedCategories = new Set(),
    onToggleCategory,
    canCreateIn = () => false,
    currentText,
    unreadText,
    mutedText = new Set(),
    mentionText = {},
    mentionVoice = {},
    listenOnly = {},
    privateChannels = {},
    recording = new Set(),
    directs = [],
    live = new Set(),
    previews = {},
    obsLive = false,
    onWatch,
    currentDirect = null,
    onOpenDirect,
    onOpenText,
    onJoin,
    onUserMenu,
    onCreate,
    canCreate = { text: false, voice: false, category: false },
    onChannelMenu,
    onDirectMenu,
    threadsOf = () => [],
    follows = new Set(),
    currentThread = null,
    onOpenThread,
    onThreadMenu,
    view,
    hiddenCount = 0,
    onShowHidden,
  }: {
    server: ServerState;
    currentChannel: number | null;
    // Root is a lobby without voice: only the channels below it are voice
    // channels, and users in root are not shown here.
    lobby?: boolean;
    speaking: ReadonlySet<number>;
    localMuted: ReadonlySet<number>;
    // Unread private messages per user name.
    unread: Record<string, number>;
    textChannels: TextChannel[];
    // Text categories (see lib/categories), those collapsed on this device,
    // and where we may create a text channel.
    categories?: readonly TextCategory[];
    collapsedCategories?: ReadonlySet<number>;
    onToggleCategory?: (id: number) => void;
    canCreateIn?: (category: number) => boolean;
    currentText: number | null;
    unreadText: ReadonlySet<number>;
    mutedText?: ReadonlySet<number>;
    // Unseen mentions of us, by text channel and by voice channel.
    mentionText?: Record<number, number>;
    mentionVoice?: Record<number, number>;
    // Listen-only voice channels (only some users may speak).
    listenOnly?: Readonly<Record<number, unknown>>;
    // Private channels by Mumble channel (a voice channel, or a text
    // channel's mirror): a lock.
    privateChannels?: Readonly<Record<number, unknown>>;
    // Voice channels being recorded: a red "REC".
    recording?: ReadonlySet<number>;
    // Live streams by id (see stream/protocol.streamId), and watching one.
    live?: ReadonlySet<number>;
    // Thumbnails of the streams (shown on the LIVE button).
    previews?: Readonly<Record<number, string>>;
    // Our own screen stream comes from OBS: no self-preview, so it may be
    // watched.
    obsLive?: boolean;
    onWatch?: (session: number) => void;
    // Stored direct chats as listed, pinned ones first (see lib/directList;
    // mentions: unread messages).
    directs?: { channel: number; peer: Peer; pinned?: boolean }[];
    currentDirect?: number | null;
    onOpenDirect?: (peer: Peer) => void;
    onOpenText: (id: number) => void;
    onJoin: (id: number) => void;
    onUserMenu: (session: number, event: MouseEvent) => void;
    // A "+" in a category (undefined: the user may not create there).
    onCreate?: (kind: ChannelKind, category?: number) => void;
    canCreate?: Record<ChannelKind, boolean>;
    // Right-click on a channel.
    onChannelMenu?: (kind: ChannelKind, id: number, event: MouseEvent) => void;
    // Right-click on a direct chat.
    onDirectMenu?: (channel: number, event: MouseEvent) => void;
    // A text channel's active threads, indented under it (those we follow
    // show unread; see lib/threads), the open one, and right-click.
    threadsOf?: (channel: number) => readonly Thread[];
    follows?: ReadonlySet<number>;
    currentThread?: number | null;
    onOpenThread?: (t: Thread) => void;
    onThreadMenu?: (id: number, event: MouseEvent) => void;
    // The text groups and voice tree as the user sees them (see
    // lib/channelList ownView; undefined: as the server orders them), and
    // the hidden channels to show again.
    view?: { groups: ReturnType<typeof textGroups>; tree: ChannelNode | null };
    hiddenCount?: number;
    onShowHidden?: () => void;
  } = $props();

  // Text channel mirrors (for Mumble clients) are not voice channels here.
  const own = $derived(
    view ??
      ownView(textGroups(textChannels, categories), withoutMirrors(channelTree(server), mirrorIds(server, textChannels, categories)), noChannelList, {
        text: () => true,
        voice: () => true,
      }),
  );
  const tree = $derived(own.tree);
  const groups = $derived(own.groups);
  // A collapsed category still shows these (see lib/categories).
  const loud = $derived(new Set([...unreadText].filter((id) => !mutedText.has(id))));
  let collapsed = $state(false);
  let directCollapsed = $state(false);
  let textCollapsed = $state(false);
  // A heading's "+" (staff; a category's moderators in it).
  const createIn = (kind: ChannelKind, id?: number, name?: string) =>
    onCreate && (id !== undefined || canCreate[kind])
      ? {
          label: name ? t("sidebar.createTextChannelIn", { category: name }) : t(kind === "voice" ? "sidebar.createVoiceChannel" : "sidebar.createTextChannel"),
          run: () => onCreate(kind, id),
        }
      : undefined;
  const INDENT = 16;
</script>

{#snippet channel(node: ChannelNode, depth: number)}
  {@const users = usersIn(server, node.channel.id)}
  <!-- Root is the lobby: its children sit at the same level, like Discord's
       flat list. Only deeper channels are indented. -->
  <li style:padding-left="{depth > 1 ? INDENT : 0}px">
    <button
      use:longPress
      class="channel hover-row"
      class:current={node.channel.id === currentChannel}
      aria-current={node.channel.id === currentChannel ? "page" : undefined}
      onclick={() => onJoin(node.channel.id)}
      oncontextmenu={(e) => {
        if (!onChannelMenu) return;
        e.preventDefault();
        onChannelMenu("voice", node.channel.id, e);
      }}
      title={htmlToText(node.channel.description) || undefined}
    >
      {#if node.channel.id in listenOnly}
        <Icon name="headphones" size={20} /><span class="sr-only">{t("sidebar.listenOnly")}</span>
      {:else}
        <Icon name="speaker" size={20} />
      {/if}
      <span class="name ellipsis">{node.channel.name}</span>
      {#if node.channel.id in privateChannels}{@render lock()}{/if}
      {#if recording.has(node.channel.id)}<span class="rec" title={t("sidebar.recordingNow")}>{t("sidebar.recBadge")}</span>{/if}
      {#if node.channel.id !== currentChannel}<Badge count={mentionVoice[node.channel.id]} label="mentions" />{/if}
    </button>
    {#if users.length}
      <ul class="users">
        {#each users as user (user.session)}
          <li>
            <button
              class="user hover-row"
              class:me={user.session === server.session}
              onclick={(e) => onUserMenu(user.session, e)}
              oncontextmenu={(e) => onUserMenu(user.session, e)}
            >
              <Avatar name={user.name} size={24} speaking={speaking.has(user.session)} />
              <span class="name ellipsis"><UserName name={user.name} /></span>
              <span class="flags">
                {#if localMuted.has(user.session)}
                  <span title={t("sidebar.mutedForYou")}>
                    <Icon name="speaker" off size={16} /><span class="sr-only">{t("sidebar.mutedForYou")}</span>
                  </span>
                {/if}
                {#if user.selfMute || user.mute || user.suppress}
                  <span title={t("sidebar.muted")}>
                    <Icon name="mic" off size={16} /><span class="sr-only">{t("sidebar.muted")}</span>
                  </span>
                {/if}
                {#if user.selfDeaf || user.deaf}
                  <span title={t("sidebar.deafened")}>
                    <Icon name="headphones" off size={16} /><span class="sr-only">{t("sidebar.deafened")}</span>
                  </span>
                {/if}
                <Badge count={unread[user.name]} />
              </span>
            </button>
            {#each [streamId(user.session, false), streamId(user.session, true)].filter((id) => live.has(id)) as id (id)}
              {@const camera = isCamera(id)}
              {#if user.session === server.session && !(obsLive && !camera)}
                <span class="live" title={t(camera ? "sidebar.sharingCamera" : "sidebar.sharingScreen")}>{@render badge(id)}</span>
              {:else}
                {@const watch = t(camera ? "sidebar.watchCamera" : "sidebar.watchScreen", { name: user.name })}
                <span class="live-wrap">
                  <button class="live" title={watch} aria-label={watch} onclick={() => onWatch?.(id)}>{@render badge(id)}</button>
                  {#if previews[id]}
                    <img class="live-preview" src={previews[id]} alt="" />
                  {/if}
                </span>
              {/if}
            {/each}
          </li>
        {/each}
      </ul>
    {/if}
    {#if node.children.length}
      <ul>
        {#each node.children as child (child.channel.id)}
          {@render channel(child, depth + 1)}
        {/each}
      </ul>
    {/if}
  </li>
{/snippet}

{#snippet badge(id: number)}
  {#if isCamera(id)}<Icon name="camera" size={14} />{:else}{t("sidebar.liveBadge")}{/if}
{/snippet}

{#snippet lock()}
  <span class="lock" title={t("sidebar.privateChannel")}><Icon name="lock" size={14} /><span class="sr-only">{t("sidebar.privateMark")}</span></span>
{/snippet}

{#snippet category(label: string, isCollapsed: boolean, toggle: () => void, create?: { label: string; run: () => void }, menu?: (e: MouseEvent) => void, locked = false)}
  <div class="category-row">
    <button
      use:longPress
      class="category"
      aria-expanded={!isCollapsed}
      onclick={toggle}
      oncontextmenu={(e) => {
        if (!menu) return;
        e.preventDefault();
        menu(e);
      }}
    >
      <svg class="chevron" class:collapsed={isCollapsed} width="12" height="12" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="3" />
      </svg>
      {label}
      {#if locked}{@render lock()}{/if}
    </button>
    {#if create}
      <button class="add" title={t("sidebar.createChannel")} aria-label={create.label} onclick={create.run}>
        <Icon name="plus" size={16} />
      </button>
    {/if}
  </div>
{/snippet}

{#snippet textList(channels: TextChannel[])}
  <ul class="text-channels">
    {#each channels as ch (ch.id)}
      <li>
        <button
          use:longPress
          class="channel hover-row"
          class:current={ch.id === currentText}
          class:unread={unreadText.has(ch.id) && ch.id !== currentText}
          class:muted={mutedText.has(ch.id)}
          aria-current={ch.id === currentText ? "page" : undefined}
          onclick={() => onOpenText(ch.id)}
          oncontextmenu={(e) => {
            if (!onChannelMenu) return;
            e.preventDefault();
            onChannelMenu("text", ch.id, e);
          }}
        >
          <Icon name="hash" size={20} />
          <span class="name ellipsis">{ch.name}</span>
          {#if isLocked({ private: privateChannels, channels: textChannels, categories }, ch)}{@render lock()}{/if}
          {#if unreadText.has(ch.id) && ch.id !== currentText}
            <span class="sr-only">{t("sidebar.unreadMark")}</span>
          {/if}
          {#if ch.id !== currentText}<Badge count={mentionText[ch.id]} label="mentions" />{/if}
        </button>
        {#if threadsOf(ch.id).length}
          <ul class="threads">
            {#each threadsOf(ch.id) as th (th.id)}
              {@const here = th.id === currentThread}
              <li>
                <button
                  use:longPress
                  class="channel thread hover-row"
                  class:current={here}
                  class:unread={unreadText.has(th.id) && !here}
                  class:muted={mutedText.has(th.id) || !follows.has(th.id)}
                  aria-current={here ? "page" : undefined}
                  onclick={() => onOpenThread?.(th)}
                  oncontextmenu={(e) => {
                    if (!onThreadMenu) return;
                    e.preventDefault();
                    onThreadMenu(th.id, e);
                  }}
                >
                  <Icon name="thread" size={16} />
                  <span class="name ellipsis">{th.name}</span>
                  {#if unreadText.has(th.id) && !here}<span class="sr-only">{t("sidebar.unreadMark")}</span>{/if}
                  {#if !here}<Badge count={mentionText[th.id]} label="mentions" />{/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </li>
    {/each}
  </ul>
{/snippet}

<nav aria-label={t("sidebar.channels")}>
  {#if groups[0]!.channels.length || canCreate.text}
    {@render category(t("sidebar.textChannels"), textCollapsed, () => (textCollapsed = !textCollapsed), createIn("text"))}
    {#if !textCollapsed}{@render textList(groups[0]!.channels)}{/if}
  {/if}
  {#each groups.slice(1) as g (g.category!.id)}
    {@const id = g.category!.id}
    {@const shut = collapsedCategories.has(id)}
    {@render category(
      g.category!.name,
      shut,
      () => onToggleCategory?.(id),
      canCreateIn(id) ? createIn("text", id, g.category!.name) : undefined,
      onChannelMenu && ((e) => onChannelMenu("category", id, e)),
      g.category!.mumble !== undefined && g.category!.mumble in privateChannels,
    )}
    {@render textList(shut ? shownCollapsed(g.channels, currentText, loud) : g.channels)}
  {/each}
  {@render category(t("sidebar.voiceChannels"), collapsed, () => (collapsed = !collapsed), createIn("voice"))}
  {#if tree && !collapsed}
    {#if lobby}
      <ul>
        {#each tree.children as child (child.channel.id)}
          {@render channel(child, 1)}
        {/each}
      </ul>
    {:else}
      <ul>{@render channel(tree, 0)}</ul>
    {/if}
  {/if}
  {#if onShowHidden && hiddenCount}
    <button type="button" class="hidden-channels" onclick={onShowHidden}>{t("sidebar.hiddenChannels", { count: hiddenCount })}</button>
  {/if}
  {#if directs.length && onOpenDirect}
    {@render category(t("sidebar.directMessages"), directCollapsed, () => (directCollapsed = !directCollapsed))}
    {#if !directCollapsed}
      <ul class="text-channels">
        {#each directs as d (d.channel)}
          <li>
            <button
              use:longPress
              class="channel hover-row"
              class:current={d.channel === currentDirect}
              class:unread={!!mentionText[d.channel] && d.channel !== currentDirect}
              class:muted={mutedText.has(d.channel)}
              aria-current={d.channel === currentDirect ? "page" : undefined}
              onclick={() => onOpenDirect(d.peer)}
              oncontextmenu={(e) => {
                if (!onDirectMenu) return;
                e.preventDefault();
                onDirectMenu(d.channel, e);
              }}
            >
              <Avatar name={d.peer.name} registered size={20} />
              <span class="name ellipsis"><UserName name={d.peer.name} /></span>
              {#if d.pinned}<span class="pinned" title={t("sidebar.pinned")}><Icon name="pin" size={14} /><span class="sr-only">{t("sidebar.pinnedMark")}</span></span>{/if}
              {#if d.channel !== currentDirect}<Badge count={mentionText[d.channel]} label="unread messages" />{/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</nav>

<style>
  nav {
    flex: 1;
    overflow-y: auto;
    padding: 12px 8px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .category-row {
    display: flex;
    align-items: center;
  }
  .category-row .category {
    flex: 1;
  }
  .add {
    display: grid;
    place-items: center;
    padding: 0 4px 2px;
    color: var(--text-muted);
  }
  .add:hover {
    color: var(--text);
  }
  .category {
    display: flex;
    align-items: center;
    gap: 4px;
    width: 100%;
    padding: 4px 4px 6px 2px;
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 600;
  }
  .category:hover {
    color: var(--text);
  }
  .chevron {
    transition: transform 0.15s;
  }
  .hidden-channels {
    margin: 4px 8px;
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 13px;
    color: var(--text-muted);
    text-align: left;
  }
  .hidden-channels:hover {
    background: var(--bg-hover);
    color: var(--text);
  }
  .chevron.collapsed {
    transform: rotate(-90deg);
  }
  .channel {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    /* A long press opens the channel menu, not the phone's text menu. */
    -webkit-touch-callout: none;
    user-select: none;
    height: 34px;
    margin: 1px 0;
    padding: 0 8px;
    border-radius: 6px;
    color: var(--text-muted);
    font-size: 15px;
    font-weight: 500;
    text-align: left;
  }
  .channel.current {
    background: var(--bg-active);
    color: var(--text-strong);
  }
  /* New messages: bright and bold, like Discord. */
  .channel.unread {
    color: var(--text-strong);
    font-weight: 600;
  }
  .channel.muted:not(.current) {
    opacity: 0.5;
  }
  .threads {
    margin: 0;
    padding: 0 0 0 20px;
    list-style: none;
  }
  .channel.thread {
    font-size: 14px;
  }
  .text-channels {
    margin-bottom: 16px;
  }
  .users {
    margin: 0 0 4px 28px;
  }
  .user {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    height: 32px;
    padding: 0 8px;
    border-radius: 6px;
    color: var(--text-muted);
    font-size: 14px;
    text-align: left;
  }
  .user.me {
    color: var(--text);
  }
  .name {
    flex: 1;
  }
  .rec {
    flex-shrink: 0;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--danger);
    color: white;
    font-size: 10px;
    font-weight: 700;
  }
  .lock,
  .pinned {
    display: flex;
    flex-shrink: 0;
    color: var(--text-muted);
  }
  .flags {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--text-faint);
  }
  .flags span {
    display: inline-flex;
  }
  .users li:has(.live) {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .users li:has(.live) .user {
    flex: 1;
    min-width: 0;
  }
  .live-wrap {
    position: relative;
    flex: none;
    margin-right: 8px;
  }
  .live-preview {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 20;
    display: none;
    width: 200px;
    border-radius: 6px;
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.5);
    pointer-events: none;
  }
  .live-wrap:hover .live-preview,
  .live-wrap:focus-within .live-preview {
    display: block;
  }
  span.live {
    margin-right: 8px;
  }
  .live {
    display: inline-flex;
    align-items: center;
    flex: none;
    padding: 0 5px;
    border-radius: 3px;
    background: var(--danger);
    color: #fff;
    font-size: 10px;
    font-weight: 700;
    line-height: 16px;
    cursor: pointer;
  }
</style>
