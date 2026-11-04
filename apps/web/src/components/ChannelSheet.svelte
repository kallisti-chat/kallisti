<script lang="ts">
  import { tick } from "svelte";
  import BottomSheet from "./BottomSheet.svelte";
  import Icon from "./Icon.svelte";
  import { t } from "../lib/i18n.svelte";
  import { isMuted, levelOf, LEVELS, MUTE_FOR, mutedUntilText, muteUntil, type ChannelPref } from "../lib/channelPrefs";

  // Phones: what the chat header has on wide screens (topic, pins,
  // threads) and the channel's notification settings (as in ChannelMenu;
  // direct chats: only mute) and link. A tap on the name opens it.
  let {
    title,
    kind,
    topic = "",
    onPins,
    onThreads,
    onCopyLink,
    onCopyInvite,
    notify,
    onNotify,
    onClose,
  }: {
    title: string;
    kind: "channel" | "text" | "dm";
    topic?: string;
    onPins?: () => void;
    onThreads?: () => void;
    onCopyLink?: () => void;
    // A one-use invite link, valid 7 days (who manages a private channel).
    onCopyInvite?: () => void;
    // desktop: desktop notifications are on; direct: a direct chat.
    notify?: { pref: ChannelPref; desktop: boolean; direct?: boolean };
    onNotify?: (patch: ChannelPref) => void;
    onClose: () => void;
  } = $props();

  const id = $props.id();
  let sheet: BottomSheet | undefined = $state();
  let muting = $state(false);
  const dm = $derived(kind === "dm");
  // Focus back on the name first: the action may move it on (a panel).
  const act = (fn: () => void) => () => {
    sheet?.dismiss();
    fn();
  };
  // The content changes under the focus (mute choices, unmute): focus
  // stays in the sheet.
  const showMuting = async (on: boolean) => {
    muting = on;
    await tick();
    sheet?.focus();
  };
  const actions = $derived(
    [
      onPins && { label: t("sidebar.pinnedMessages"), icon: "pin" as const, run: onPins },
      onThreads && { label: t("sidebar.threads"), icon: "thread" as const, run: onThreads },
      onCopyLink && { label: t("sidebar.copyLink"), icon: "copy" as const, run: onCopyLink },
      onCopyInvite && { label: t("sidebar.copyInviteOneUse"), icon: "copy" as const, run: onCopyInvite },
    ].filter((a) => !!a),
  );
</script>

<BottomSheet label={t("sidebar.channelDetails", { name: `${kind === "dm" ? "@" : kind === "text" ? "#" : ""}${title}` })} {onClose} bind:this={sheet}>
  {#if muting && notify && onNotify}
    <h2>{t(dm ? "sidebar.muteConversation" : "sidebar.muteChannel")}</h2>
    <ul class="sheet-list">
      {#each MUTE_FOR as [label, minutes] (minutes)}
        <li>
          <button type="button" class="sheet-item" onclick={() => (onNotify({ mutedUntil: muteUntil(minutes, Date.now()) }), showMuting(false))}
            >{t(label)}</button
          >
        </li>
      {/each}
    </ul>
    <button type="button" class="sheet-item cancel" onclick={() => showMuting(false)}>{t("common.cancel")}</button>
  {:else}
    <h2>
      {#if kind === "dm"}<span aria-hidden="true">@</span>{:else}<Icon name={kind === "text" ? "hash" : "speaker"} size={20} />{/if}
      <span class="ellipsis">{title}</span>
    </h2>
    {#if topic}<p class="topic">{topic}</p>{/if}
    {#if actions.length}
      <ul class="sheet-list">
        {#each actions as a (a.label)}
          <li>
            <button type="button" class="sheet-item" onclick={act(a.run)}><Icon name={a.icon} size={20} />{a.label}</button>
          </li>
        {/each}
      </ul>
    {/if}
    {#if notify && onNotify}
      {@const muted = isMuted(notify.pref, Date.now())}
      {@const end = muted ? mutedUntilText(notify.pref.mutedUntil) : null}
      <h3 id="{id}-notify">{t("sidebar.notifications")}</h3>
      <ul class="sheet-list" aria-labelledby="{id}-notify">
        <li>
          {#if muted}
            <button type="button" class="sheet-item" onclick={() => (onNotify({ mutedUntil: undefined }), showMuting(false))}>
              <Icon name="bell" size={20} /><span>{t(dm ? "sidebar.unmuteConversation" : "sidebar.unmuteChannel")}{#if end}<small>{t("sidebar.mutedUntil", { time: end })}</small>{/if}</span>
            </button>
          {:else}
            <button type="button" class="sheet-item" onclick={() => showMuting(true)}><Icon name="bell" off size={20} />{t(dm ? "sidebar.muteConversation" : "sidebar.muteChannel")}</button>
          {/if}
        </li>
        {#if !notify.direct}
          {#each LEVELS as [level, label] (level)}
            <li>
              <button type="button" class="sheet-item choice" aria-pressed={levelOf(notify.pref) === level} onclick={() => onNotify({ level })}
                >{t(label)}</button
              >
            </li>
          {/each}
        {/if}
      </ul>
      {#if !notify.direct && !notify.desktop}<p class="note">{t("sidebar.desktopNotificationsOff")}</p>{/if}
    {/if}
  {/if}
</BottomSheet>

<style>
  h2 {
    display: flex;
    gap: 8px;
    align-items: center;
    margin: 0 4px 8px;
    font-size: 18px;
    font-weight: 600;
  }
  h2 :global(svg),
  h2 > span[aria-hidden] {
    flex-shrink: 0;
    color: var(--text-faint);
  }
  h3 {
    margin: 16px 4px 8px;
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .topic {
    margin: 0 4px 12px;
    color: var(--text-muted);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .topic + .sheet-list,
  h2 + .sheet-list {
    margin-top: 4px;
  }
  small {
    display: block;
    font-size: 13px;
    color: var(--text-muted);
  }
  .choice[aria-pressed="true"]::after {
    content: "✓" / "";
    margin-left: auto;
    color: var(--accent);
  }
  .cancel {
    justify-content: center;
    margin-top: 8px;
    border-radius: 8px;
    background: var(--bg-input);
  }
  .note {
    margin: 8px 4px 0;
    font-size: 13px;
    color: var(--text-muted);
  }
</style>
