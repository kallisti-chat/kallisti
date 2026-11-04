<script lang="ts">
  import Icon from "./Icon.svelte";
  import { t } from "../lib/i18n.svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import ConnectionInfo from "./ConnectionInfo.svelte";
  import type { ConnectionInfo as Info } from "../lib/connectionInfo.svelte";
  import type { GoLiveAction, GoLiveEntry } from "../lib/stream/goLive";

  // Above the user panel while in a voice channel, as in Discord: where you
  // are, going live, and the button to leave.
  // entries: the "Go live" menu (see goLiveEntries; none: no button), live:
  // one of our streams runs. music: music mode is on (see
  // Settings.musicMode). info: the connection's quality (see ConnectionInfo).
  // onJoinHere: we are text-only, our account is in voice on another
  // device in the channel; the bar offers to move voice here instead.
  let {
    channel,
    onLeave,
    music = false,
    recording = false,
    live = false,
    entries = [],
    onGoLive,
    info,
    onJoinHere,
    onShow,
  }: {
    channel: string;
    onLeave: () => void;
    music?: boolean;
    // The channel is being recorded.
    recording?: boolean;
    live?: boolean;
    entries?: GoLiveEntry[];
    onGoLive?: (action: GoLiveAction) => void;
    info: Info;
    onJoinHere?: () => void;
    // Voice is on another server (several servers in one client): the
    // channel's name leads there.
    onShow?: () => void;
  } = $props();

  let menu = $state<{ x: number; y: number } | null>(null);
  let liveButton: HTMLButtonElement | undefined = $state();
  const choose = (action: GoLiveAction) => {
    menu = null;
    onGoLive?.(action);
  };
</script>

<section class="bar" aria-label={t("client.voice")}>
  <div class="where">
    <div class="state" class:elsewhere={onJoinHere}>
      {#if onJoinHere}
        {t("client.voiceElsewhere")}
      {:else}
        <ConnectionInfo {info} />
        {t("client.voiceConnected")}
        {#if music}<span class="note" title={t("client.musicModeHint")}>{t("client.musicMode")}</span>{/if}
        {#if recording}<span class="note rec" title={t("client.recordingHint")}>● {t("client.recording")}</span>{/if}
      {/if}
    </div>
    {#if onShow}
      <button class="channel ellipsis link" title={t("client.showChannel", { channel })} onclick={onShow}>{channel}</button>
    {:else}
      <div class="channel ellipsis" title={channel}>{channel}</div>
    {/if}
  </div>
  {#if onJoinHere}
    <button class="here" onclick={onJoinHere}>{t("client.joinVoiceHere")}</button>
  {:else if entries.length && onGoLive}
    <button
      class="live"
      class:on={live}
      title={t("client.goLive")}
      aria-label={t("client.goLive")}
      aria-pressed={live}
      aria-haspopup="menu"
      aria-expanded={!!menu}
      bind:this={liveButton}
      onclick={(e) => {
        const r = e.currentTarget.getBoundingClientRect();
        menu = menu ? null : { x: r.left, y: r.top - 4 };
      }}
    >
      <Icon name="screen" size={20} />
    </button>
  {/if}
  {#if !onJoinHere}
    <button title={t("client.leaveVoice")} aria-label={t("client.leaveVoice")} onclick={onLeave}>
      <Icon name="hangup" size={20} />
    </button>
  {/if}
</section>

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} up label={t("client.goLive")} anchor={liveButton} onClose={() => (menu = null)}>
    {#each entries as e (e.action)}
      <button class="item" disabled={!!e.off} onclick={() => choose(e.action)}>
        {e.label}
        {#if e.off}<span class="why">{e.off}</span>{/if}
      </button>
    {/each}
  </ContextMenu>
{/if}

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 8px 8px 0;
    padding: 8px 8px 8px 12px;
    border-radius: 8px;
    background: var(--bg-input);
  }
  .where {
    flex: 1;
    min-width: 0;
  }
  .state {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px;
    color: var(--accent);
    font-size: 13px;
    font-weight: 600;
  }
  .note {
    margin-left: 4px;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--bg-hover);
    color: var(--text-muted);
    font-size: 11px;
    font-weight: 500;
  }
  .rec {
    background: var(--danger);
    color: white;
  }
  .channel {
    color: var(--text-muted);
    font-size: 12px;
  }
  .bar .link {
    display: block;
    max-width: 100%;
    padding: 0;
    text-align: left;
  }
  .bar .link:hover {
    color: var(--text);
    text-decoration: underline;
  }
  .bar button {
    display: inline-flex;
    padding: 6px;
    border-radius: 6px;
    color: var(--text-muted);
  }
  .bar button:hover {
    background: var(--bg-hover);
    color: var(--danger);
  }
  .bar .live:hover {
    color: var(--text);
  }
  .bar .live.on {
    color: var(--accent);
  }
  .state.elsewhere {
    color: var(--text-muted);
  }
  .bar .here {
    padding: 6px 10px;
    background: var(--accent-strong);
    color: white;
    font-size: 13px;
    font-weight: 600;
    white-space: nowrap;
  }
  .bar .here:hover {
    background: var(--accent);
    color: white;
  }
  .item {
    display: block;
    width: 100%;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    text-align: left;
    color: var(--text-muted);
  }
  .item:not(:disabled):hover {
    background: var(--accent-strong);
    color: white;
  }
  .why {
    display: block;
    font-size: 12px;
    color: var(--text-faint);
  }
</style>
