<script lang="ts" module>
  import type Icon from "./Icon.svelte";
  import type { ComponentProps } from "svelte";

  // confirm: asked first (in the sheet), for actions that cannot be undone.
  export type SheetItem = {
    label: string;
    icon: ComponentProps<typeof Icon>["name"];
    run: () => void;
    danger?: boolean;
    confirm?: string;
  };
</script>

<script lang="ts">
  import { tick } from "svelte";
  import BottomSheet from "./BottomSheet.svelte";
  import Emoji from "./Emoji.svelte";
  import EmojiPicker from "./EmojiPicker.svelte";
  import IconView from "./Icon.svelte";
  import { t } from "../lib/i18n.svelte";

  // Phones: a message's actions, from a long press. Quick reactions and
  // "+" (all emoji) when reacting is allowed (onReact), then the actions.
  // The caller closes it after an action (onClose).
  let {
    quick,
    mine,
    onReact,
    items,
    onClose,
  }: {
    // Two rows: 8 and the "+", then 9 (see quickRows).
    quick: { first: string[]; second: string[] };
    mine: (emoji: string) => boolean;
    onReact?: (emoji: string) => void;
    items: SheetItem[];
    onClose: () => void;
  } = $props();

  let confirming = $state<SheetItem | null>(null);
  let picker = $state<DOMRect | null>(null);
  let sheet: BottomSheet | undefined = $state();

  // The confirm step replaces the focused button: focus stays in the sheet.
  const confirm = async (item: SheetItem | null) => {
    confirming = item;
    await tick();
    sheet?.focus();
  };
  const run = (item: SheetItem) => {
    if (item.confirm && confirming !== item) void confirm(item);
    else item.run();
  };
</script>

{#snippet react(emoji: string)}
  <button type="button" class:mine={mine(emoji)} aria-label={t("chat.reactWith", { emoji })} onclick={() => onReact?.(emoji)}><Emoji text={emoji} /></button>
{/snippet}

<BottomSheet label={t("chat.messageActions")} {onClose} bind:this={sheet}>
  {#if confirming}
    <p class="question">{confirming.confirm}</p>
    <button type="button" class="sheet-item danger" onclick={() => confirming && run(confirming)}>{confirming.label}</button>
    <button type="button" class="sheet-item" onclick={() => confirm(null)}>{t("common.cancel")}</button>
  {:else}
    {#if onReact}
      <div class="quick" role="group" aria-label={t("chat.reactions")}>
        {#each quick.first as emoji (emoji)}{@render react(emoji)}{/each}
        <button
          type="button"
          class="more"
          aria-label={t("chat.moreEmoji")}
          onclick={(e) => (picker = e.currentTarget.getBoundingClientRect())}><IconView name="plus" size={22} /></button
        >
        {#each quick.second as emoji (emoji)}{@render react(emoji)}{/each}
      </div>
    {/if}
    <ul class="sheet-list">
      {#each items as item (item.label)}
        <li>
          <button type="button" class="sheet-item" class:danger={item.danger} onclick={() => run(item)}>
            <IconView name={item.icon} size={20} />{item.label}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</BottomSheet>
{#if picker && onReact}
  <EmojiPicker anchor={picker} label={t("chat.addReaction")} selected={mine} onPick={onReact} onClose={() => (picker = null)} />
{/if}

<style>
  /* 9 columns: as wide as the phone allows, at most 44 px. */
  .quick {
    display: grid;
    grid-template-columns: repeat(9, minmax(0, 44px));
    justify-content: space-between;
    gap: 8px 4px;
    margin-bottom: 12px;
  }
  .quick button {
    display: grid;
    width: 100%;
    aspect-ratio: 1;
    padding: 0;
    place-items: center;
    border: none;
    border-radius: 50%;
    background: var(--bg-input);
    color: var(--text-muted);
  }
  .quick button.mine {
    background: rgb(88 101 242 / 0.3);
  }
  .quick :global(.emoji) {
    width: 60%;
    height: auto;
  }
  .danger {
    color: var(--danger);
  }
  .question {
    margin: 0 4px 12px;
    font-size: 16px;
    font-weight: 600;
  }
  .question ~ .sheet-item {
    margin-top: 8px;
    border-radius: 8px;
    background: var(--bg-input);
    justify-content: center;
  }
</style>
