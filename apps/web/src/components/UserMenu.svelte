<script lang="ts">
  import UserName from "./UserName.svelte";
  import type { Role } from "../lib/roles";
  import type { User } from "@kallisti/protocol";
  import type { UserStatus } from "../lib/presence";
  import Emoji from "./Emoji.svelte";
  import { MAX_VOLUME, type UserAudio } from "../lib/userAudio";
  import { untilText, type ModerationAction, type UserModeration } from "../lib/moderation";
  import Avatar from "./Avatar.svelte";
  import RoleTag from "./RoleTag.svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import { t } from "../lib/i18n.svelte";
  import { htmlToLines } from "../lib/htmlText";
  import { untrack } from "svelte";

  let {
    user,
    byId = false,
    isMe,
    audio,
    x,
    y,
    onAudio,
    blocked,
    onBlock,
    onMessage,
    friend,
    onSettings,
    role = null,
    status = null,
    moderator = null,
    onModerator,
    admin = null,
    onAdmin,
    moderation = null,
    timedOutUntil = null,
    channelTimeout = null,
    onModerate,
    onConfirm,
    onDeleteMessages,
    fediHandle,
    onClose,
  }: {
    user: Pick<User, "name" | "comment" | "mute" | "deaf">;
    // A member we do not see (offline, or in a channel hidden from us): no
    // voice items (volume, mute).
    byId?: boolean;
    isMe: boolean;
    audio: UserAudio;
    x: number;
    y: number;
    onAudio: (patch: Partial<UserAudio>) => void;
    // Their text messages hidden for me (see lib/blocked).
    blocked: boolean;
    onBlock: () => void;
    // None: we cannot write to them (an offline user, we are a guest).
    onMessage?: () => void;
    // A registered user: in my friends list (see lib/friends), or not.
    friend?: { is: boolean; onToggle: () => void };
    onSettings: () => void;
    role?: Role | null;
    status?: UserStatus | null;
    // What we may do about the user's role (see moderatorAction,
    // adminAction).
    moderator?: "make" | "remove" | null;
    onModerator?: (on: boolean) => void;
    admin?: "make" | "remove" | null;
    onAdmin?: (on: boolean) => void;
    // What we may do to the user (see userModeration): onModerate acts at
    // once, onConfirm asks first (kick, ban, time-out).
    moderation?: UserModeration | null;
    timedOutUntil?: number | null;
    // Where we may give a channel time-out (see moderation.channelTimeout),
    // and when the user's there ends (null: none).
    channelTimeout?: { name: string; until: number | null } | null;
    onModerate?: (a: ModerationAction) => void;
    onConfirm?: (kind: "kick" | "ban" | "timeout" | "channelTimeout") => void;
    // Asks before deleting their recent messages (staff; offline too).
    onDeleteMessages?: () => void;
    // Their verified fediverse handle, if they show it ("": none).
    fediHandle?: (name: string) => Promise<string>;
    onClose: () => void;
  } = $props();

  let handle = $state("");
  $effect(() => {
    const name = user.name;
    let current = true;
    handle = "";
    void untrack(() => fediHandle)?.(name).then((h) => current && (handle = h)); // per name, not per new prop function
    return () => (current = false);
  });

  // The comment as text, never as HTML.
  const bio = $derived(htmlToLines(user.comment));

  const act = (fn: () => void) => () => {
    fn();
    onClose();
  };
</script>

