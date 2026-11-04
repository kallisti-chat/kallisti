<script lang="ts">
  import { EMOJI_TYPES, emojiNameFromFile, type CustomEmoji } from "../lib/customEmoji";
  import type { Key } from "../i18n/en";
  import { t } from "../lib/i18n.svelte";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import EmojiImage from "./EmojiImage.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  // A list of emoji to add to, rename (when onRename) and delete: the
  // server's (server settings) or ours (settings). formError says what is
  // wrong with a new one before the upload (the server checks again).
  let {
    list,
    formError,
    onAdd,
    onDelete,
    onRename,
    empty,
  }: {
    list: readonly CustomEmoji[];
    formError: (name: string, file: File | null) => string | null;
    onAdd: (name: string, file: File) => Promise<void>;
    onDelete: (name: string) => Promise<void>;
    onRename?: (name: string, to: string) => Promise<void>;
    empty: string;
  } = $props();

  const add = createSave();
  const change = createSave();
  let name = $state("");
  let file = $state<File | null>(null);
  let confirming = $state<string | null>(null);
  let renaming = $state<{ name: string; to: string } | null>(null);
  let fileInput: HTMLInputElement | undefined = $state();
  let image: EmojiImage | undefined = $state();
  let done = $state<Key>("admin.deleted");

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    const n = name.trim();
    add.run(async () => {
      // Checked as uploaded: still images are scaled down first.
      const out = (await image?.output()) ?? null;
      const problem = formError(n, out);
      if (problem || !out) throw new Error(problem ?? t("admin.chooseImage"));
      await onAdd(n, out);
      name = "";
      file = null;
      if (fileInput) fileInput.value = "";
    });
  };
  const rename = (e: SubmitEvent) => {
    e.preventDefault();
    if (!renaming || !onRename) return;
    const { name: from, to } = renaming;
    renaming = null;
    done = "admin.renamed";
    if (to.trim() && to.trim() !== from) change.run(() => onRename(from, to.trim()));
  };
</script>

<form class="add-emoji" onsubmit={submit}>
  <input
    bind:this={fileInput}
    type="file"
    disabled={add.busy}
    accept={EMOJI_TYPES.join(",")}
    aria-label={t("admin.emojiImage")}
    onchange={(e) => {
      file = e.currentTarget.files?.[0] ?? null;
      if (file && !name) name = emojiNameFromFile(file.name);
    }}
  />
  <input bind:value={name} placeholder={t("admin.namePlaceholder")} aria-label={t("admin.emojiName")} maxlength="32" />
  <button type="submit" disabled={add.busy}>{busyLabel(add.state, t("common.add"), t("admin.adding"))}</button>
  <SaveStatus save={add.state} done={t("admin.added")} />
</form>
<p class="note">{t("admin.emojiNote")}</p>
<EmojiImage bind:this={image} {file} name={name.trim()} />
{#if list.length === 0}
  <p class="note">{empty}</p>
{:else}
  <ul>
    {#each list as e (e.name)}
      <li>
        <img class="emoji-image" src={e.url} alt="" />
        {#if renaming?.name === e.name}
          <form class="rename" onsubmit={rename}>
            <!-- svelte-ignore a11y_autofocus -->
            <input bind:value={renaming.to} aria-label={t("admin.emojiNewName", { name: e.name })} maxlength="32" autofocus />
            <button type="submit">{t("common.save")}</button>
            <button type="button" onclick={() => (renaming = null)}>{t("common.cancel")}</button>
          </form>
        {:else}
          <div class="what"><strong>:{e.name}:</strong></div>
          {#if confirming === e.name}
            <button
              type="button"
              class="danger"
              disabled={change.busy}
              onclick={() => ((done = "admin.deleted"), change.run(() => onDelete(e.name)), (confirming = null))}
            >
              {t("common.delete")}
            </button>
            <button type="button" onclick={() => (confirming = null)}>{t("common.cancel")}</button>
          {:else}
            {#if onRename}<button type="button" onclick={() => (renaming = { name: e.name, to: e.name })}>{t("common.rename")}</button>{/if}
            <button type="button" onclick={() => (confirming = e.name)}>{t("admin.deleteAsk")}</button>
          {/if}
        {/if}
      </li>
    {/each}
  </ul>
{/if}
<SaveStatus save={change.state} done={t(done)} />

<style>
  .add-emoji {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
  input:not([type="file"]) {
    flex: 1;
    min-width: 8em;
    padding: 6px 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  input[type="file"] {
    max-width: 100%;
  }
  button {
    padding: 6px 10px;
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
  }
  .danger {
    background: var(--danger);
    color: white;
  }
  .note {
    color: var(--text-muted);
    font-size: 14px;
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .what {
    flex: 1;
    min-width: 0;
  }
  .rename {
    display: flex;
    flex: 1;
    gap: 6px;
  }
  .emoji-image {
    width: 32px;
    height: 32px;
    object-fit: contain;
  }
</style>
