<script lang="ts">
  import { untrack } from "svelte";
  import { pickedEmoji } from "../lib/emojiPrefs.svelte";
  import { CLEAR_AFTER, MAX_STATUS_TEXT, clearAfterUntil, type ClearAfter, type CustomStatus } from "../lib/presence";
  import Emoji from "./Emoji.svelte";
  import { i18n, t } from "../lib/i18n.svelte";
  import EmojiPicker from "./EmojiPicker.svelte";

  // Set a custom status: an emoji, a text, and when it goes. start: the
  // custom status there is (an edit keeps its end unless changed).
  let {
    start,
    onSave,
    onClose,
  }: { start: CustomStatus | null; onSave: (c: CustomStatus) => void; onClose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  const first = untrack(() => start);
  let emoji = $state(first?.emoji ?? "");
  let text = $state(first?.text ?? "");
  const keep = first?.until;
  const options = $derived([
    ...(keep ? [{ value: "keep" as const, label: t("client.keepEnd", { time: new Date(keep).toLocaleString(i18n.locale, { dateStyle: "short", timeStyle: "short" }) }) }] : []),
    ...CLEAR_AFTER.map((c) => ({ value: c.value, label: t(c.label) })),
  ]);
  let clearAfter = $state<ClearAfter | "keep">(keep ? "keep" : first ? "never" : "today");
  let picker = $state<DOMRect | null>(null);
  $effect(() => dialog?.showModal());

  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    const until = clearAfter === "keep" ? keep : clearAfterUntil(clearAfter, Date.now());
    onSave({ ...(emoji && { emoji }), ...(text.trim() && { text: text.trim() }), ...(until && { until }) });
    dialog?.close();
  };
  const pick = (e: string) => {
    pickedEmoji(e);
    emoji = e;
    picker = null;
  };
</script>

<!-- Escape in the emoji picker closes the picker, not the dialog. -->
<dialog
  bind:this={dialog}
  onclose={onClose}
  onkeydowncapture={(e) => picker && e.key === "Escape" && e.preventDefault()}
  aria-labelledby="status-dialog-title"
>
  <form onsubmit={submit}>
    <header>
      <h2 id="status-dialog-title">{t("client.customStatusTitle")}</h2>
      <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
    </header>
    <div class="row">
      <button
        type="button"
        class="emoji"
        aria-label={emoji ? t("client.changeEmoji") : t("client.chooseEmoji")}
        onclick={(e) => (picker = e.currentTarget.getBoundingClientRect())}
        ><Emoji text={emoji || "🙂"} /></button
      >
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={text} maxlength={MAX_STATUS_TEXT} placeholder={t("client.statusPlaceholder")} aria-label={t("client.statusText")} autofocus />
      {#if emoji || text}
        <button type="button" class="clear" aria-label={t("client.clearStatusFields")} onclick={() => ((emoji = ""), (text = ""))}>×</button>
      {/if}
    </div>
    <label>
      {t("client.clearAfter")}
      <select bind:value={clearAfter}>
        {#each options as c (c.value)}<option value={c.value}>{c.label}</option>{/each}
      </select>
    </label>
    <footer>
      <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
      <button type="submit" disabled={!emoji && !text.trim()}>{t("common.save")}</button>
    </footer>
  </form>
  {#if picker}
    <EmojiPicker anchor={picker} personal={false} onPick={pick} onClose={() => (picker = null)} />
  {/if}
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
  .row {
    display: flex;
    align-items: center;
    gap: 4px;
    margin: 0 0 16px;
    padding: 0 6px;
    border-radius: 4px;
    background: var(--bg-rail);
  }
  .row input {
    flex: 1;
    min-width: 0;
    padding: 10px 4px;
    border: none;
    background: none;
    outline: none;
  }
  .emoji {
    display: inline-flex;
    padding: 4px;
    border-radius: 4px;
    font-size: 20px;
  }
  .emoji:hover {
    background: var(--bg-active);
  }
  .clear {
    padding: 0 6px;
    font-size: 20px;
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
  select {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    text-transform: none;
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
</style>
