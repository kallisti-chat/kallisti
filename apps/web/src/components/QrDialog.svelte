<script lang="ts">
  import type { Snippet } from "svelte";
  import { t } from "../lib/i18n.svelte";

  // The frame of the QR login dialogs (see QrShowDialog, QrLinkDialog):
  // a title, a close button, the body (its state announced).
  let { title, onClose, children }: { title: string; onClose: () => void; children: Snippet } = $props();
  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => dialog?.showModal());
  const id = $props.id();
</script>

<dialog class="qr" bind:this={dialog} onclose={onClose} aria-labelledby="{id}-title">
  <header>
    <h2 id="{id}-title">{title}</h2>
    <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  <div role="status" aria-live="polite">{@render children()}</div>
</dialog>

<style>
  dialog {
    width: min(360px, 92vw);
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
    padding-bottom: 8px;
  }
  h2 {
    margin: 0;
    font-size: 18px;
  }
  .close {
    font-size: 24px;
    line-height: 1;
    color: var(--text-muted);
  }
  /* The bodies' parts (written in the dialogs that use the frame). */
  dialog :global(p) {
    margin: 0 0 12px;
    font-size: 14px;
  }
  dialog :global(.note) {
    color: var(--text-muted);
  }
  dialog :global(.warn) {
    color: var(--danger);
  }
  dialog :global(.code) {
    display: grid;
    place-items: center;
    margin: 8px 0 16px;
  }
  dialog :global(.buttons) {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  dialog :global(.buttons button) {
    padding: 10px 16px;
    border-radius: 4px;
    font-weight: 600;
  }
  dialog :global(.primary) {
    background: var(--accent-strong);
    color: white;
  }
  dialog :global(.secondary) {
    background: none;
    color: var(--text);
  }
</style>
