<script lang="ts">
  import { downloadUrl, formatDuration, mediaRatio, onlyOne, releaseOnDestroy, shownLength, videoPreload } from "../lib/media";
  import { formatSize } from "../lib/textChannels";
  import { t } from "../lib/i18n.svelte";

  // A video attachment: a play button and the length until it starts, then
  // the browser's controls; the file below.
  // onError: it cannot play (the caller shows the file instead).
  // width, height, length (ms): from the server if it knows them, so the
  // box and the length are right before the metadata loads. poster: the
  // server's thumbnail; with it the video loads only when played.
  let {
    url,
    poster,
    name,
    size,
    width,
    height,
    length,
    onLoad,
    onError,
  }: {
    url: string;
    poster?: string;
    name: string;
    size: number;
    width?: number;
    height?: number;
    length?: number;
    onLoad?: () => void;
    onError?: () => void;
  } = $props();
  let video: HTMLVideoElement | undefined = $state();
  let started = $state(false);
  let duration = $state(0);
  const start = () => {
    started = true;
    video?.play().catch(() => {});
  };
</script>

<div class="video">
  <!-- #t=0.1: Safari shows the first frame, not black (the same src with
       a poster: one that comes later does not reload the video). Uploads
       come without captions. -->
  <!-- svelte-ignore a11y_media_has_caption -->
  <video
    bind:this={video}
    src="{url}#t=0.1"
    {poster}
    preload={videoPreload(poster)}
    playsinline
    style:aspect-ratio={mediaRatio({ width, height })}
    controls={started}
    bind:duration
    onloadedmetadata={onLoad}
    onerror={onError}
    {@attach releaseOnDestroy}
    onplay={(e) => {
      started = true;
      onlyOne(e.currentTarget);
    }}
  ></video>
  {#if !started}
    <button class="play" aria-label={t("media.playFile", { name })} onclick={start}>
      <svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M7 4v16l13-8z" /></svg>
    </button>
    {#if shownLength(duration, length)}<span class="length">{formatDuration(shownLength(duration, length))}</span>{/if}
  {/if}
  <div class="file">
    <a class="name ellipsis" href={url} target="_blank" rel="noopener noreferrer">{name}</a>
    <span class="size">{formatSize(size)}</span>
    <a class="download" href={downloadUrl(url)} download={name} aria-label={t("media.downloadFile", { name })} title={t("media.download")}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        ><path d="M12 4v11M7 10l5 5 5-5M5 20h14" /></svg
      >
    </a>
  </div>
</div>

<style>
  .video {
    position: relative;
    width: min(420px, 100%);
    margin-top: 4px;
    overflow: hidden;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-sidebar);
  }
  video {
    display: block;
    width: 100%;
    max-height: 320px;
    background: #000;
  }
  .play {
    position: absolute;
    top: calc(50% - 18px); /* the video's middle (the file bar is 36px) */
    left: 50%;
    display: grid;
    place-items: center;
    width: 60px;
    height: 60px;
    margin: -30px;
    border-radius: 50%;
    background: rgb(0 0 0 / 0.6);
    color: #fff;
  }
  .play:hover {
    background: rgb(0 0 0 / 0.8);
  }
  .length {
    position: absolute;
    right: 10px;
    bottom: 46px;
    padding: 1px 6px;
    border-radius: 4px;
    background: rgb(0 0 0 / 0.6);
    font-size: 12px;
    color: #fff;
  }
  .file {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 36px;
    padding: 0 8px 0 12px;
  }
  .name {
    flex: 1;
    min-width: 0;
    font-size: 14px;
    color: var(--link);
    text-decoration: none;
  }
  .name:hover {
    text-decoration: underline;
  }
  .size {
    font-size: 12px;
    color: var(--text-muted);
  }
  .download {
    display: grid;
    place-items: center;
    color: var(--text-muted);
  }
  .download:hover {
    color: var(--text);
  }
</style>
