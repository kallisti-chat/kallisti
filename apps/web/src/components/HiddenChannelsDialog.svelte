<script lang="ts">
  import type { ListKind } from "../lib/channelList";
  import { t } from "../lib/i18n.svelte";
  import Icon from "./Icon.svelte";

  // The channels the user hid from their list (see lib/channelList), each
  // to show again.
  let {
    channels,
    onShow,
    onClose,
  }: { channels: readonly { kind: ListKind; id: number; name: string }[]; onShow: (kind: ListKind, id: number) => void; onClose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => dialog?.showModal());
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="hidden-dialog-title">
  <header>
    <h2 id="hidden-dialog-title">{t("admin.hiddenTitle")}</h2>
    <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  <p class="note">{t("admin.hiddenNote")}</p>
  {#if channels.length}
    <ul>
      {#each channels as c (`${c.kind}${c.id}`)}
        <li>
          <span class="name">
            <Icon name={c.kind === "text" ? "hash" : "speaker"} size={16} /><span class="sr-only">{c.kind === "text" ? t("admin.textChannel") : t("admin.voiceChannel")}</span>
            <span class="ellipsis">{c.name}</span>
          </span>
          <button type="button" class="secondary" aria-label={t("admin.showNamed", { name: c.name })} onclick={() => onShow(c.kind, c.id)}>{t("admin.show")}</button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="note">{t("admin.hiddenNone")}</p>
  {/if}
</dialog>

<style>
  dialog {
    width: min(400px, 92vw);
    max-height: 80vh;
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
  .note {
    margin: 0 0 12px;
    font-size: 14px;
    color: var(--text-muted);
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    color: var(--text-muted);
  }
  .name .ellipsis {
    color: var(--text);
  }
  .secondary {
    padding: 6px 12px;
    border-radius: 6px;
    background: var(--bg-input);
    font-weight: 600;
  }
</style>