<ContextMenu {x} {y} label={t("sidebar.actionsFor", { name: user.name })} {onClose}>
  <div class="who">
    <Avatar name={user.name} size={32} status={status?.presence} registered={byId} />
    <span class="names">
      <span class="line"><span><UserName name={user.name} /></span><RoleTag {role} /></span>
      {#if handle}
        <span class="handle" title={t("sidebar.verifiedHandle")}><span aria-hidden="true">✓</span> {handle}</span>
      {/if}
      {#if status?.custom}
        <span class="custom"><Emoji text={status.custom.emoji} /> {status.custom.text}</span>
      {/if}
    </span>
  </div>
  {#if bio}<p class="bio">{bio}</p>{/if}
  <div class="sep"></div>
  {#if isMe}
    <button onclick={act(onSettings)}>{t("common.settings")}</button>
  {:else}
    {#if onMessage || friend}
      {#if onMessage}<button onclick={act(onMessage)}>{t("sidebar.message")}</button>{/if}
      {#if friend}<button onclick={act(friend.onToggle)}>{t(friend.is ? "sidebar.removeFriend" : "sidebar.addFriend")}</button>{/if}
      <div class="sep"></div>
    {/if}
    {#if !byId}
      <label class="volume">
        <span class="row">
          {t("sidebar.userVolume")} <span class="value">{Math.round(audio.volume * 100)}%</span>
        </span>
        <input
          type="range"
          min="0"
          max={MAX_VOLUME * 100}
          step="5"
          value={Math.round(audio.volume * 100)}
          oninput={(e) => onAudio({ volume: Number(e.currentTarget.value) / 100 })}
          aria-valuetext="{Math.round(audio.volume * 100)}%"
        />
      </label>
      <button
        role="switch"
        aria-checked={audio.muted}
        onclick={() => onAudio({ muted: !audio.muted })}
      >
        {t("sidebar.muteUser")}
        <span class="box" class:on={audio.muted} aria-hidden="true"></span>
      </button>
    {/if}
    <button role="switch" aria-checked={blocked} onclick={onBlock}>
      {t("sidebar.blockMessages")}
      <span class="box" class:on={blocked} aria-hidden="true"></span>
    </button>
    {#if (moderator && onModerator) || (admin && onAdmin)}
      <div class="sep"></div>
    {/if}
    {#if moderator && onModerator}
      <button onclick={act(() => onModerator(moderator === "make"))}>
        {t(moderator === "make" ? "sidebar.makeModerator" : "sidebar.removeModerator")}
      </button>
    {/if}
    {#if admin && onAdmin}
      <button onclick={act(() => onAdmin(admin === "make"))}>
        {t(admin === "make" ? "sidebar.makeAdmin" : "sidebar.removeAdmin")}
      </button>
    {/if}
    {#if timedOutUntil !== null}
      <p class="note">{t("sidebar.timedOutUntil", { time: untilText(timedOutUntil) })}</p>
    {/if}
    {#if channelTimeout?.until != null}
      <p class="note">{t("sidebar.timedOutInUntil", { channel: channelTimeout.name, time: untilText(channelTimeout.until) })}</p>
    {/if}
    {#if moderation && onModerate && onConfirm}
      <div class="sep"></div>
      {#if moderation.mute}
        <!-- A time-out keeps the user muted. -->
        <button role="switch" aria-checked={user.mute} disabled={timedOutUntil !== null} onclick={() => onModerate({ kind: "mute", on: !user.mute })}>
          {t("sidebar.serverMute")}
          <span class="box" class:on={user.mute} aria-hidden="true"></span>
        </button>
        <button role="switch" aria-checked={user.deaf} onclick={() => onModerate({ kind: "deaf", on: !user.deaf })}>
          {t("sidebar.serverDeafen")}
          <span class="box" class:on={user.deaf} aria-hidden="true"></span>
        </button>
      {/if}
      {#if moderation.moveTo.length}
        <label class="volume">
          {t("sidebar.moveTo")}
          <select value="" onchange={(e) => act(() => onModerate({ kind: "move", channel: Number(e.currentTarget.value) }))()}>
            <option value="" disabled>{t("sidebar.chooseVoiceChannel")}</option>
            {#each moderation.moveTo as c (c.id)}<option value={String(c.id)}>{c.name}</option>{/each}
          </select>
        </label>
      {/if}
      {#if moderation.disconnect}
        <button onclick={act(() => onModerate({ kind: "move", channel: 0 }))}>{t("sidebar.disconnectVoice")}</button>
      {/if}
      {#if moderation.channelTimeout !== null && channelTimeout}
        {#if channelTimeout.until !== null}
          <button onclick={act(() => onModerate({ kind: "channelTimeout", channel: moderation.channelTimeout!, seconds: 0 }))}>
            {t("sidebar.removeTimeoutIn", { channel: channelTimeout.name })}
          </button>
        {:else}
          <button class="danger" onclick={act(() => onConfirm("channelTimeout"))}>{t("sidebar.timeOutIn", { channel: channelTimeout.name })}</button>
        {/if}
      {/if}
      {#if moderation.timeout}
        {#if timedOutUntil !== null}
          <button onclick={act(() => onModerate({ kind: "timeout", seconds: 0 }))}>{t("sidebar.removeTimeout")}</button>
        {:else}
          <button class="danger" onclick={act(() => onConfirm("timeout"))}>{t("sidebar.timeOut")}</button>
        {/if}
      {/if}
      {#if moderation.kick}
        <button class="danger" onclick={act(() => onConfirm("kick"))}>{t("sidebar.kick")}</button>
      {/if}
      {#if moderation.ban}
        <button class="danger" onclick={act(() => onConfirm("ban"))}>{t("sidebar.ban")}</button>
      {/if}
    {/if}
    {#if onDeleteMessages}
      {#if !moderation}<div class="sep"></div>{/if}
      <button class="danger" onclick={act(onDeleteMessages)}>{t("sidebar.deleteMessages")}</button>
    {/if}
  {/if}
</ContextMenu>

<style>
  .who {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 8px;
    font-weight: 600;
  }
  /* Long ones (from Mumble apps) scroll. */
  .bio {
    max-width: 240px;
    max-height: 120px;
    overflow-y: auto;
    margin: 0;
    padding: 0 8px 6px;
    font-size: 13px;
    white-space: pre-line;
    overflow-wrap: anywhere;
    color: var(--text-muted);
  }
  .names {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .line {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .handle {
    max-width: 220px;
    font-size: 13px;
    font-weight: 400;
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }
  .handle span {
    color: var(--online);
    font-weight: 700;
  }
  .custom {
    max-width: 220px;
    font-size: 13px;
    font-weight: 400;
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }
  .sep {
    height: 1px;
    margin: 4px 6px;
    background: var(--border-subtle);
  }
  button {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    text-align: left;
    color: var(--text-muted);
    cursor: pointer;
  }
  button:hover {
    background: var(--accent-strong);
    color: white;
  }
  .volume {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .row {
    display: flex;
    justify-content: space-between;
  }
  .value {
    color: var(--text);
  }
  input[type="range"] {
    accent-color: var(--accent);
  }
  select {
    padding: 6px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    font-size: 14px;
    text-transform: none;
  }
  .note {
    margin: 0;
    padding: 4px 8px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .danger {
    color: var(--danger);
  }
  .danger:hover {
    background: var(--danger);
  }
  .box {
    width: 18px;
    height: 18px;
    border: 2px solid var(--text-muted);
    border-radius: 4px;
  }
  .box.on {
    border-color: var(--accent);
    background: var(--accent-strong)
      url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='white' stroke-width='3'%3E%3Cpath d='M5 12l5 5 9-10'/%3E%3C/svg%3E")
      center / 14px no-repeat;
  }
  button:hover .box {
    border-color: white;
  }
</style>
