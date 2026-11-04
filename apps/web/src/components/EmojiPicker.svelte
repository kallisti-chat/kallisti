<script lang="ts">
  import { onMount, tick } from "svelte";
  import Emoji from "./Emoji.svelte";
  import { fitMenu } from "../lib/menu";
  import { gridMove, loadEmojiData, normalEmoji, searchEmoji, withTone, type EmojiData, type EmojiEntry } from "../lib/emojiPicker";
  import { recentEmoji, skinTone } from "../lib/emojiPrefs.svelte";
  import { customEmojiFor, customEntries } from "../lib/customEmoji";
  import { personalEntries } from "../lib/personalEmoji";
  import { t } from "../lib/i18n.svelte";

  // All emoji: search, recently used, categories, skin tone. A popup at
  // `anchor` (the button that opened it); on phones a sheet at the bottom.
  // onPick gets the emoji in the server's form (normalEmoji); the caller
  // adds it to the recent ones. selected: emoji to mark (the user's
  // reactions). The cells are reached with the arrow keys, Tab stays in
  // the dialog. personal: offer our personal emoji ("<:name:id>"; not
  // for statuses).
  let {
    anchor,
    onPick,
    onClose,
    selected = () => false,
    label = t("media.emoji"),
    personal = true,
  }: {
    anchor: DOMRect;
    onPick: (emoji: string) => void;
    onClose: () => void;
    selected?: (emoji: string) => boolean;
    label?: string;
    personal?: boolean;
  } = $props();

  // As in the CSS.
  const WIDTH = 352;
  const HEIGHT = 420;
  const TONES = ["✋", "✋🏻", "✋🏼", "✋🏽", "✋🏾", "✋🏿"];

  let data = $state<EmojiData | null>(null);
  let failed = $state(false);
  let query = $state("");
  let choosingTone = $state(false);
  let shown = $state<{ emoji: string; name: string } | null>(null);
  let search: HTMLInputElement | undefined = $state();
  let grid: HTMLElement | undefined = $state();

  // The server's custom emoji (":name:") and ours, first in the list and
  // in results.
  const customEmoji = customEmojiFor();
  const custom = $derived(customEntries(customEmoji().list));
  const mine = $derived(personal ? personalEntries(customEmoji().mine) : []);
  // Recent emoji show as they were used (with their skin tone, if any);
  // custom ones only while this server has them, personal ones while ours.
  const byEmoji = $derived(
    new Map([
      ...custom.map((e) => [e.emoji, e] as const),
      ...mine.map((e) => [e.emoji, e] as const),
      ...(data?.emoji ?? []).flatMap((e) =>
        [e.emoji, ...(e.skins ?? [])].map((emoji) => [normalEmoji(emoji), { ...e, emoji, skins: undefined }] as const),
      ),
    ]),
  );
  const results = $derived(
    data && query.trim() ? [...searchEmoji(custom, query), ...searchEmoji(mine, query), ...searchEmoji(data.emoji, query)] : null,
  );
  const sections = $derived.by(() => {
    if (!data) return [];
    const recent = recentEmoji.value.flatMap((e) => byEmoji.get(normalEmoji(e)) ?? []);
    const groups = data.groups.map((g, i) => ({ ...g, emoji: data!.emoji.filter((e) => e.group === i) }));
    return [
      ...(custom.length ? [{ name: t("media.emojiServerSection"), icon: custom[0]!.emoji, emoji: custom }] : []),
      ...(mine.length ? [{ name: t("media.emojiMineSection"), icon: mine[0]!.emoji, emoji: mine }] : []),
      ...(recent.length ? [{ name: t("media.emojiRecentSection"), icon: "🕒", emoji: recent }] : []),
      ...groups,
    ];
  });

  const placement = $derived.by(() => {
    const view = { width: innerWidth, height: innerHeight };
    const below = fitMenu({ x: anchor.right - WIDTH, y: anchor.bottom + 4 }, { width: WIDTH, height: HEIGHT }, view);
    // Above the button when it does not fit below.
    return anchor.bottom + 4 + HEIGHT > view.height ? { ...below, y: Math.max(8, anchor.top - HEIGHT - 4) } : below;
  });

  let dialog: HTMLElement | undefined = $state();
  onMount(() => {
    // Touch screens: not the search field, its keyboard would cover the sheet.
    if (matchMedia("(hover: hover)").matches) search?.focus();
    else dialog?.focus();
    loadEmojiData().then(
      (d) => (data = d),
      () => (failed = true),
    );
  });

  const pick = (e: EmojiEntry) => onPick(normalEmoji(withTone(e, skinTone.value)));
  let toneButton: HTMLButtonElement | undefined = $state();

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

  const jump = async (i: number) => {
    query = "";
    await tick();
    grid?.querySelector(`[data-section="${i}"]`)?.scrollIntoView({ block: "start" });
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
  aria-label={label}
  style:left="{placement.x}px"
  style:top="{placement.y}px"
  onkeydown={(e) => {
    if (e.key === "Tab") trapTab(e);
    if (e.key === "Escape") {
      e.stopPropagation();
      if (choosingTone) {
        choosingTone = false;
        toneButton?.focus();
      } else onClose();
    }
  }}
>
  <div class="top">
    <input
      bind:this={search}
      bind:value={query}
      oninput={() => (shown = null)}
      type="search"
      placeholder={t("media.emojiSearchPlaceholder")}
      aria-label={t("media.emojiSearch")}
      onkeydown={(e) => {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          buttons()[0]?.focus();
        } else if (e.key === "Enter" && results?.[0]) {
          e.preventDefault();
          pick(results[0]);
        }
      }}
    />
    <button
      type="button"
      class="tone"
      bind:this={toneButton}
      aria-label={t("media.skinTone")}
      aria-expanded={choosingTone}
      title={t("media.skinTone")}
      onclick={() => (choosingTone = !choosingTone)}><Emoji text={TONES[skinTone.value] ?? TONES[0]!} /></button
    >
    {#if choosingTone}
      <div class="tones" role="group" aria-label={t("media.skinTones")}>
        {#each TONES as tone, i (tone)}
          <button
            type="button"
            aria-label={i === 0 ? t("media.noSkinTone") : t("media.skinToneNumber", { number: i })}
            aria-pressed={skinTone.value === i}
            onclick={() => (skinTone.set(i), (choosingTone = false), search?.focus())}><Emoji text={tone} /></button
          >
        {/each}
      </div>
    {/if}
  </div>
  {#if !results && sections.length}
    <nav class="groups" aria-label={t("media.emojiCategories")}>
      {#each sections as s, i (s.name)}
        <button type="button" title={s.name} aria-label={s.name} onclick={() => jump(i)}><Emoji text={s.icon} /></button>
      {/each}
    </nav>
  {/if}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="grid" bind:this={grid} onkeydown={onGridKey}>
    {#snippet cells(list: EmojiEntry[])}
      <div class="cells">
        {#each list as e (e.emoji)}
          {@const emoji = withTone(e, skinTone.value)}
          <button
            type="button"
            class="cell"
            tabindex="-1"
            class:selected={selected(normalEmoji(emoji))}
            aria-label={e.name}
            onmouseenter={() => (shown = { emoji, name: e.name })}
            onfocus={() => (shown = { emoji, name: e.name })}
            onclick={() => pick(e)}><Emoji text={emoji} lazy /></button
          >
        {/each}
      </div>
    {/snippet}
    {#if failed}
      <p class="note">{t("media.emojiListFailed")}</p>
    {:else if !data}
      <p class="note">{t("common.loading")}</p>
    {:else if results}
      {#if results.length}{@render cells(results)}{:else}<p class="note">{t("media.noEmojiFound")}</p>{/if}
    {:else}
      {#each sections as s, i (s.name)}
        <section data-section={i}>
          <h3>{s.name}</h3>
          {@render cells(s.emoji)}
        </section>
      {/each}
    {/if}
  </div>
  <footer aria-hidden="true">
    {#if shown}<span class="big"><Emoji text={shown.emoji} /></span><span class="ellipsis">{shown.name}</span>{/if}
  </footer>
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
    position: relative;
    display: flex;
    gap: 8px;
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
  .tone,
  .tones button,
  .groups button,
  .cell {
    display: grid;
    padding: 4px;
    place-items: center;
    border: none;
    border-radius: 4px;
    background: none;
    cursor: pointer;
  }
  .tone :global(.emoji),
  .tones :global(.emoji),
  .groups :global(.emoji) {
    width: 22px;
    height: 22px;
  }
  .tones {
    position: absolute;
    top: 48px;
    right: 12px;
    z-index: 1;
    display: flex;
    padding: 4px;
    border-radius: 6px;
    background: var(--bg-input);
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.3);
  }
  .tones button[aria-pressed="true"],
  .cell.selected {
    background: rgb(88 101 242 / 0.3);
  }
  .groups {
    display: flex;
    gap: 2px;
    padding: 0 8px 4px;
    border-bottom: 1px solid var(--bg-active);
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
    grid-template-columns: repeat(auto-fill, 40px);
  }
  .cell {
    width: 40px;
    height: 40px;
  }
  .cell :global(.emoji) {
    width: 32px;
    height: 32px;
  }
  .tone:hover,
  .tones button:hover,
  .groups button:hover,
  .cell:hover,
  .cell:focus-visible {
    background: var(--bg-active);
  }
  .note {
    padding: 16px 4px;
    color: var(--text-muted);
    text-align: center;
  }
  footer {
    display: flex;
    gap: 8px;
    align-items: center;
    height: 44px;
    padding: 0 12px;
    background: var(--bg-input);
    font-size: 14px;
  }
  .big :global(.emoji) {
    width: 28px;
    height: 28px;
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
    footer {
      display: none;
    }
  }
</style>
