<script lang="ts">
  import { qualityLabel, type Kind, type Quality } from "../lib/stream/quality";
  import { t } from "../lib/i18n.svelte";

  // The streamer's own screen share or camera, small, with its quality
  // (changed while live), a camera switch (onSwitch: more than one camera)
  // and Stop. viewers: the names of those watching.
  let {
    stream,
    kind = "screen",
    viewers = [],
    quality,
    options,
    onQuality,
    onSwitch,
    onStop,
  }: {
    stream: MediaStream;
    kind?: Kind;
    viewers?: string[];
    quality: Quality;
    options: { heights: number[]; fps: number[] };
    onQuality: (q: Quality) => void;
    onSwitch?: () => void;
    onStop: () => void;
  } = $props();
  const show = (video: HTMLVideoElement, s: MediaStream) => {
    video.srcObject = s;
    return { update: (next: MediaStream) => (video.srcObject = next) };
  };
  const camera = $derived(kind === "camera");
  // Your own camera as in a mirror (not a phone's back camera).
  const mirror = $derived(kind === "camera" && stream.getVideoTracks()[0]?.getSettings().facingMode !== "environment");
</script>

<section class="preview" aria-label={camera ? t("media.yourCamera") : t("media.yourScreenShare")}>
  <video use:show={stream} class:mirror autoplay muted playsinline></video>
  <div class="bar">
    <span class="live">{camera ? t("media.cameraBadge") : t("media.liveBadge")}</span>
    <span class="viewers" title={viewers.length ? viewers.join(", ") : t("media.nobodyWatching")}>
      {t("media.watchingCount", { count: viewers.length })}<span class="sr-only">: {viewers.join(", ")}</span>
    </span>
    <button onclick={onStop} aria-label={camera ? t("media.stopYourCamera") : t("media.stopYourScreenShare")}>{t("media.stop")}</button>
  </div>
  <div class="bar quality" title={t("media.qualityTitle", { quality: qualityLabel(quality) })}>
    <select aria-label={camera ? t("media.cameraResolution") : t("media.screenResolution")} value={quality.height} onchange={(e) => onQuality({ ...quality, height: Number(e.currentTarget.value) })}>
      {#each options.heights as h (h)}
        <option value={h}>{h}p</option>
      {/each}
    </select>
    <select aria-label={camera ? t("media.cameraFrameRate") : t("media.screenFrameRate")} value={quality.fps} onchange={(e) => onQuality({ ...quality, fps: Number(e.currentTarget.value) })}>
      {#each options.fps as f (f)}
        <option value={f}>{t("media.framesPerSecond", { fps: f })}</option>
      {/each}
    </select>
    {#if onSwitch}
      <button onclick={onSwitch} title={t("media.switchCameraHint")}>{t("media.switchCamera")}</button>
    {/if}
  </div>
</section>

<style>
  .preview {
    flex: none;
    margin: 8px 8px 0;
    overflow: hidden;
    border-radius: 8px;
    background: #000;
  }
  video {
    display: block;
    width: 100%;
    max-height: 140px;
    object-fit: contain;
  }
  .mirror {
    transform: scaleX(-1);
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 8px;
    background: var(--bg-input);
  }
  .quality {
    gap: 6px;
    padding-top: 0;
  }
  select {
    flex: 1;
    min-width: 0;
    padding: 2px 4px;
    border-radius: 4px;
    background: var(--bg-hover);
    color: var(--text);
    font-size: 12px;
  }
  .live {
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--danger);
    color: #fff;
    font-size: 11px;
    font-weight: 700;
  }
  .viewers {
    flex: 1;
    margin-left: 8px;
    color: var(--text-muted);
    font-size: 12px;
  }
  button {
    padding: 4px 10px;
    border-radius: 4px;
    background: var(--bg-hover);
    font-size: 13px;
    font-weight: 600;
  }
  button:hover {
    color: var(--danger);
  }
  .quality button:hover {
    color: var(--text);
  }
</style>
