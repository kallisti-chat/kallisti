<script lang="ts">
  import { untrack } from "svelte";
  import Icon from "./Icon.svelte";
  import StreamStats from "./StreamStats.svelte";
  import type { StreamTotals } from "../lib/stream/stats";
  import { fullScreenStep, gridColumns, nextFocus, stripOf, tapAction, type StageView } from "../lib/stream/layout";
  import { createPip } from "../lib/stream/pip";
  import { isCamera } from "../lib/stream/protocol";
  import { streamLabel } from "../lib/stream/quality";
  import { dragShare, keyShare, shareOf, STAGE_MAX, STAGE_MIN } from "../lib/stream/stageSize";
  import { streamStage } from "../lib/settings.svelte";
  import { t } from "../lib/i18n.svelte";

  // The screen shares and cameras being watched (by stream id), above the
  // chat (as Discord's call area): a grid of all, or one in focus over a
  // strip of the others (the strip can be hidden; its streams still
  // decode). Each with its name, picture-in-picture, full screen and close.
  let {
    sessions,
    nameOf,
    attach,
    onClose,
    volumeOf,
    onVolume,
    statsOf,
    phone = false,
  }: {
    sessions: number[];
    nameOf: (session: number) => string;
    attach: (session: number, canvas: HTMLCanvasElement | null) => void;
    onClose: (session: number) => void;
    // A stream's sound volume, 0 to 1 (undefined: no sound).
    volumeOf: (session: number) => number | undefined;
    onVolume: (session: number, volume: number) => void;
    // A watched stream's numbers (see StreamStats; missing: no "Stats").
    statsOf?: (session: number) => { totals: StreamTotals; delay: number } | null;
    phone?: boolean;
  } = $props();

  // Phones start in focus view (a grid of small tiles helps nobody there).
  // The streams that show their numbers (see StreamStats).
  let withStats = $state<ReadonlySet<number>>(new Set());
  const toggleStats = (session: number) =>
    (withStats = withStats.has(session) ? new Set([...withStats].filter((s) => s !== session)) : new Set([...withStats, session]));
  let view = $state<StageView>(untrack(() => (phone ? "focus" : "grid")));
  let stripHidden = $state(false);
  let focused = $state<number | null>(null);
  let before: number[] = [];
  $effect.pre(() => {
    const now = sessions;
    untrack(() => (focused = nextFocus(focused, before, now)));
    before = now;
  });
  const focusing = $derived(view === "focus" && sessions.length > 1);
  const strip = $derived(focusing ? stripOf(sessions, focused) : []);
  let width = $state(0);
  let height = $state(0);
  const cols = $derived(gridColumns(sessions.length, width, height));
  let stage = $state<HTMLElement>();

  // Its height: the lower edge is dragged (see stream/stageSize), saved
  // when let go. While dragging only here.
  let dragging = $state<number | null>(null);
  const share = $derived(dragging ?? shareOf(streamStage.value, phone));
  let dragFrom: { y: number; share: number } | null = null;
  const startDrag = (e: PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    dragFrom = { y: e.clientY, share };
    dragging = share;
  };
  const drag = (e: PointerEvent) => {
    if (dragFrom) dragging = dragShare(dragFrom.share, dragFrom.y, e.clientY, innerHeight);
  };
  const endDrag = () => {
    if (dragging !== null) streamStage.set(dragging);
    dragFrom = dragging = null;
  };
  const resizeKey = (e: KeyboardEvent) => {
    const next = keyShare(share, e.key);
    if (next === null) return;
    e.preventDefault();
    streamStage.set(next);
  };

  // A click on a stream focuses it; on the focused one: back to the grid.
  const pick = (session: number) => {
    if (sessions.length < 2) return;
    view = focusing && session === focused ? "grid" : "focus";
    focused = session;
  };

  // Touch screens: no hover, so a tap shows a stream's controls for a few
  // seconds (see tapAction); using them keeps them.
  const touch = matchMedia("(hover: none)").matches;
  let shown = $state<number | null>(null);
  let hideTimer: ReturnType<typeof setTimeout> | undefined;
  const reveal = (session: number) => {
    shown = session;
    clearTimeout(hideTimer);
    hideTimer = setTimeout(() => (shown = null), 3000);
  };
  $effect(() => () => clearTimeout(hideTimer));
  const tap = (session: number) => {
    if (tapAction({ touch, shown: shown === session }) === "show") return reveal(session);
    pick(session);
    if (touch) shown = null;
  };
  // iPhones have no full screen for page elements (only for videos).
  const fullScreenSupported = document.fullscreenEnabled === true;

  const fullScreen = (el: Element | null | undefined) => {
    if (!el) return;
    if (fullScreenStep(document.fullscreenElement, el) === "exit") void document.exitFullscreen();
    else void el.requestFullscreen();
  };

  // Picture-in-picture: the canvas's picture in a video element of its own
  // (only videos float). Chromium and Safari; Firefox has no such API, so
  // no button there.
  const pipSupported = document.pictureInPictureEnabled === true && "captureStream" in HTMLCanvasElement.prototype;
  const canvases = new Map<number, HTMLCanvasElement>();
  let pip = $state<number | null>(null);
  const pips = createPip(
    document,
    async (session) => {
      const canvas = canvases.get(session);
      if (!canvas) return null;
      const capture = canvas.captureStream();
      // A still screen draws seldom: a first frame now, so play() does not
      // wait (and the click's permission does not run out).
      (capture.getVideoTracks()[0] as CanvasCaptureMediaStreamTrack | undefined)?.requestFrame();
      const video = document.createElement("video");
      video.muted = true;
      video.srcObject = capture;
      const done = () => {
        capture.getTracks().forEach((t) => t.stop());
        video.srcObject = null;
      };
      video.onleavepictureinpicture = () => {
        done();
        pips.left(video);
      };
      try {
        await video.play();
        await video.requestPictureInPicture();
        return video;
      } catch {
        done();
        return null;
      }
    },
    (session) => (pip = session),
  );

  const canvasOf = (canvas: HTMLCanvasElement, session: number) => {
    canvases.set(session, canvas);
    attach(session, canvas);
    return {
      destroy: () => {
        canvases.delete(session);
        attach(session, null);
        if (pips.current() === session) void document.exitPictureInPicture().catch(() => {});
      },
    };
  };
