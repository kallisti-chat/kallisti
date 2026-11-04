<script lang="ts">
  import BottomSheet from "./BottomSheet.svelte";
  import ReactionList from "./ReactionList.svelte";
  import { t } from "../lib/i18n.svelte";
  import type { Reaction } from "../lib/reactions";

  // Phones: who reacted (a long press on a reaction, as in Discord).
  // emojiMenu: a custom emoji's own menu (add it to one's emoji, …), which
  // the long press opened before.
  let {
    reactions,
    start,
    emojiMenu,
    onClose,
  }: { reactions: readonly Reaction[]; start?: string; emojiMenu?: (emoji: string) => (() => void) | null; onClose: () => void } = $props();
  let chosen = $state<string>();
  const menu = $derived(chosen ? emojiMenu?.(chosen) : null);
</script>

<BottomSheet label={t("chat.reactions")} {onClose}>
  <h2 class="title">{t("chat.reactions")}</h2>
  <ReactionList {reactions} {start} bind:chosen />
  {#if menu}
    <button type="button" class="sheet-item" onclick={menu}>{t("chat.emojiOptions")}</button>
  {/if}
</BottomSheet>

<style>
  .title {
    margin: 0 0 12px;
    font-size: 16px;
  }
</style>
