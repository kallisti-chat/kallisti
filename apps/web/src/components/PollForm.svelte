<script lang="ts">
  import { tick } from "svelte";
  import Emoji from "./Emoji.svelte";
  import EmojiPicker from "./EmojiPicker.svelte";
  import { t } from "../lib/i18n.svelte";
  import { emptyPollDraft, pollBody, pollProblem, POLL_ANSWER_MAX, POLL_ANSWERS_MAX, POLL_DURATIONS, POLL_QUESTION_MAX, type PollBody } from "../lib/polls";

  // A new poll (see lib/polls): question, answers with an emoji each,
  // duration, multiple choice, anonymous votes. onSubmit settles once the
  // server stored it (or fails with why: the form stays).
  // nonce: the same for each try of this form (a retry after no answer
  // cannot post the poll twice).
  let { onSubmit, onClose }: { onSubmit: (poll: PollBody, nonce: string) => Promise<void>; onClose: () => void } = $props();
  const nonce = crypto.randomUUID();

  let dialog: HTMLDialogElement | undefined = $state();
  let draft = $state(emptyPollDraft());
  let error = $state("");
  let sending = $state(false);
  // The answer whose emoji is being picked (in the dialog: outside it, all
  // is inert), and where its button is.
  let picking = $state<{ i: number; anchor: DOMRect } | null>(null);
  $effect(() => dialog?.showModal());
  const problem = $derived(pollProblem(draft));
  const addAnswer = async () => {
    draft.answers.push({ text: "" });
    await tick();
    dialog?.querySelector<HTMLInputElement>(".answer:last-of-type input.text")?.focus();
  };
  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    if (problem || sending) return void (error = problem ?? "");
    sending = true;
    error = "";
    try {
      await onSubmit(pollBody($state.snapshot(draft)), nonce);
      dialog?.close();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      sending = false;
    }
  };
</script>

<!-- Not closed while posting (a refusal shows here); Escape in the emoji
     picker closes only the picker. -->
<dialog
  bind:this={dialog}
  onclose={onClose}
  oncancel={(e) => {
    if (sending || picking) e.preventDefault();
    picking = null;
  }}
  aria-labelledby="poll-title"
>
  <form onsubmit={submit}>
    <header>
      <h2 id="poll-title">{t("chat.createPoll")}</h2>
      <button type="button" class="close" disabled={sending} onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
    </header>
    <label>
      {t("chat.pollQuestion")}
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={draft.question} maxlength={POLL_QUESTION_MAX} placeholder={t("chat.pollQuestionPlaceholder")} required autofocus />
    </label>
    <fieldset>
      <legend>{t("chat.pollAnswers")}</legend>
      {#each draft.answers as answer, i (i)}
        <div class="answer">
          <button type="button" class="emoji" aria-label={answer.emoji ? t("chat.pollChangeAnswerEmoji", { n: i + 1 }) : t("chat.pollAddAnswerEmoji", { n: i + 1 })}
            onclick={(e) => (picking = { i, anchor: e.currentTarget.getBoundingClientRect() })}
            >{#if answer.emoji}<Emoji text={answer.emoji} />{:else}<span class="add-emoji" aria-hidden="true">🙂</span>{/if}</button
          >
          {#if answer.emoji}
            <button type="button" class="remove" onclick={() => (answer.emoji = undefined)} aria-label={t("chat.pollRemoveAnswerEmoji", { n: i + 1 })}>⌫</button>
          {/if}
          <input class="text" bind:value={answer.text} maxlength={POLL_ANSWER_MAX} placeholder={t("chat.pollAnswerN", { n: i + 1 })} aria-label={t("chat.pollAnswerN", { n: i + 1 })} />
          {#if draft.answers.length > 2}
            <button type="button" class="remove" onclick={() => draft.answers.splice(i, 1)} aria-label={t("chat.pollRemoveAnswer", { n: i + 1 })}>×</button>
          {/if}
        </div>
      {/each}
      {#if draft.answers.length < POLL_ANSWERS_MAX}
        <button type="button" class="add" onclick={addAnswer}>{t("chat.pollAddAnswer")}</button>
      {/if}
    </fieldset>
    <label>
      {t("chat.pollDuration")}
      <select bind:value={draft.hours}>
        {#each POLL_DURATIONS as [hours, key] (hours)}
          <option value={hours}>{t(key)}</option>
        {/each}
      </select>
    </label>
    <label class="check"><input type="checkbox" bind:checked={draft.multiple} /> {t("chat.pollAllowMultiple")}</label>
    <label class="check"><input type="checkbox" bind:checked={draft.anonymous} /> {t("chat.pollAnonymous")}</label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if picking}
      {@const at = picking.i}
      <EmojiPicker
        anchor={picking.anchor}
        label={t("chat.pollAnswerEmoji", { n: at + 1 })}
        onPick={(emoji) => {
          if (draft.answers[at]) draft.answers[at].emoji = emoji;
          picking = null;
        }}
        onClose={() => (picking = null)}
      />
    {/if}
    <footer>
      <button type="button" class="secondary" disabled={sending} onclick={() => dialog?.close()}>{t("common.cancel")}</button>
      <button type="submit" class="primary" disabled={!!problem || sending}>{sending ? t("chat.pollPosting") : t("chat.pollPost")}</button>
    </footer>
  </form>
</dialog>

<style>
  dialog {
    width: min(480px, 92vw);
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
  label,
  legend {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0 0 16px;
  }
  label.check {
    flex-direction: row;
    align-items: center;
    gap: 8px;
    font-weight: 400;
    text-transform: none;
    color: var(--text);
    font-size: 14px;
  }
  fieldset {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0 0 16px;
    padding: 0;
    border: none;
  }
  legend {
    margin-bottom: 6px;
    padding: 0;
  }
  .answer {
    display: flex;
    gap: 6px;
  }
  input:not([type="checkbox"]),
  select {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    font-size: 14px; /* not the label's */
    font-weight: 400;
    text-transform: none;
  }
  button.emoji {
    flex: none;
    width: 3em;
    border-radius: 4px;
    background: var(--bg-rail);
    font-size: 18px;
  }
  .add-emoji {
    opacity: 0.4;
    filter: grayscale(1);
  }
  button.emoji :global(img.emoji) {
    width: 22px;
    height: 22px;
    vertical-align: middle;
  }
  input.text {
    flex: 1;
    min-width: 0;
  }
  .remove {
    padding: 0 8px;
    font-size: 20px;
    color: var(--text-muted);
  }
  .add {
    align-self: flex-start;
    padding: 4px 0;
    color: var(--link);
  }
  .error {
    margin: 0 0 8px;
    color: var(--danger);
  }
  footer button {
    padding: 10px 16px;
    border-radius: 4px;
    color: white;
    font-weight: 600;
  }
  footer .secondary {
    background: none;
    color: var(--text);
  }
  footer .primary {
    background: var(--accent-strong);
  }
  footer .primary:disabled {
    opacity: 0.5;
  }
</style>
