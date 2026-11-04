<script lang="ts">
  import ReactionList from "./ReactionList.svelte";
  import { t } from "../lib/i18n.svelte";
  import type { InfoRow } from "../lib/messageInfo";
  import type { Reaction } from "../lib/reactions";

  // A message's info, as in Signal: when it was sent, edited and pinned
  // (see lib/messageInfo), and who reacted with what.
  let { author, rows, reactions, onClose }: { author: string; rows: InfoRow[]; reactions: readonly Reaction[]; onClose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => dialog?.showModal());
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="message-info-title">
  <header>
    <h2 id="message-info-title">{t("chat.messageFrom", { author })}</h2>
    <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  <dl>
    {#each rows as row (row.label)}
      <dt>{row.label}</dt>
      <dd>{row.value}</dd>
    {/each}
  </dl>
  {#if reactions.length}
    <h3>{t("chat.reactions")}</h3>
    <ReactionList {reactions} />
  {/if}
</dialog>

<style>
  dialog {
    width: min(420px, 92vw);
    max-height: 90vh;
    padding: 24px;
    border: none;
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding-bottom: 12px;
  }
  h2 {
    margin: 0;
    font-size: 18px;
    overflow-wrap: anywhere;
  }
  h3 {
    margin: 16px 0 8px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .close {
    font-size: 24px;
    line-height: 1;
    color: var(--text-muted);
  }
  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px 16px;
    margin: 0;
  }
  dt {
    color: var(--text-muted);
  }
  dd {
    margin: 0;
  }
</style>
