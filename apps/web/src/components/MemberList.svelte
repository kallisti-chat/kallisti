<script lang="ts">
  import UserName from "./UserName.svelte";
  import type { Badge as Tag } from "../lib/roles";
  import type { UserStatus } from "../lib/presence";
  import type { User } from "@kallisti/protocol";
  import type { Peer } from "../lib/textChannels";
  import { onlineRows, readerGroupLabel, readerRow, withoutFriends, type OnlineMember, type ReaderGroup, type ReaderRow } from "../lib/members";
  import type { FriendRow } from "../lib/friends";
  import Avatar from "./Avatar.svelte";
  import Badge from "./Badge.svelte";
  import Emoji from "./Emoji.svelte";
  import RoleTag from "./RoleTag.svelte";
  import { t } from "../lib/i18n.svelte";

  let {
    here,
    elsewhere,
    channelName,
    unread,
    speaking,
    onUserMenu,
    offline = [],
    hidden = [],
    readers = null,
    voiceFirst = false,
    onMemberMenu = () => {},
    roleOf = () => null,
    statusOf = () => ({ presence: "online", custom: null }),
    inert = false,
    friends = [],
    friendsHere = "",
    onFriend = () => {},
    collapsed = [],
    onToggleGroup,
  }: {
    here: User[];
    elsewhere: User[];
    channelName: string;
    // Unread private messages per user name.
    unread: Record<string, number>;
    speaking: ReadonlySet<number>;
    onUserMenu: (session: number, event: MouseEvent) => void;
    // Registered users who are offline, and members online in channels we
    // cannot see (see lib/members): their menu goes by user id.
    offline?: Peer[];
    hidden?: OnlineMember[];
    // In a private channel: who reads it, instead of the groups above.
    readers?: ReaderGroup<User>[] | null;
    // A voice channel's view: who is in it stays on top of the readers.
    voiceFirst?: boolean;
    onMemberMenu?: (userId: number, event: MouseEvent) => void;
    roleOf?: (user: Pick<User, "userId">) => Tag | null;
    statusOf?: (session: number) => UserStatus;
    inert?: boolean;
    // My friends from all my servers (see lib/friends), on top; those of
    // this server (its origin: friendsHere) without the server's name.
    friends?: readonly FriendRow[];
    friendsHere?: string;
    onFriend?: (row: FriendRow, event: MouseEvent) => void;
    // Collapsed groups by key (see lib/memberGroups); undefined: none
    // collapse.
    collapsed?: readonly string[];
    onToggleGroup?: (key: string) => void;
  } = $props();
  const shut = (key: string) => collapsed.includes(key);
  // This server's friends show only in the Friends group.
  const friendIds = $derived(new Set(friends.filter((r) => r.friend.origin === friendsHere).map((r) => r.friend.user)));
  const own = (list: ReaderRow<User>[]) => withoutFriends(list, friendIds);
</script>

