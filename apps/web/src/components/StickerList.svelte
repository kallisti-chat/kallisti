<script lang="ts">
  import { EMOJI_TYPES, emojiNameFromFile } from "../lib/customEmoji";
  import type { Key } from "../i18n/en";
  import { t } from "../lib/i18n.svelte";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import { MAX_STICKER_BYTES, STICKER_SIZE, bulkNames, stickerForm, type Sticker, type StickerFields } from "../lib/stickers";
  import { fitSticker } from "../lib/stickerImage";
  import EmojiImage from "./EmojiImage.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  // The server's stickers (moderation dialog): add, edit (name,
  // description, related emoji, pack) and delete. url: absolute.
  let {
    list,
    onAdd,
    onEdit,
    onDelete,
  }: {
    list: readonly Sticker[];
    onAdd: (fields: StickerFields, file: File) => Promise<void>;
    onEdit: (id: number, fields: StickerFields) => Promise<void>;
    onDelete: (id: number) => Promise<void>;
  } = $props();

  const blank = (): StickerFields => ({ name: "", description: "", emoji: "", pack: "" });
  const trimmed = (f: StickerFields): StickerFields => ({ name: f.name.trim(), description: f.description.trim(), emoji: f.emoji.trim(), pack: f.pack.trim() });
  const add = createSave();
  const change = createSave();
  let fields = $state(blank());
  // One file: with a preview and its own name; many: named after their
  // files, the other fields for all of them.
  let files = $state<File[]>([]);
  const file = $derived(files.length === 1 ? files[0]! : null);
  let progress = $state<{ done: number; total: number } | null>(null);
  let editing = $state<({ id: number } & StickerFields) | null>(null);
  let confirming = $state<number | null>(null);
  let fileInput: HTMLInputElement | undefined = $state();
  let image: EmojiImage | undefined = $state();
  let done = $state<Key>("admin.deleted");

  const addMany = async (f: StickerFields) => {
    const names = bulkNames(
      files.map((x) => x.name),
      list,
    );
    const failed: string[] = [];
    for (const [i, x] of files.entries()) {
      progress = { done: i + 1, total: files.length };
      try {
        const out = await fitSticker(x);
        if (out.size > MAX_STICKER_BYTES) throw new Error(t("admin.stickerOverLimit"));
        await onAdd({ ...f, name: names[i]! }, out);
      } catch (err) {
        failed.push(`${x.name} (${(err as Error).message})`);
      }
    }
    progress = null;
    files = [];
    if (fileInput) fileInput.value = "";
    if (failed.length) throw new Error(t("admin.stickersNotAdded", { list: failed.join(", ") }));
  };
  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    const f = trimmed(fields);
    if (files.length > 1) {
      const problem = stickerForm({ ...f, name: "ok" }, []);
      add.run(async () => {
        if (problem) throw new Error(problem);
        await addMany(f);
      });
      return;
    }
    add.run(async () => {
      const problem = stickerForm(f, list);
      if (problem) throw new Error(problem);
      // Checked as uploaded: still images are scaled down first.
      const out = (await image?.output()) ?? null;
      if (!out) throw new Error(t("admin.chooseImage"));
      if (out.size > MAX_STICKER_BYTES) throw new Error(t("admin.stickerTooBig"));
      await onAdd(f, out);
      fields = blank();
      files = [];
      if (fileInput) fileInput.value = "";
    });
  };
  const save = (e: SubmitEvent) => {
    e.preventDefault();
    if (!editing) return;
    const { id, ...rest } = editing;
    const f = trimmed(rest);
    const problem = stickerForm(f, list, id);
    done = "common.saved";
    change.run(async () => {
      if (problem) throw new Error(problem);
      await onEdit(id, f);
      editing = null;
    });
  };
</script>

