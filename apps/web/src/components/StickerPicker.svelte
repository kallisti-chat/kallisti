<script lang="ts">
  import { onMount } from "svelte";
  import { fitMenu } from "../lib/menu";
  import { gridMove } from "../lib/emojiPicker";
  import { stickerGroups, stickerSearch, type Sticker } from "../lib/stickers";
  import { t } from "../lib/i18n.svelte";

  // The server's stickers to send: search (name, description, pack,
  // emoji), grouped by pack. A popup at `anchor` (the button that opened
  // it); on phones a sheet at the bottom, as the emoji picker. url:
  // absolute.
  let { anchor, stickers, onPick, onClose }: { anchor: DOMRect; stickers: readonly Sticker[]; onPick: (s: Sticker) => void; onClose: () => void } =
    $props();

  // As in the CSS.
  const WIDTH = 352;
  const HEIGHT = 420;

  let query = $state("");
  let search: HTMLInputElement | undefined = $state();
  let grid: HTMLElement | undefined = $state();
  let dialog: HTMLElement | undefined = $state();

  const found = $derived(stickerSearch(stickers, query));
  const groups = $derived(stickerGroups(found));
  const placement = $derived.by(() => {
    const view = { width: innerWidth, height: innerHeight };
    const below = fitMenu({ x: anchor.right - WIDTH, y: anchor.bottom + 4 }, { width: WIDTH, height: HEIGHT }, view);
    return anchor.bottom + 4 + HEIGHT > view.height ? { ...below, y: Math.max(8, anchor.top - HEIGHT - 4) } : below;
  });

  onMount(() => {
    // Touch screens: not the search field, its keyboard would cover the sheet.
    if (matchMedia("(hover: hover)").matches) search?.focus();
    else dialog?.focus();
  });

  const trapTab = (e: KeyboardEvent) => {
    const stops = [...(dialog?.querySelectorAll<HTMLElement>('input, button:not([tabindex="-1"])') ?? [])];
    const at = stops.indexOf(document.activeElement as HTMLElement);
    const next = at < 0 ? 0 : (at + (e.shiftKey ? -1 : 1) + stops.length) % stops.length;
    e.preventDefault();
    stops[next]?.focus();
  };
  const buttons = () => [...(grid?.querySelectorAll<HTMLButtonElement>("button.cell") ?? [])];
  const onGridKey = (e: KeyboardEvent) => {
    const all = buttons();
    const from = all.indexOf(e.target as HTMLButtonElement);
    if (from < 0) return;
    const to = gridMove(
      all.map((b) => ({ x: b.offsetLeft, y: b.offsetTop })),
      from,
      e.key,
    );
    if (to === null) return;
    e.preventDefault();
    if (to < 0) search?.focus();
    else all[to]?.focus();
  };
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" data-no-swipe onclick={onClose}></div>
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  class="picker"
  bind:this={dialog}
  role="dialog"
  aria-modal="true"
  tabindex="-1"
  aria-label={t("media.stickers")}
  style:left="{placement.x}px"
  style:top="{placement.y}px"
  onkeydown={(e) => {
    if (e.key === "Tab") trapTab(e);
    if (e.key === "Escape") {
      e.stopPropagation();
      onClose();
    }
  }}
>
  <div class="top">
    <input
      bind:this={search}
      bind:value={query}
      type="search"
      placeholder={t("media.stickerSearchPlaceholder")}
      aria-label={t("media.stickerSearch")}
      onkeydown={(e) => {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          buttons()[0]?.focus();
        } else if (e.key === "Enter" && found[0]) {
          e.preventDefault();
          onPick(found[0]);
        }
      }}
    />
  </div>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="grid" bind:this={grid} onkeydown={onGridKey}>
    {#if stickers.length === 0}
      <p class="note">{t("media.noStickers")}</p>
    {:else if found.length === 0}
      <p class="note">{t("media.noStickerFound")}</p>
    {/if}
    {#each groups as g (g.pack)}
      {#if g.pack || groups.length > 1}<h3>{g.pack || t("media.stickersWithoutPack")}</h3>{/if}
      <div class="cells">
        {#each g.stickers as s (s.id)}
          <button
            type="button"
            class="cell"
            title={s.description ? `${s.name}: ${s.description}` : s.name}
            aria-label={s.name}
            onclick={() => onPick(s)}><img src={s.url} alt="" loading="lazy" /></button
          >
        {/each}
      </div>
    {/each}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
  }
  .picker {
    position: fixed;
    z-index: 41;
    display: flex;
    flex-direction: column;
    width: 352px;
    height: 420px;
    border-radius: 8px;
    background: var(--bg-rail);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.4);
    overflow: hidden;
    outline: none;
  }
  .top {
    display: flex;
    padding: 12px 12px 8px;
  }
  .top input {
    flex: 1;
    min-width: 0;
    padding: 6px 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-input);
    color: inherit;
    font: inherit;
    font-size: 14px;
  }
  .grid {
    flex: 1;
    overflow-y: auto;
    scroll-padding-top: 32px; /* below the sticky heading */
    padding: 0 8px 8px;
  }
  h3 {
    position: sticky;
    top: 0;
    z-index: 1;
    margin: 0;
    padding: 8px 4px 4px;
    background: var(--bg-rail);
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
  }
  .cells {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(80px, 1fr));
    gap: 4px;
  }
  .cell {
    display: grid;
    aspect-ratio: 1;
    padding: 4px;
    place-items: center;
    border: none;
    border-radius: 6px;
    background: none;
    cursor: pointer;
  }
  .cell img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .cell:hover,
  .cell:focus-visible {
    background: var(--bg-active);
  }
  .note {
    padding: 16px 4px;
    color: var(--text-muted);
    text-align: center;
  }
  /* Phones: a sheet at the bottom. */
  @media (max-width: 700px) {
    .backdrop {
      background: rgb(0 0 0 / 0.5);
    }
    .picker {
      top: auto !important;
      bottom: 0;
      left: 0 !important;
      width: 100%;
      height: 70vh;
      padding-bottom: var(--safe-bottom);
      border-radius: 12px 12px 0 0;
    }
  }
</style>
