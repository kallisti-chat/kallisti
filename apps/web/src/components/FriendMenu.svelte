<script lang="ts">
  import ContextMenu from "./ContextMenu.svelte";
  import { t } from "../lib/i18n.svelte";

  // A friend on another server (see lib/friends): a direct chat there
  // (undefined: no login there), or out of the list.
  let {
    name,
    server,
    x,
    y,
    onMessage,
    onRemove,
    onClose,
  }: { name: string; server: string; x: number; y: number; onMessage?: () => void; onRemove: () => void; onClose: () => void } = $props();

  const act = (fn: () => void) => () => {
    fn();
    onClose();
  };
</script>

<ContextMenu {x} {y} label={t("sidebar.actionsFor", { name })} {onClose}>
  {#if onMessage}
    <button onclick={act(onMessage)}>{t("sidebar.messageOn", { server })}</button>
  {:else}
    <p class="note">{t("sidebar.addServerToWrite", { server, name })}</p>
  {/if}
  <button onclick={act(onRemove)}>{t("sidebar.removeFriend")}</button>
</ContextMenu>

<style>
  button {
    display: flex;
    align-items: center;
    width: 100%;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    text-align: left;
    color: var(--text-muted);
    cursor: pointer;
  }
  button:hover {
    background: var(--accent-strong);
    color: white;
  }
  .note {
    margin: 0;
    padding: 4px 8px;
    font-size: 13px;
    color: var(--text-muted);
  }
</style>
