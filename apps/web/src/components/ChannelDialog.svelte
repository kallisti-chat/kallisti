<script lang="ts">
  import { untrack } from "svelte";
  import type { ChannelForm, ChannelKind } from "../lib/channelForm";
  import type { ListenOnly, Speaker } from "../lib/listenOnly";
  import { SLOW_MODES, slowModeLabel } from "../lib/slowMode";
  import { around } from "../lib/i18n";
  import { t } from "../lib/i18n.svelte";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  // Create, edit or delete a channel or a text category.

  let {
    form,
    onSubmit,
    onClose,
  }: {
    form: ChannelForm;
    onSubmit: (v: {
      kind: ChannelKind;
      name: string;
      description: string;
      temporary: boolean;
      listenOnly?: ListenOnly;
      private?: boolean;
      slowMode?: number;
      category?: number;
    }) => Promise<void>;
    onClose: () => void;
  } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  // The fields start from the form; the dialog is made anew for each one.
  const start = untrack(() => form);
  // Staff making a text channel may make a category instead.
  let kind = $state(start.kind);
  // A text channel's category (0: none); the choice is for staff.
  const categories = start.mode === "delete" ? undefined : start.categories;
  const startCategory = start.mode === "delete" ? undefined : start.category;
  let category = $state(startCategory ?? 0);
  let name = $state(start.mode === "create" ? "" : start.name);
  let description = $state(start.mode === "edit" ? start.description : "");
  // Voice channels with the built-in server.
  const startListenOnly = start.mode === "edit" ? start.listenOnly : undefined;
  let listenOnly = $state(startListenOnly?.on ?? false);
  let speakers = $state<Speaker[]>(startListenOnly?.speakers ?? []);
  const addable = $derived((startListenOnly?.users ?? []).filter((u) => !speakers.some((s) => s.id === u.id)));
  const addSpeaker = (event: Event & { currentTarget: HTMLSelectElement }) => {
    const u = addable.find((a) => String(a.id) === event.currentTarget.value);
    if (u) speakers = [...speakers, u];
    event.currentTarget.value = "";
  };
  // Admins and moderators (built-in server): a private channel.
  const canPrivate = start.mode === "create" ? start.canPrivate === true : start.mode === "edit" && !!start.private;
  let isPrivate = $state(start.mode === "edit" ? (start.private?.on ?? false) : false);
  // Text channels (built-in server): undefined, no field.
  const startSlowMode = start.mode === "edit" ? start.slowMode : undefined;
  let slowMode = $state(startSlowMode ?? 0);
  // Users who may only make temporary voice channels get one of those.
  const temporary = $derived(form.mode === "create" && form.temporaryOnly && kind === "voice" && !isPrivate);
  $effect(() => dialog?.showModal());

  const title = $derived(
    form.mode === "create" && form.parent
      ? t("admin.createSubchannel")
      : kind === "category"
        ? t(form.mode === "create" ? "admin.createCategory" : form.mode === "edit" ? "admin.editCategory" : "admin.deleteCategory")
        : t(form.mode === "create" ? "admin.createChannel" : form.mode === "edit" ? "admin.editChannel" : "admin.deleteChannel"),
  );
  // The delete question, around the channel's name.
  const deleteAsk = $derived(
    around(t(form.kind === "voice" ? "admin.deleteVoiceAsk" : form.kind === "category" ? "admin.deleteCategoryAsk" : "admin.deleteTextAsk"), "name"),
  );
  // Closes once the server confirms; a refusal shows here.
  const save = createSave();
  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    if (form.mode !== "delete" && !name.trim()) return;
    const values = {
      kind,
      name: name.trim(),
      description,
      temporary,
      ...(startListenOnly ? { listenOnly: { on: listenOnly, speakers } } : {}),
      ...(canPrivate ? { private: isPrivate } : {}),
      ...(startSlowMode !== undefined ? { slowMode } : {}),
      ...(startCategory !== undefined && kind === "text" ? { category } : {}),
    };
    save.run(() => onSubmit(values).then(() => dialog?.close()));
  };
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="channel-dialog-title">
  <form onsubmit={submit}>
    <header>
      <h2 id="channel-dialog-title">{title}</h2>
      <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
    </header>
    {#if form.mode === "delete"}
      <p>{deleteAsk[0]}<strong>{form.kind === "text" ? `#${form.name}` : form.name}</strong>{deleteAsk[1]}</p>
    {:else}
      {#if start.mode === "create" && start.canCategory}
        <div class="kinds" role="radiogroup" aria-label={t("admin.whatToCreate")}>
          <label class="check"><input type="radio" bind:group={kind} value="text" /> {t("admin.kindText")}</label>
          <label class="check"><input type="radio" bind:group={kind} value="category" /> {t("admin.kindCategory")}</label>
        </div>
      {/if}
      <label>
        {kind === "category" ? t("admin.categoryName") : t("admin.channelName")}
        <!-- svelte-ignore a11y_autofocus -->
        <input bind:value={name} maxlength={kind === "voice" ? 100 : 32} required autofocus readonly={start.mode === "edit" && start.topicOnly}
          placeholder={kind === "text" ? t("admin.textChannelPlaceholder") : kind === "category" ? t("admin.categoryPlaceholder") : t("admin.channelPlaceholder")} />
      </label>
      {#if categories?.length && kind === "text"}
        <label>
          {t("admin.categoryField")}
          <select bind:value={category}>
            <option value={0}>{t("admin.noCategory")}</option>
            {#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
          </select>
        </label>
      {/if}
      {#if form.mode === "edit" && kind !== "category"}
        <label>
          {kind === "text" ? t("admin.topic") : t("admin.description")}
          <textarea bind:value={description} rows="3" maxlength={kind === "text" ? 200 : undefined}></textarea>
        </label>
      {/if}
      {#if startSlowMode !== undefined}
        <label>
          {t("admin.slowMode")}
          <select bind:value={slowMode}>
            {#each SLOW_MODES as s (s)}<option value={s}>{slowModeLabel(s)}</option>{/each}
          </select>
          <span class="note hint">{t("admin.slowModeHint")}</span>
        </label>
      {/if}
      {#if canPrivate}
        <label class="check">
          <input type="checkbox" bind:checked={isPrivate} />
          <span>
            {kind === "category" ? t("admin.privateCategory") : t("admin.privateChannel")}
            <span class="note">
              {kind === "category" ? t("admin.privateCategoryNote") : t("admin.privateChannelNote")}
            </span>
          </span>
        </label>
      {/if}
      {#if startListenOnly}
        <label class="check">
          <input type="checkbox" bind:checked={listenOnly} />
          <span>
            {t("admin.listenOnly")}
            <span class="note">{t("admin.listenOnlyNote")}</span>
          </span>
        </label>
        {#if listenOnly}
          <div class="speakers">
            <span class="label">{t("admin.whoCanSpeak")}</span>
            {#if speakers.length}
              <ul>
                {#each speakers as s (s.id)}
                  <li>
                    {s.name}
                    <button type="button" aria-label={t("admin.removeNamed", { name: s.name })} onclick={() => (speakers = speakers.filter((x) => x.id !== s.id))}>×</button>
                  </li>
                {/each}
              </ul>
            {/if}
            {#if addable.length}
              <select aria-label={t("admin.addSpeaker")} value="" onchange={addSpeaker}>
                <option value="" disabled>{t("admin.addSpeakerOption")}</option>
                {#each addable as u (u.id)}<option value={String(u.id)}>{u.name}</option>{/each}
              </select>
            {:else if !speakers.length}
              <p class="note">{t("admin.nobodyCanSpeak")}</p>
            {/if}
          </div>
        {/if}
      {/if}
      {#if temporary}
        <p class="note">{t("admin.temporaryNote")}</p>
      {/if}
    {/if}
    <footer>
      <SaveStatus save={save.state} />
      <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
      <button type="submit" class:danger={form.mode === "delete"} disabled={save.busy}>
        {form.mode === "create"
          ? busyLabel(save.state, t("common.create"), t("admin.creating"))
          : form.mode === "edit"
            ? busyLabel(save.state, t("common.save"), t("common.saving"))
            : busyLabel(save.state, t("common.delete"), t("admin.deleting"))}
      </button>
    </footer>
  </form>
</dialog>

<style>
  dialog {
    width: min(440px, 92vw);
    padding: 24px;
    border: none;
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  header,
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  header {
    padding-bottom: 16px;
  }
  footer {
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
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
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0 0 16px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  input,
  textarea {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    text-transform: none;
  }
  .check {
    flex-direction: row;
    align-items: flex-start;
    gap: 10px;
    font-size: 14px;
    font-weight: normal;
    text-transform: none;
    color: var(--text);
  }
  .check input {
    margin-top: 3px;
  }
  .kinds {
    display: flex;
    gap: 24px;
  }
  .check .note {
    display: block;
    font-size: 12px;
  }
  .hint {
    font-weight: normal;
    text-transform: none;
  }
  .speakers {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0 0 16px;
  }
  .label {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .speakers ul {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .speakers li {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 4px 4px 10px;
    border-radius: 12px;
    background: var(--bg-rail);
    font-size: 14px;
  }
  .speakers li button {
    padding: 0 6px;
    color: var(--text-muted);
  }
  select {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  p {
    margin: 0 0 16px;
    font-size: 14px;
  }
  .note {
    color: var(--text-muted);
  }
  footer button {
    padding: 10px 16px;
    border-radius: 4px;
    background: var(--accent-strong);
    color: white;
    font-weight: 600;
  }
  footer .secondary {
    background: none;
    color: var(--text);
  }
  footer .danger {
    background: var(--danger);
  }
</style>