</script>

<section class="stage" class:focusing class:bare={focusing && stripHidden} bind:this={stage} aria-label={t("media.streams")}>
  {#if sessions.length > 1}
    <div class="bar">
      <button
        onclick={() => (view = view === "grid" ? "focus" : "grid")}
        title={view === "grid" ? t("media.focusView") : t("media.gridView")}
        aria-label={view === "grid" ? t("media.focusView") : t("media.gridView")}
      >
        <Icon name={view === "grid" ? "focus" : "grid"} />
      </button>
      {#if focusing}
        <button onclick={() => (stripHidden = !stripHidden)} aria-pressed={stripHidden} title={stripHidden ? t("media.showOthersHint") : t("media.hideOthersHint")}>
          {stripHidden ? t("media.showOthers") : t("media.hideOthers")}
        </button>
      {/if}
      {#if fullScreenSupported}
        <button onclick={() => fullScreen(stage)} title={t("media.fullScreen")} aria-label={t("media.fullScreenAll")}>
          <Icon name="fullscreen" />
        </button>
      {/if}
    </div>
  {/if}
  <div class="tiles" class:many={sessions.length > 1} bind:clientWidth={width} bind:clientHeight={height} style:--cols={cols} style:--strip={strip.length} style:--share={share}>
    {#each sessions as session (session)}
      {@const at = strip.indexOf(session)}
      {@const label = streamLabel(nameOf(session), isCamera(session))}
      <div class="tile" class:focused={focusing && session === focused} style:grid-column={at >= 0 ? at + 2 : null}>
        <button class="screen" onclick={() => tap(session)} tabindex={sessions.length > 1 ? 0 : -1} aria-label={focusing && session === focused ? t("media.gridView") : t("media.focusStream", { stream: label })}>
          <canvas use:canvasOf={session} aria-label={label}></canvas>
        </button>
        {#if statsOf && withStats.has(session)}
          <StreamStats read={() => statsOf(session)} />
        {/if}
        {#if pip === session}
          <!-- The canvas draws on below: the floating window shows it. -->
          <div class="pip-note">
            <Icon name="pip" size={28} />
            <span>{t("media.playingInPip")}</span>
            <button onclick={() => pips.toggle(session)}>{t("media.bringBackFromPip")}</button>
          </div>
        {/if}
        <!-- svelte-ignore a11y_no_static_element_interactions (only keeps the controls shown) -->
        <div class="overlay" class:shown={shown === session} onpointerdown={() => touch && reveal(session)}>
          <span class="who"><span class="live">{#if isCamera(session)}<Icon name="camera" size={12} /><span class="sr-only">{t("media.cameraBadgeHidden")}</span>{:else}{t("media.liveBadge")}{/if}</span> {nameOf(session)}</span>
          <span class="tools">
            {#if volumeOf(session) !== undefined}
              <input
                type="range"
                min="0"
                max="1"
                step="0.05"
                value={volumeOf(session)}
                aria-label={t("media.streamVolume", { name: nameOf(session) })}
                oninput={(e) => onVolume(session, Number(e.currentTarget.value))}
              />
            {/if}
            {#if statsOf}
              <button onclick={() => toggleStats(session)} aria-pressed={withStats.has(session)} title={t("media.stats")} aria-label={t("media.statsOf", { stream: label })}>
                <Icon name="info" size={16} />
              </button>
            {/if}
            {#if pipSupported}
              <button onclick={() => pips.toggle(session)} aria-pressed={pip === session} title={t("media.pictureInPicture")} aria-label={t("media.pictureInPicture")}>
                <Icon name="pip" size={16} />
              </button>
            {/if}
            {#if fullScreenSupported}
              <button onclick={(e) => fullScreen(e.currentTarget.closest(".tile"))} title={t("media.fullScreen")} aria-label={t("media.fullScreen")}>⛶</button>
            {/if}
            <button onclick={() => onClose(session)} title={t("media.stopWatching")} aria-label={t("media.stopWatchingStream", { stream: label })}>×</button>
          </span>
        </div>
      </div>
    {/each}
  </div>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (a focusable separator is a resize handle) -->
  <div
    class="resize"
    role="separator"
    aria-orientation="horizontal"
    aria-label={t("media.resizeStage")}
    aria-valuemin={STAGE_MIN * 100}
    aria-valuemax={STAGE_MAX * 100}
    aria-valuenow={Math.round(share * 100)}
    tabindex="0"
    class:dragging={dragging !== null}
    onpointerdown={startDrag}
    onpointermove={drag}
    onpointerup={endDrag}
    onpointercancel={endDrag}
    onkeydown={resizeKey}
  ></div>
</section>

<style>
  .stage {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 16px;
    background: #000;
  }
  .bar {
    display: flex;
    justify-content: flex-end;
    gap: 4px;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    grid-auto-rows: minmax(0, 1fr);
    gap: 8px;
    height: calc(var(--share) * 100vh);
  }
  /* Focus: the focused stream over a centred strip of the others (their
     columns are set inline, between the two flexible ones). */
  .focusing .tiles {
    grid-template-columns: 1fr repeat(var(--strip), minmax(0, 160px)) 1fr;
    grid-template-rows: minmax(0, 1fr) 90px;
  }
  .focusing .tile.focused {
    grid-row: 1;
    grid-column: 1 / -1;
  }
  .focusing .tile:not(.focused) {
    grid-row: 2;
  }
  .bare .tiles {
    grid-template-rows: minmax(0, 1fr);
  }
  /* A hidden tile in full screen stays there. */
  .bare .tile:not(.focused):not(:fullscreen),
  .focusing .tile:not(.focused) .tools input {
    display: none;
  }
  .stage:fullscreen .tiles {
    flex: 1;
    height: auto;
  }
  /* The lower edge: drag it, or the arrow keys. */
  .resize {
    height: 8px;
    margin: 0 -16px -8px;
    cursor: ns-resize;
    touch-action: none;
  }
  .resize::after {
    content: "";
    display: block;
    width: 48px;
    height: 4px;
    margin: 2px auto 0;
    border-radius: 2px;
    background: rgb(255 255 255 / 0.25);
  }
  .resize:hover::after,
  .resize:focus-visible::after,
  .resize.dragging::after {
    background: rgb(255 255 255 / 0.6);
  }
  .stage:fullscreen .resize {
    display: none;
  }
  .tile {
    position: relative;
    min-width: 0;
    min-height: 0;
    background: #000;
  }
  .screen {
    display: block;
    width: 100%;
    height: 100%;
    padding: 0;
    cursor: default;
  }
  .many .screen {
    cursor: pointer;
  }
  canvas {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .pip-note {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    background: #111;
    color: rgb(255 255 255 / 0.8);
    font-size: 14px;
    text-align: center;
  }
  .pip-note button {
    padding: 4px 12px;
    border-radius: 4px;
    background: rgb(255 255 255 / 0.15);
    color: #fff;
  }
  .pip-note button:hover {
    background: rgb(255 255 255 / 0.25);
  }
  .overlay {
    position: absolute;
    inset: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 4px;
    padding: 6px 8px;
    background: linear-gradient(rgb(0 0 0 / 0.6), transparent);
    color: #fff;
    font-size: 13px;
    opacity: 0;
    transition: opacity 0.15s;
    pointer-events: none; /* clicks go to the screen below */
  }
  .who {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .tile:hover .overlay,
  .tile:focus-within .overlay,
  .overlay.shown {
    opacity: 1;
  }
  .live {
    display: inline-flex;
    vertical-align: middle;
    padding: 0 5px;
    border-radius: 3px;
    background: var(--danger);
    font-size: 10px;
    font-weight: 700;
  }
  .tools {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .tools input {
    width: 90px;
    accent-color: var(--accent);
  }
  .tools button,
  .bar button {
    display: flex;
    align-items: center;
    padding: 2px 8px;
    border-radius: 4px;
    color: #fff;
    font-size: 16px;
  }
  .bar button {
    font-size: 13px;
  }
  .tools button:hover,
  .bar button:hover {
    background: rgb(255 255 255 / 0.15);
  }
  /* Full screen covers the system bars (phones): keep clear of them. */
  .tile:fullscreen .overlay,
  .stage:fullscreen .bar {
    padding-top: max(6px, var(--safe-top));
    padding-right: max(8px, var(--safe-right));
    padding-left: max(8px, var(--safe-left));
  }
  .stage:fullscreen {
    padding-bottom: max(8px, var(--safe-bottom));
  }
  /* Touch screens: controls a finger can hit, and none while hidden. */
  @media (hover: none) {
    .overlay:not(.shown) .tools {
      pointer-events: none;
    }
    .tools button,
    .bar button {
      justify-content: center;
      min-width: 44px;
      min-height: 44px;
    }
    .tools input {
      width: 120px;
      height: 32px;
    }
  }
  @media (max-width: 700px) {
    .stage {
      padding: 8px;
    }
    .focusing .tiles {
      grid-template-rows: minmax(0, 1fr) 64px;
    }
  }
</style>
