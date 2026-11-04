<script lang="ts">
  import { tick } from "svelte";
  import Emoji from "./Emoji.svelte";
  import { t } from "../lib/i18n.svelte";
  import { pollOpen, share, showResults, timeLeft, winners, type Poll } from "../lib/polls";

  // A poll in the chat (see lib/polls): pick answers and vote; the counts
  // show after one's own vote, on "Show results" or at the end. onVote:
  // missing where we cannot vote (guests, read-only); onEnd: the author's.
  let { poll, onVote, onEnd }: { poll: Poll; onVote?: (answers: number[]) => void; onEnd?: () => void } = $props();

  let now = $state(Date.now());
  const open = $derived(pollOpen(poll, now));
  $effect(() => {
    if (!open) return;
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });
  let picked = $state<number[]>([]);
  let peek = $state(false);
  let ending = $state(false); // "End poll" asks first
  let box: HTMLFieldSetElement | undefined = $state();
  const results = $derived(showResults(poll, now) || peek);
  const top = $derived(open ? [] : winners(poll));
  const name = `poll-${Math.random().toString(36).slice(2)}`;
  // Our vote arrived: the picks are done (a refused vote keeps them).
  $effect(() => {
    if (poll.mine?.length) picked = [];
  });
  // Buttons that go away take the focus with them: it goes to the poll.
  const then = async (f: () => void) => {
    f();
    await tick();
    box?.focus();
  };
  const pick = (i: number, on: boolean) => {
    picked = !poll.multiple ? [i] : on ? [...new Set([...picked, i])].sort((a, b) => a - b) : picked.filter((x) => x !== i);
  };
</script>

<fieldset class="poll" bind:this={box} tabindex="-1" aria-label={t("chat.pollLabel", { question: poll.question })}>
  <legend class="question">{poll.question}</legend>
  <p class="how">
    {poll.multiple ? t("chat.pollPickSeveral") : t("chat.pollPickOne")} ·
    {poll.anonymous ? t("chat.pollVotesAnonymous") : t("chat.pollVotesPublic")}
  </p>
  <ul>
    {#each poll.answers as answer, i (i)}
      {@const percent = share(answer.votes, poll.voters)}
      {@const chosen = poll.mine?.includes(i)}
      <li>
        {#if results}
          <div class="result" class:won={top.includes(i)} class:chosen title={answer.voters?.join(", ")}>
            <span class="bar" style:width="{percent}%"></span>
            <span class="label"
              >{#if answer.emoji}<span class="emoji"><Emoji text={answer.emoji} /></span>{/if}{answer.text}{#if chosen}<span class="sr-only"> {t("chat.pollYourVote")}</span>
                <span aria-hidden="true">✓</span>{/if}</span
            >
            <span class="count">{t("chat.pollAnswerCount", { count: answer.votes, percent })}</span>
          </div>
          {#if answer.voters?.length}<p class="voters">{answer.voters.join(", ")}</p>{/if}
        {:else}
          <label class="answer" class:on={picked.includes(i)} class:off={!onVote || !open}>
            <input
              type={poll.multiple ? "checkbox" : "radio"}
              {name}
              checked={picked.includes(i)}
              disabled={!onVote || !open}
              onchange={(e) => pick(i, e.currentTarget.checked)}
            />
            {#if answer.emoji}<span class="emoji"><Emoji text={answer.emoji} /></span>{/if}{answer.text}
          </label>
        {/if}
      </li>
    {/each}
  </ul>
  <div class="foot">
    <span class="status">{t("chat.pollStatus", { count: poll.voters, status: open ? timeLeft(poll.ends, now) : t("chat.pollEnded") })}</span>
    {#if ending}
      <span>{t("chat.pollEndConfirm")}</span>
      <button type="button" class="primary" onclick={() => then(() => ((ending = false), onEnd?.()))}>{t("chat.pollEnd")}</button>
      <button type="button" onclick={() => then(() => (ending = false))}>{t("common.cancel")}</button>
    {:else}
      {#if open && onVote && poll.mine?.length}
        <button type="button" onclick={() => then(() => onVote([]))}>{t("chat.pollRemoveVote")}</button>
      {:else if open && peek}
        <button type="button" onclick={() => then(() => (peek = false))}>{t("chat.pollBackToVoting")}</button>
      {:else if open && !results}
        <button type="button" onclick={() => then(() => (peek = true))}>{t("chat.pollShowResults")}</button>
        {#if onVote}
          <button type="button" class="primary" disabled={!picked.length} onclick={() => then(() => onVote(picked))}>{t("chat.pollVote")}</button>
        {/if}
      {/if}
      {#if open && onEnd}<button type="button" onclick={() => (ending = true)}>{t("chat.pollEnd")}</button>{/if}
    {/if}
  </div>
</fieldset>

<style>
  .poll {
    min-width: 0;
    border: none;
    max-width: 440px;
    margin-top: 4px;
    padding: 12px 14px;
    border-radius: 8px;
    background: var(--bg-sidebar);
  }
  .question {
    float: left; /* a legend in the box, as a heading */
    width: 100%;
    margin: 0;
    padding: 0;
    font-weight: 700;
    overflow-wrap: anywhere;
  }
  .how {
    clear: both;
    margin: 2px 0 10px;
    font-size: 12px;
    color: var(--text-muted);
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .answer,
  .result {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 40px;
    padding: 6px 10px;
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    background: var(--bg-main);
    color: var(--text);
    font: inherit;
    text-align: left;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  .answer {
    cursor: pointer;
  }
  .answer.off {
    cursor: default;
  }
  .answer:not(.off):hover,
  .answer.on,
  .answer:focus-within {
    border-color: var(--accent);
  }
  .answer input {
    flex: none;
    margin: 0;
    accent-color: var(--accent);
  }
  .bar {
    position: absolute;
    inset: 0 auto 0 0;
    background: color-mix(in srgb, var(--accent) 22%, transparent);
  }
  .result.won .bar {
    background: color-mix(in srgb, var(--accent) 40%, transparent);
  }
  .result.chosen {
    border-color: var(--accent);
  }
  .label,
  .count {
    position: relative;
  }
  .label {
    flex: 1;
  }
  .count {
    font-size: 12px;
    color: var(--text-muted);
    white-space: nowrap;
  }
  .emoji {
    margin-right: 6px;
  }
  .emoji :global(img.emoji) {
    width: 20px;
    height: 20px;
    vertical-align: middle;
  }
  .voters {
    margin: 2px 0 0 10px;
    font-size: 12px;
    color: var(--text-muted);
  }
  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-top: 10px;
    font-size: 13px;
  }
  .status {
    flex: 1;
    color: var(--text-muted);
  }
  .foot button {
    padding: 4px 10px;
    border-radius: 4px;
    background: var(--bg-main);
    color: var(--text);
  }
  .foot button.primary {
    background: var(--accent-strong);
    color: #fff;
  }
  .foot button:disabled {
    opacity: 0.5;
  }
</style>
