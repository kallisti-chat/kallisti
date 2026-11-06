<script lang="ts">
  import ContextMenu from "./ContextMenu.svelte";
  import Icon from "./Icon.svelte";
  import type { SheetItem } from "./MessageSheet.svelte";
  import { t } from "../lib/i18n.svelte";

  // Desktop: a message's actions (its "More" button, a right-click): the
  // phone sheet's list (see MessageSheet). An action that asks first
  // (confirm) asks here.
  let { x, y, anchor, items, onClose }: { x: number; y: number; anchor?: HTMLElement; items: SheetItem[]; onClose: () => void } = $props();
  let asking = $state<SheetItem | null>(null);
  const run = (item: SheetItem) => {
    if (item.confirm && asking !== item) return void (asking = item);
    onClose();
    item.run();
  };
</script>

<ContextMenu {x} {y} {anchor} label={t("chat.messageActions")} {onClose}>
  {#if asking}
    <p class="ask">{asking.confirm}</p>
    <button type="button" class="danger" onclick={() => run(asking!)}>{asking.label}</button>
    <button type="button" onclick={() => (asking = null)}>{t("common.cancel")}</button>
  {:else}
    {#each items as item (item.label)}
      <button type="button" class:danger={item.danger} onclick={() => run(item)}><Icon name={item.icon} size={18} /><span>{item.label}</span></button>
    {/each}
  {/if}
</ContextMenu>

<style>
  button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    text-align: left;
    color: var(--text-muted);
  }
  button:hover,
  button:focus-visible {
    background: var(--accent-strong);
    color: white;
  }
  .danger {
    color: var(--danger);
  }
  .danger:hover,
  .danger:focus-visible {
    background: var(--danger);
  }
  .ask {
    margin: 0;
    padding: 8px;
    font-size: 14px;
    color: var(--text);
  }
</style>
