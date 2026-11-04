<script lang="ts">
  import { encodeEmoji, loadEmojiImage, renderEmoji } from "../lib/emojiCanvas";
  import { EMOJI_SIZE, MAX_ZOOM, cropKey, cropZoom, isAnimated, keepsOriginal, middleCrop, moveCrop, zoomCrop, type Crop, type Size } from "../lib/emojiResize";
  import { around } from "../lib/i18n";
  import { t } from "../lib/i18n.svelte";

  // A new emoji's image: still images are scaled down (optionally cropped
  // to a square first), with a preview; output() gives the file to upload.
  // max: the size to fit (a sticker's is larger); limit: its byte limit,
  // for the note.
  let { file, name, max = EMOJI_SIZE, limit = "256 KB" }: { file: File | null; name: string; max?: number; limit?: string } = $props();

  type Loaded = { url: string } & ({ animated: true } | { animated: false; image: HTMLCanvasElement; original: Size });
  let loaded = $state.raw<Loaded | null>(null);
  let failed = $state(false);
  let cropping = $state(false);
  let crop = $state.raw<Crop>({ x: 0, y: 0, size: 1 });
  let loading: Promise<Loaded | null> = Promise.resolve(null);

  const load = async (f: File, url: string): Promise<Loaded> => {
    if (isAnimated(new Uint8Array(await f.arrayBuffer()))) return { url, animated: true };
    return { url, animated: false, ...(await loadEmojiImage(f, max)) };
  };

  $effect(() => {
    const f = file;
    loaded = null;
    failed = false;
    cropping = false;
    if (!f) return;
    let stale = false;
    const url = URL.createObjectURL(f);
    loading = load(f, url).then(
      (l) => {
        if (stale) return null;
        if (!l.animated) crop = middleCrop(l.image.width, l.image.height);
        return (loaded = l);
      },
      () => {
        if (!stale) failed = true;
        return null;
      },
    );
    return () => {
      stale = true;
      URL.revokeObjectURL(url);
    };
  });

  const still = $derived(loaded && !loaded.animated ? loaded : null);
  const W = $derived(still?.image.width ?? 1);
  const H = $derived(still?.image.height ?? 1);
  const rendered = $derived(still && renderEmoji(still.image, cropping ? crop : null, max));
  const preview = $derived(rendered ? rendered.toDataURL() : loaded?.url);
  const size = $derived({ width: rendered?.width ?? 0, height: rendered?.height ?? 0 });

  // The file to upload: as it came if it is animated, small enough and
  // not cropped, or cannot be read (the server checks it).
  export const output = async (): Promise<File | null> => {
    const l = await loading;
    if (!file || !l || l.animated || !rendered || keepsOriginal({ ...l.original, animated: false, cropping, max })) return file;
    const blob = await encodeEmoji(rendered);
    return new File([blob], `${name || "emoji"}.${blob.type === "image/webp" ? "webp" : "png"}`, { type: blob.type });
  };

  // Dragging the crop: pointer moves in CSS pixels, the crop in image pixels.
  let img: HTMLImageElement | undefined = $state();
  let drag: { x: number; y: number } | null = null;
  const move = (e: PointerEvent) => {
    if (!drag || !img) return;
    const scale = W / img.clientWidth;
    crop = moveCrop(crop, (e.clientX - drag.x) * scale, (e.clientY - drag.y) * scale, W, H);
    drag = { x: e.clientX, y: e.clientY };
  };
</script>

{#if failed}
  <p class="note">{t("media.imageUnreadable")}</p>
{:else if loaded?.animated}
  <p class="note">{t("media.animatedKept", { limit })}</p>
{:else if still}
  <label class="check"><input type="checkbox" bind:checked={cropping} /> {t("media.cropToSquare")}</label>
  {#if cropping}
    <div
      class="crop"
      role="presentation"
      onwheel={(e) => {
        e.preventDefault();
        crop = zoomCrop(crop, e.deltaY < 0 ? 1.1 : 1 / 1.1, W, H);
      }}
    >
      <img bind:this={img} src={still.url} alt="" draggable="false" />
      <button
        type="button"
        class="box"
        aria-label={t("media.cropArea")}
        style:left="{(crop.x / W) * 100}%"
        style:top="{(crop.y / H) * 100}%"
        style:width="{(crop.size / W) * 100}%"
        style:height="{(crop.size / H) * 100}%"
        onpointerdown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          drag = { x: e.clientX, y: e.clientY };
        }}
        onpointermove={move}
        onpointerup={() => (drag = null)}
        onpointercancel={() => (drag = null)}
        onkeydown={(e) => {
          const next = cropKey(crop, e.key, e.shiftKey, W, H);
          if (next) (e.preventDefault(), (crop = next));
        }}
      ></button>
    </div>
    <label class="check">
      {t("media.zoom")}
      <input
        type="range"
        min="1"
        max={MAX_ZOOM}
        step="0.01"
        value={cropZoom(crop, W, H)}
        oninput={(e) => (crop = zoomCrop(crop, +e.currentTarget.value / cropZoom(crop, W, H), W, H))}
      />
    </label>
  {/if}
{/if}
{#if preview}
  <div class="preview" aria-label={t("media.preview")}>
    <p>
      {#if max === EMOJI_SIZE}{@const [before, after] = around(rendered ? t("media.looksLikeInMessageSize", size) : t("media.looksLikeInMessage"), "emoji")}{before}<img
          class="small"
          src={preview}
          alt={`:${name}:`}
        />{after}{:else}{rendered ? t("media.asUploadedSize", size) : t("media.asUploaded")}{/if}
    </p>
    <img class="large" src={preview} alt="" />
  </div>
{/if}

<style>
  .note {
    color: var(--text-muted);
    font-size: 14px;
  }
  .check {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .crop {
    position: relative;
    width: fit-content;
    overflow: hidden;
    line-height: 0;
  }
  .crop img {
    display: block;
    max-width: min(320px, 80vw);
    max-height: 240px;
    user-select: none;
  }
  .box {
    position: absolute;
    padding: 0;
    border: 2px solid #fff;
    background: none;
    box-shadow: 0 0 0 9999px rgb(0 0 0 / 50%);
    cursor: move;
    touch-action: none;
  }
  .box:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -5px;
  }
  .preview {
    display: flex;
    gap: 12px;
    align-items: center;
    padding: 8px;
    border-radius: 4px;
    background: var(--bg-rail);
  }
  .preview p {
    margin: 0;
    font-size: 16px;
  }
  .small,
  .large {
    object-fit: contain;
  }
  .small {
    width: 22px;
    height: 22px;
    vertical-align: -0.3em;
  }
  .large {
    width: 48px;
    height: 48px;
  }
</style>
