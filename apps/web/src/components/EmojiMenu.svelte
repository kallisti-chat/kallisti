<script lang="ts">
  import { untrack } from "svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import SaveStatus from "./SaveStatus.svelte";
  import { customEmojiFor } from "../lib/customEmoji";
  import { emojiActions, personalFormError } from "../lib/personalEmoji";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import { t } from "../lib/i18n.svelte";

  // A custom emoji in a message or reaction (":name:" or "<:name:id>"):
  // add it to our emoji (onAdd; the name can change on a clash), or
  // remove a personal one everywhere (onRemove: moderators).
  let {
    x,
    y,
    emoji,
    onAdd,
    onRemove,
    onClose,
  }: {
    x: number;
    y: number;
    emoji: string;
    onAdd?: (emoji: string, name: string) => Promise<void>;
    onRemove?: (emoji: string) => Promise<void>;
    onClose: () => void;
  } = $props();

  const custom = customEmojiFor();
  const actions = $derived(emojiActions(emoji, custom(), { add: !!onAdd, remove: !!onRemove }));
  let name = $state(untrack(() => actions?.add ?? ""));
  let confirming = $state(false);
  let added = $state(false);
  const save = createSave();

  const add = (e: SubmitEvent) => {
    e.preventDefault();
    const n = name.trim();
    save.run(async () => {
      const problem = personalFormError(n, null, custom().mine, custom().urls, false);
      if (problem) throw new Error(problem);
      await onAdd?.(emoji, n);
      added = true; // a server emoji stays "not ours"
    });
  };
  const remove = () => save.run(async () => (await onRemove?.(emoji), onClose()));
</script>

{#if actions}
  <ContextMenu {x} {y} label=":{actions.name}:" {onClose}>
    <div class="head">
      <img src={actions.url} alt="" />
      <div>
        <strong>:{actions.name}:</strong>
        <span>{actions.kind === "server" ? t("media.serverEmoji") : actions.mine ? t("media.oneOfYourEmoji") : t("media.personalEmoji")}</span>
      </div>
    </div>
    {#if onAdd && (actions.add !== null || save.state.phase !== "idle")}
      <form onsubmit={add}>
        {#if actions.add !== null && !added}
          <input bind:value={name} aria-label={t("media.nameInYourEmoji")} maxlength="32" />
          <button type="submit" class="primary" disabled={save.busy}>{busyLabel(save.state, t("media.addToMyEmoji"), t("media.addingEmoji"))}</button>
        {/if}
        <SaveStatus save={save.state} done={t("media.addedToYourEmoji")} />
      </form>
    {/if}
    {#if actions.remove}
      {#if confirming}
        <p class="note">{t("media.removeEmojiNote", { emoji: `:${actions.name}:` })}</p>
        <button type="button" class="item danger" disabled={save.busy} onclick={remove}>{t("media.removeForEveryone")}</button>
        <button type="button" class="item" onclick={() => (confirming = false)}>{t("common.cancel")}</button>
      {:else}
        <button type="button" class="item danger" onclick={() => (confirming = true)}>{t("media.removeForEveryoneAsk")}</button>
      {/if}
    {/if}
  </ContextMenu>
{/if}

<style>
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px;
  }
  .head img {
    width: 48px;
    height: 48px;
    object-fit: contain;
  }
  .head div {
    display: flex;
    flex-direction: column;
  }
  .head span,
  .note {
    font-size: 12px;
    color: var(--text-muted);
  }
  .note {
    max-width: 240px;
    margin: 4px 6px;
  }
  form {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 6px;
  }
  input {
    flex: 1;
    min-width: 8em;
    padding: 6px 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  .primary {
    padding: 6px 10px;
    border-radius: 4px;
    background: var(--accent-strong);
    color: white;
  }
  .item {
    width: 100%;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    text-align: left;
    color: var(--text-muted);
  }
  .item:hover {
    background: var(--accent-strong);
    color: white;
  }
  .danger {
    color: var(--danger);
  }
  .danger:hover {
    background: var(--danger);
  }
</style>