<!-- Offline users: faded, no status (Discord hides their custom one). -->
{#snippet row(name: string, role: Tag | null, status: UserStatus | null, speaking: boolean, onMenu: (e: MouseEvent) => void)}
  <li>
    <button class="hover-row" class:offline={!status} onclick={onMenu} oncontextmenu={onMenu}>
      <Avatar {name} size={32} status={status?.presence} {speaking} registered={!status} />
      <span class="who">
        <span class="line">
          <span class="ellipsis"><UserName {name} /></span>
          <RoleTag {role} />
        </span>
        {#if status?.custom}
          <span class="custom ellipsis" title={status.custom.text}><Emoji text={status.custom.emoji} lazy /> {status.custom.text}</span>
        {/if}
      </span>
      <span class="unread"><Badge count={unread[name]} /></span>
    </button>
  </li>
{/snippet}

<!-- A group's heading: a button that collapses it. -->
{#snippet heading(key: string, title: string, count: number)}
  <h2>
    {#if onToggleGroup}
      <button class="group" aria-expanded={!shut(key)} onclick={() => onToggleGroup(key)}>
        <svg class="chevron" class:shut={shut(key)} width="12" height="12" viewBox="0 0 24 24" aria-hidden="true"><path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" /></svg>
        {t("sidebar.groupHeading", { title, count })}
      </button>
    {:else}
      {t("sidebar.groupHeading", { title, count })}
    {/if}
  </h2>
{/snippet}

<!-- A group: users we see, members online in channels we cannot see
     (without a channel), offline members. -->
{#snippet rows(key: string, title: string, list: ReaderRow<User>[])}
  {#if list.length}
    {@render heading(key, title, list.length)}
    {#if !shut(key)}
    <ul>
      {#each list as r (readerRow(r).key)}
        {#if "user" in r}
          {@const user = r.user}
          {@render row(user.name, roleOf(user), statusOf(user.session), speaking.has(user.session), (e) => onUserMenu(user.session, e))}
        {:else if "member" in r}
          {@const p = r.member}
          {@render row(p.name, roleOf({ userId: p.id }), statusOf(p.session), false, (e) => onMemberMenu(p.id, e))}
        {:else}
          {@const p = r.offline}
          {@render row(p.name, roleOf({ userId: p.id }), null, false, (e) => onMemberMenu(p.id, e))}
        {/if}
      {/each}
    </ul>
    {/if}
  {/if}
{/snippet}

<!-- Friends: online ones first; where they are below the name. -->
{#snippet friendRows()}
  {#if friends.length}
    {@render heading("friends", t("sidebar.friends"), friends.length)}
    {#if !shut("friends")}
      <ul aria-label={t("sidebar.friends")}>
        {#each friends as r (`${r.friend.origin} ${r.friend.user}`)}
          {@const elsewhere = r.friend.origin !== friendsHere}
          {@const where = [elsewhere ? (r.server ?? new URL(r.friend.origin).host) : "", r.voice ? t("sidebar.friendInVoice", { channel: r.voice }) : ""].filter(Boolean).join(" · ")}
          <li>
            <button class="hover-row" class:offline={!r.online} onclick={(e) => onFriend(r, e)} oncontextmenu={(e) => onFriend(r, e)}>
              <Avatar name={r.friend.name} size={32} status={r.online ? "online" : undefined} registered={!r.online} />
              <span class="who">
                <span class="line"><span class="ellipsis"><UserName name={r.friend.name} /></span></span>
                {#if where}<span class="custom ellipsis">{where}</span>{/if}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
{/snippet}

<aside aria-label={t("sidebar.members")} {inert}>
  {@render friendRows()}
  {#if readers}
    {#if voiceFirst}{@render rows("here", t("sidebar.inChannel", { channel: channelName }), own(onlineRows(here, [])))}{/if}
    {#each readers as g (g.title)}
      {@render rows(`readers ${g.title}`, readerGroupLabel(g.title), own(g.rows))}
    {/each}
  {:else}
    {@render rows("here", t("sidebar.inChannel", { channel: channelName }), own(onlineRows(here, [])))}
    {@render rows("online", t("sidebar.online"), own(onlineRows(elsewhere, hidden)))}
    {@render rows("offline", t("sidebar.offline"), own(offline.map((p) => ({ offline: p }))))}
  {/if}
</aside>

<style>
  aside {
    --avatar-cutout: var(--bg-main);
    overflow-y: auto;
    padding: 12px 8px;
    background: var(--bg-main);
  }
  h2 {
    margin: 20px 8px 4px;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  h2:first-child {
    margin-top: 8px;
  }
  .group {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    color: inherit;
  }
  .group:hover {
    color: var(--text);
  }
  .chevron {
    transition: transform 0.1s;
  }
  .chevron.shut {
    transform: rotate(-90deg);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .hover-row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 42px;
    margin: 1px 0;
    padding: 0 8px;
    border-radius: 6px;
    color: var(--text-muted);
    font-size: 15px;
    font-weight: 500;
    text-align: left;
  }
  .hover-row:hover {
    --avatar-cutout: var(--bg-hover);
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .line {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .custom {
    font-size: 12px;
    font-weight: 400;
  }
  .unread {
    margin-left: auto;
  }
  .offline {
    opacity: 0.4;
  }
  .offline:hover {
    opacity: 1;
  }
</style>
