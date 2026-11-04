<script lang="ts">
  import { downloadUrl, finite, formatDuration, onlyOne, releaseOnDestroy, shownLength } from "../lib/media";
  import { formatSize } from "../lib/textChannels";
  import { t } from "../lib/i18n.svelte";

  // An audio attachment: the file card with a player row.
  // onError: it cannot play (the caller shows the file instead).
  // length: milliseconds, from the server if it knows it.
  let { url, name, size, length, onError }: { url: string; name: string; size: number; length?: number; onError?: () => void } = $props();
  let audio: HTMLAudioElement | undefined = $state();
  let paused = $state(true);
  let time = $state(0);
  let duration = $state(0);
  let muted = $state(false);
  const ext = $derived((name.split(".").pop() ?? "").slice(0, 4).toUpperCase());
</script>

<div class="player">
  <div class="top">
    <span class="kind" aria-hidden="true">{ext}</span>
    <span class="meta">
      <a class="name ellipsis" href={url} target="_blank" rel="noopener noreferrer">{name}</a>
      <span class="size">{formatSize(size)}</span>
    </span>
    <a class="icon" href={downloadUrl(url)} download={name} aria-label={t("media.downloadFile", { name })} title={t("media.download")}>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        ><path d="M12 4v11M7 10l5 5 5-5M5 20h14" /></svg
      >
    </a>
  </div>
  <div class="bar">
    <button class="icon" aria-label={paused ? t("media.playFile", { name }) : t("media.pauseFile", { name })} onclick={() => (paused ? audio?.play().catch(() => {}) : audio?.pause())}>
      <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"
        >{#if paused}<path d="M7 4v16l13-8z" />{:else}<path d="M6 4h4v16H6zM14 4h4v16h-4z" />{/if}</svg
      >
    </button>
    <span class="time">{formatDuration(time)} / {formatDuration(shownLength(duration, length))}</span>
    <input
      type="range"
      min="0"
      max={finite(duration)}
      step="0.1"
      value={time}
      aria-label={t("media.position")}
      oninput={(e) => audio && (audio.currentTime = Number(e.currentTarget.value))}
    />
    <button class="icon" aria-label={muted ? t("media.unmute") : t("media.mute")} aria-pressed={muted} onclick={() => (muted = !muted)}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        ><path d="M4 9h4l5-4v14l-5-4H4z" />{#if muted}<path d="M17 9l4 6M21 9l-4 6" />{:else}<path d="M16 9a4 4 0 0 1 0 6" />{/if}</svg
      >
    </button>
  </div>
  <audio
    bind:this={audio}
    src={url}
    preload="metadata"
    bind:paused
    bind:currentTime={time}
    bind:duration
    bind:muted
    onplay={(e) => onlyOne(e.currentTarget)}
    {@attach releaseOnDestroy}
    onerror={onError}
  ></audio>
</div>

<style>
  .player {
    width: min(400px, 100%);
    margin-top: 4px;
    padding: 10px 14px 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-sidebar);
  }
  .top {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .kind {
    display: grid;
    flex: none;
    place-items: center;
    width: 30px;
    height: 36px;
    border-radius: 4px;
    background: var(--bg-active);
    font-size: 10px;
    color: var(--text-muted);
  }
  .meta {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
    line-height: 1.25;
  }
  .name {
    color: var(--link);
    text-decoration: none;
  }
  .name:hover {
    text-decoration: underline;
  }
  .size,
  .time {
    font-size: 12px;
    color: var(--text-muted);
  }
  .time {
    flex: none;
    font-variant-numeric: tabular-nums;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 10px;
    padding: 4px 8px;
    border-radius: 6px;
    background: var(--bg-rail);
  }
  .icon {
    display: grid;
    flex: none;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    color: var(--text-muted);
  }
  .icon:hover {
    color: var(--text);
  }
  .bar button.icon:first-child {
    color: var(--text);
  }
  input[type="range"] {
    flex: 1;
    min-width: 0;
    accent-color: var(--accent);
  }
</style>
