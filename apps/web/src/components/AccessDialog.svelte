<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import { readerGroupLabel, readerRow, type ReaderGroup } from "../lib/members";

  // Who has access to a private channel, for its members (read-only; staff
  // and the channel's moderators change it in the Members dialog): its
  // members and the staff, online first (see readerGroups).
  let { name, groups, onClose }: { name: string; groups: readonly ReaderGroup<{ name: string; session: number }>[]; onClose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => dialog?.showModal());
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="access-dialog-title">
  <header>
    <h2 id="access-dialog-title">{t("settings.accessTitle", { name })}</h2>
    <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  <p class="note">{t("settings.accessNote")}</p>
  {#each groups as g (g.title)}
    <h3>{t("settings.accessGroupHeading", { group: readerGroupLabel(g.title), count: g.rows.length })}</h3>
    <ul aria-label={readerGroupLabel(g.title)}>
      {#each g.rows as r (readerRow(r).key)}
        <li class:offline={"offline" in r}><span class="ellipsis">{readerRow(r).name}</span></li>
      {/each}
    </ul>
  {/each}
</dialog>

<style>
  dialog {
    width: min(440px, 92vw);
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
  h3 {
    margin: 12px 0 6px;
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
  .note {
    margin: 0 0 4px;
    font-size: 14px;
    color: var(--text-muted);
  }
  ul {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    max-width: 100%;
    padding: 4px 10px;
    border-radius: 12px;
    background: var(--bg-rail);
    font-size: 14px;
  }
  li.offline {
    opacity: 0.55;
  }
</style>
