<script lang="ts">
  import { untrack } from "svelte";
  import { swipeStep } from "../lib/fileGrid";
  import { jumpStep } from "../lib/jump";
  import { viewerStep } from "../lib/lightbox";
  import { t } from "../lib/i18n.svelte";
  import { generatedImageName } from "../lib/media";

  // Pictures and videos large over the app, one at a time from `start`:
  // the arrow keys, the buttons or a swipe go to the next or the one
  // before. Closes with Escape, the button, or a click outside the image;
  // the browser puts focus back. download: the original of a picture shown
  // by its preview (see lib/media lightboxOf); description: its alt text;
  // video: a player with poster.
  type Item = { url: string; name: string; download?: string; description?: string; video?: boolean; poster?: string };
  // more: a chat's older messages can load (onOlder; loading: meanwhile).
  let {
    items,
    start = 0,
    more = false,
    loading = false,
    onOlder,
    onClose,
  }: { items: readonly Item[]; start?: number; more?: boolean; loading?: boolean; onOlder?: () => void; onClose: () => void } = $props();
  // By URL: the list grows while it is open (older messages above, new
  // ones below), and the item shown stays.
  // svelte-ignore state_referenced_locally
  let at = $state(items[start]?.url); // each opening is a new Lightbox
  const index = $derived(Math.max(0, items.findIndex((i) => i.url === at)));
  const item = $derived(items[index] ?? items[0]!);
  // A step back from the first item loads older messages, page by page
  // until one has media (a few pages at most, see jumpStep), then goes on.
  const hasOlder = $derived(more && !!onOlder);
  let seeking = $state(false);
  let pages = 0;
  const go = (by: number) => {
    const next = viewerStep(index, by, items.length, hasOlder);
    seeking = next === "older";
    pages = 0;
    if (next !== "older") at = items[next]?.url;
  };
  $effect(() => {
    if (!seeking) return;
    const next = jumpStep({ loaded: index > 0, more, loading, pages });
    untrack(() => {
      if (next === "load") {
        pages++;
        onOlder?.();
      } else if (next !== "wait") {
        seeking = false;
        if (next === "jump") at = items[index - 1]?.url;
      }
    });
  });
  let swipeFrom: { x: number; y: number } | null = null;

  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => dialog?.showModal());
  // Not only through the close event: browsers send it late in hidden tabs.
  const close = () => {
    dialog?.close();
    onClose();
  };
</script>

<!-- On the window: a step button that ends disabled takes the focus away. -->
<svelte:window
  onkeydown={(e) => {
    // Not from a video: there they seek.
    if (!(e.target instanceof HTMLVideoElement) && (e.key === "ArrowLeft" || e.key === "ArrowRight") && !e.altKey && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      go(e.key === "ArrowRight" ? 1 : -1);
    }
  }}
/>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
  bind:this={dialog}
  aria-label={item.description || (!item.video && generatedImageName(item.name) ? t("media.picture") : item.name)}
  onclose={onClose}
  onclick={(e) => {
    if (e.target === e.currentTarget || (e.target as Element).classList.contains("stage")) close();
  }}
  onpointerdown={(e) => (swipeFrom = e.pointerType !== "mouse" && e.isPrimary ? { x: e.clientX, y: e.clientY } : null)}
  onpointerup={(e) => {
    if (swipeFrom && e.isPrimary) go(swipeStep(e.clientX - swipeFrom.x, e.clientY - swipeFrom.y));
    swipeFrom = null;
  }}
  onpointercancel={() => (swipeFrom = null)}
>
  <div class="stage">
    {#if items.length > 1 || hasOlder}
      <button type="button" class="step before" disabled={index === 0 && !hasOlder} onclick={() => go(-1)} aria-label={t("media.viewerBefore")}>‹</button>
    {/if}
    {#if item.video}
      <!-- Keyed: the next video starts at its beginning. -->
      {#key item.url}
        <!-- svelte-ignore a11y_media_has_caption (uploads come without captions) -->
        <video src={item.url} poster={item.poster} controls playsinline aria-label={item.description || item.name}></video>
      {/key}
    {:else}
      <img src={item.url} alt={item.description || (generatedImageName(item.name) ? t("media.picture") : item.name)} />
    {/if}
    {#if items.length > 1}
      <button type="button" class="step next" disabled={index === items.length - 1} onclick={() => go(1)} aria-label={t("media.viewerNext")}
        >›</button
      >
    {/if}
  </div>
  {#if item.description}
    <p class="description">{item.description}</p>
  {/if}
  <footer>
    <span class="name ellipsis">{item.name}</span>
    {#if items.length > 1}
      <span class="count" aria-live="polite">{t("media.viewerCount", { index: index + 1, count: items.length })}</span>
    {/if}
    {#if item.download}
      <a href={item.download}>{t("media.downloadOriginal")}</a>
    {:else}
      <a href={item.url} target="_blank" rel="noopener noreferrer">{t("media.openOriginal")}</a>
    {/if}
    <button type="button" class="close" onclick={close} aria-label={t("common.close")}>×</button>
  </footer>
</dialog>

<style>
  dialog {
    width: 100vw;
    max-width: 100vw;
    height: 100vh;
    max-height: 100vh;
    inset: 0;
    margin: 0;
    padding: var(--safe-top) var(--safe-right) var(--safe-bottom) var(--safe-left);
    border: none;
    background: none;
    display: flex;
    flex-direction: column;
  }
  dialog:not([open]) {
    display: none;
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.85);
  }
  .stage {
    position: relative;
    touch-action: pan-y pinch-zoom; /* sideways swipes go to the pictures */
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
  }
  img,
  video {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    border-radius: 4px;
  }
  .step {
    position: absolute;
    top: 50%;
    z-index: 1;
    width: 44px;
    height: 44px;
    margin-top: -22px;
    border-radius: 50%;
    background: rgb(0 0 0 / 0.5);
    color: #ddd;
    font-size: 28px;
    line-height: 1;
  }
  .step:hover:not(:disabled) {
    color: white;
  }
  .step:disabled {
    visibility: hidden;
  }
  .before {
    left: 16px;
  }
  .next {
    right: 16px;
  }
  .description {
    max-height: 20vh;
    margin: 0 16px;
    overflow-y: auto;
    color: #ddd;
    font-size: 14px;
    white-space: pre-wrap;
  }
  .count {
    color: #aaa;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 8px 16px 16px;
    color: #ddd;
    font-size: 14px;
  }
  .name {
    flex: 1;
    min-width: 0;
  }
  a {
    color: #ddd;
  }
  a:hover {
    color: white;
  }
  .close {
    font-size: 28px;
    line-height: 1;
    color: #ddd;
  }
  .close:hover {
    color: white;
  }
</style>
