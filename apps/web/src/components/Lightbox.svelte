<script lang="ts">
  import { step, swipeStep } from "../lib/fileGrid";
  import { t } from "../lib/i18n.svelte";

  // Images large over the app, one at a time from `start`: the arrow keys,
  // the buttons or a swipe go to the next or the one before. Closes with
  // Escape, the button, or a click outside the image; the browser puts
  // focus back. download: the original of a picture shown by its preview
  // (see lib/media lightboxOf); description: its alt text.
  type Item = { url: string; name: string; download?: string; description?: string };
  let { items, start = 0, onClose }: { items: readonly Item[]; start?: number; onClose: () => void } = $props();
  // svelte-ignore state_referenced_locally
  let index = $state(start); // each opening is a new Lightbox
  const item = $derived(items[index] ?? items[0]!);
  const go = (by: number) => (index = step(index, by, items.length));
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
    if ((e.key === "ArrowLeft" || e.key === "ArrowRight") && !e.altKey && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      go(e.key === "ArrowRight" ? 1 : -1);
    }
  }}
/>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
  bind:this={dialog}
  aria-label={item.name}
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
    {#if items.length > 1}
      <button type="button" class="step before" disabled={index === 0} onclick={() => go(-1)} aria-label={t("media.previousPicture")}>‹</button>
    {/if}
    <img src={item.url} alt={item.description || item.name} />
    {#if items.length > 1}
      <button type="button" class="step next" disabled={index === items.length - 1} onclick={() => go(1)} aria-label={t("media.nextPicture")}
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
      <span class="count" aria-live="polite">{t("media.pictureCount", { index: index + 1, count: items.length })}</span>
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
  img {
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