<!-- sticker: the one edited (null: a new one), for the labels. -->
{#snippet fieldInputs(f: StickerFields, sticker: string | null)}
  {@const label = (fresh: Key, named: Key) => (sticker === null ? t(fresh) : t(named, { name: sticker }))}
  <input bind:value={f.name} placeholder={t("admin.namePlaceholder")} aria-label={label("admin.newStickerName", "admin.stickerName")} maxlength="32" />
  <input bind:value={f.emoji} placeholder={t("admin.stickerEmojiPlaceholder")} aria-label={label("admin.newStickerEmoji", "admin.stickerEmoji")} class="short" />
  <input bind:value={f.pack} placeholder={t("admin.stickerPackPlaceholder")} aria-label={label("admin.newStickerPack", "admin.stickerPack")} maxlength="32" />
  <input
    bind:value={f.description}
    placeholder={t("admin.stickerDescriptionPlaceholder")}
    aria-label={label("admin.newStickerDescription", "admin.stickerDescription")}
    maxlength="100"
    class="wide"
  />
{/snippet}

<form class="fields" onsubmit={submit}>
  <input
    bind:this={fileInput}
    type="file"
    disabled={add.busy}
    accept={EMOJI_TYPES.join(",")}
    aria-label={t("admin.stickerImages")}
    multiple
    onchange={(e) => {
      files = [...(e.currentTarget.files ?? [])];
      if (file && !fields.name) fields.name = emojiNameFromFile(file.name);
    }}
  />
  {@render fieldInputs(fields, null)}
  <button type="submit" disabled={add.busy}
    >{busyLabel(
      add.state,
      files.length > 1 ? t("admin.addCount", { count: files.length }) : t("common.add"),
      progress ? t("admin.addingProgress", progress) : t("admin.adding"),
    )}</button
  >
  <SaveStatus save={add.state} done={t("admin.added")} />
</form>
<p class="note">{t("admin.stickerNote", { size: STICKER_SIZE })}</p>
{#if files.length > 1}
  <p class="note">{t("admin.stickerFilesNote", { count: files.length })}</p>
{:else}
  <EmojiImage bind:this={image} {file} name={fields.name.trim()} max={STICKER_SIZE} limit="512 KB" />
{/if}
{#if list.length === 0}
  <p class="note">{t("admin.noStickers")}</p>
{:else}
  <ul>
    {#each list as s (s.id)}
      <li>
        <img class="sticker-image" src={s.url} alt="" />
        {#if editing?.id === s.id}
          <form class="fields" onsubmit={save}>
            {@render fieldInputs(editing, s.name)}
            <button type="submit" disabled={change.busy}>{t("common.save")}</button>
            <button type="button" onclick={() => (editing = null)}>{t("common.cancel")}</button>
          </form>
        {:else}
          <div class="what">
            <strong>{s.name}</strong>{#if s.emoji}&nbsp;{s.emoji}{/if}{#if s.pack}<span class="pack"> · {s.pack}</span>{/if}
            {#if s.description}<div class="description">{s.description}</div>{/if}
          </div>
          {#if confirming === s.id}
            <button
              type="button"
              class="danger"
              disabled={change.busy}
              onclick={() => ((done = "admin.deleted"), change.run(() => onDelete(s.id)), (confirming = null))}
            >
              {t("common.delete")}
            </button>
            <button type="button" onclick={() => (confirming = null)}>{t("common.cancel")}</button>
          {:else}
            <button
              type="button"
              onclick={() => (editing = { id: s.id, name: s.name, description: s.description ?? "", emoji: s.emoji ?? "", pack: s.pack ?? "" })}
              >{t("common.edit")}</button
            >
            <button type="button" onclick={() => (confirming = s.id)}>{t("admin.deleteAsk")}</button>
          {/if}
        {/if}
      </li>
    {/each}
  </ul>
{/if}
<SaveStatus save={change.state} done={t(done)} />

<style>
  .fields {
    display: flex;
    flex: 1;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
  input:not([type="file"]) {
    flex: 1;
    min-width: 6em;
    padding: 6px 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  input.short {
    flex: 0 1 5em;
    min-width: 4em;
  }
  input.wide {
    flex-basis: 100%;
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
  .pack,
  .description {
    color: var(--text-muted);
    font-size: 13px;
  }
  .sticker-image {
    width: 64px;
    height: 64px;
    object-fit: contain;
  }
</style>
