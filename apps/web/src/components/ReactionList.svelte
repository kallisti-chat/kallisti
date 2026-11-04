<script lang="ts">
  import { untrack } from "svelte";
  import Emoji from "./Emoji.svelte";
  import { t } from "../lib/i18n.svelte";
  import type { Reaction } from "../lib/reactions";

  // Who reacted with what, as in Discord: a button per emoji (from
  // `start`), the names below. chosen: the emoji shown (bindable; another
  // one when its reaction goes away).
  let { reactions, start, chosen = $bindable() }: { reactions: readonly Reaction[]; start?: string; chosen?: string } = $props();
  chosen = untrack(() => start ?? reactions[0]?.emoji ?? "");
  const shown = $derived(reactions.find((r) => r.emoji === chosen) ?? reactions[0]);
  $effect(() => {
    if (shown && shown.emoji !== chosen) chosen = shown.emoji;
  });
  const id = `reactions-${Math.random().toString(36).slice(2)}`;
</script>

<div class="tabs" role="group" aria-label={t("chat.reactions")}>
  {#each reactions as r, i (r.emoji)}
    <button type="button" id="{id}-{i}" aria-pressed={r === shown} aria-controls="{id}-names" onclick={() => (chosen = r.emoji)}
      ><Emoji text={r.emoji} /> {r.users.length}</button
    >
  {/each}
</div>
{#if shown}
  <ul class="names" id="{id}-names" aria-labelledby="{id}-{reactions.indexOf(shown)}">
    {#each shown.users as name, i (`${name}-${i}`)}
      <li>{name}</li>
    {/each}
  </ul>
{/if}

<style>
  .tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 8px;
  }
  .tabs button {
    display: flex;
    align-items: center;
    gap: 4px;
    min-height: 32px;
    padding: 2px 10px;
    border: 1px solid var(--border-subtle);
    border-radius: 16px;
    background: var(--bg-main);
    color: var(--text);
  }
  .tabs button[aria-pressed="true"] {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 18%, transparent);
  }
  .tabs :global(img.emoji) {
    width: 18px;
    height: 18px;
  }
  .names {
    max-height: 40vh;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
  }
  .names li {
    padding: 8px 4px;
    border-bottom: 1px solid var(--border-subtle);
  }
</style>
