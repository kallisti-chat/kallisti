<script lang="ts">
  import { untrack } from "svelte";
  import { i18n, t } from "../lib/i18n.svelte";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import type { Webhook } from "../lib/webhooks";
  import SaveStatus from "./SaveStatus.svelte";

  // A text channel's webhooks (staff, see lib/webhooks): the list, a new
  // one (its URL shown once), its picture, rename, delete. Each change
  // applies at once.
  let {
    name,
    webhooks = null,
    created = null,
    onLoad = () => {},
    onCreate,
    onRename,
    onDelete,
    picture,
    onPicture,
    onRemovePicture,
    onClose,
  }: {
    name: string; // the channel's, as shown
    webhooks?: readonly Webhook[] | null; // null: not loaded yet
    created?: { id: number; url: string } | null; // the new one's URL
    onLoad?: () => void;
    onCreate: (name: string) => Promise<void>;
    onRename: (id: number, name: string) => Promise<void>;
    onDelete: (id: number) => Promise<void>;
    picture: (id: number) => string; // its address
    onPicture: (id: number, file: File) => Promise<void>;
    onRemovePicture: (id: number) => Promise<void>;
    onClose: () => void;
  } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => dialog?.showModal());
  $effect(() => untrack(onLoad));

  let newName = $state("");
  let copied = $state(false);
  const copy = (url: string) => void navigator.clipboard?.writeText(url).then(() => (copied = true));
  // The one being renamed (its new name), the one asked to delete.
  let renaming = $state<{ id: number; name: string } | null>(null);
  let deleting = $state<number | null>(null);
  const changes = createSave();
  const create = (event: SubmitEvent) => {
    event.preventDefault();
    const n = newName.trim();
    if (!n) return;
    copied = false;
    changes.run(() => onCreate(n).then(() => (newName = "")));
  };
  const rename = (event: SubmitEvent) => {
    event.preventDefault();
    const r = renaming;
    if (r?.name.trim()) changes.run(() => onRename(r.id, r.name.trim()).then(() => (renaming = null)));
  };
  const when = (at: number) => new Date(at).toLocaleDateString(i18n.locale);
  // A new picture under the same address: shown anew.
  let version = $state(0);
  const choosePicture = (id: number, el: HTMLInputElement) => {
    const file = el.files?.[0];
    el.value = ""; // the same file again is a new upload
    if (file) changes.run(() => onPicture(id, file).then(() => void version++));
  };
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="webhooks-dialog-title">
  <header>
    <h2 id="webhooks-dialog-title">{t("admin.webhooksTitle", { name })}</h2>
    <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  <p class="note">{t("admin.webhooksNote")}</p>
  <!-- Until it is deleted. -->
  {#if created && (webhooks === null || webhooks.some((w) => w.id === created.id))}
    <div class="row">
      <input readonly value={created.url} aria-label={t("admin.newWebhookUrl")} onfocus={(e) => e.currentTarget.select()} />
      <button type="button" class="action" onclick={() => copy(created.url)}>{copied ? t("common.copied") : t("common.copy")}</button>
    </div>
    <p class="note">{t("admin.webhookCopyNow")}</p>
  {/if}
  <form class="row" onsubmit={create}>
    <input bind:value={newName} maxlength="80" placeholder={t("admin.webhookNamePlaceholder")} aria-label={t("admin.webhookNameLabel")} />
    <button type="submit" class="action" disabled={changes.busy || !newName.trim()}>{busyLabel(changes.state, t("common.create"), t("admin.working"))}</button>
  </form>
  <SaveStatus save={changes.state} />
  {#if webhooks === null}
    <p class="note">{t("admin.loadingWebhooks")}</p>
  {:else if webhooks.length}
    <ul aria-label={t("admin.webhooksList")}>
      {#each webhooks as w (w.id)}
        <li>
          {#if renaming?.id === w.id}
            <form class="row" onsubmit={rename}>
              <!-- svelte-ignore a11y_autofocus -->
              <input bind:value={renaming.name} maxlength="80" aria-label={t("admin.webhookNewName", { name: w.name })} autofocus />
              <button type="submit" class="action" disabled={changes.busy}>{t("common.save")}</button>
              <button type="button" class="action" onclick={() => (renaming = null)}>{t("common.cancel")}</button>
            </form>
          {:else}
            {#if w.picture}
              <img class="picture" src="{picture(w.id)}?v={version}" alt="" />
            {:else}
              <span class="picture none" aria-hidden="true"></span>
            {/if}
            <span class="what">
              <span class="ellipsis">{w.name}</span>
              <span class="sub">{t("admin.webhookBy", { creator: w.creator, date: when(w.created) })}</span>
            </span>
            {#if deleting === w.id}
              <button type="button" class="danger" disabled={changes.busy} onclick={() => changes.run(() => onDelete(w.id).then(() => (deleting = null)))}>
                {t("admin.deleteIt")}
              </button>
              <button type="button" onclick={() => (deleting = null)}>{t("admin.keep")}</button>
            {:else}
              <label class="file">
                {t("admin.picture")}
                <input
                  type="file"
                  accept="image/png,image/jpeg,image/gif,image/webp"
                  aria-label={t("admin.pictureOf", { name: w.name })}
                  disabled={changes.busy}
                  onchange={(e) => choosePicture(w.id, e.currentTarget)}
                />
              </label>
              {#if w.picture}
                <button type="button" aria-label={t("admin.removePictureOf", { name: w.name })} onclick={() => changes.run(() => onRemovePicture(w.id))}>{t("admin.noPicture")}</button>
              {/if}
              <button type="button" onclick={() => ((renaming = { id: w.id, name: w.name }), (deleting = null))}>{t("common.rename")}</button>
              <button type="button" onclick={() => ((deleting = w.id), (renaming = null))} aria-label={t("admin.deleteNamed", { name: w.name })}>{t("common.delete")}</button>
            {/if}
          {/if}
        </li>
      {/each}
    </ul>
  {:else}
    <p class="note">{t("admin.noWebhooks")}</p>
  {/if}
</dialog>

<style>
  dialog {
    width: min(480px, 92vw);
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
  p {
    margin: 0 0 12px;
    font-size: 14px;
  }
  .note {
    color: var(--text-muted);
  }
  .row {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
  }
  .row > * {
    min-width: 0;
  }
  .row input {
    flex: 1;
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  .action {
    flex: none;
    padding: 10px 12px;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 12px 0 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 6px 6px 10px;
    border-radius: 4px;
    background: var(--bg-rail);
    font-size: 14px;
  }
  li form {
    flex: 1;
    margin: 0;
  }
  li .action,
  li input {
    background: var(--bg-main);
  }
  .what {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }
  .sub {
    font-size: 12px;
    color: var(--text-muted);
  }
  li > button {
    padding: 4px 8px;
    border-radius: 4px;
    color: var(--text-muted);
  }
  li > button:hover {
    color: var(--text);
  }
  li > .danger {
    color: var(--danger);
  }
  .picture {
    flex: none;
    width: 28px;
    height: 28px;
    margin-right: 6px;
    border-radius: 50%;
  }
  .picture.none {
    background: var(--bg-active);
  }
  .file {
    position: relative;
    padding: 4px 8px;
    border-radius: 4px;
    color: var(--text-muted);
    cursor: pointer;
  }
  .file:hover,
  .file:focus-within {
    color: var(--text);
  }
  .file input {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
  }
</style>
