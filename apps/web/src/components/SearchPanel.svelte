<script lang="ts">
  import { messageFiles, type TextMessage } from "../lib/textChannels";
  import { previewText } from "../lib/markdown";
  import { spoilerUpload } from "../lib/media";
  import { formatTimestamp } from "../lib/chat";
  import { around } from "../lib/i18n";
  import { settings } from "../lib/settings.svelte";
  import Avatar from "./Avatar.svelte";
  import { i18n, t } from "../lib/i18n.svelte";

  // Search results, in place of the member list. With a heading: another
  // list of messages (the pinned ones).
  let {
    text,
    heading,
    label,
    closeLabel,
    none,
    messages,
    where,
    canOpen,
    onOpen,
    onClose,
    inert = false,
  }: {
    text: string | null; // null: searching (loading)
    heading?: string;
    label?: string; // default: search results
    closeLabel?: string; // the close button's label
    none?: string;
    messages: readonly TextMessage[];
    // Where a message is ("#general", a voice channel's name).
    where: (m: TextMessage) => string;
    // Results in text channels open there; voice chats only show here.
    canOpen: (m: TextMessage) => boolean;
    onOpen: (m: TextMessage) => void;
    onClose: () => void;
    inert?: boolean; // in a closed phone drawer
  } = $props();
  const now = Date.now();
  const shownLabel = $derived(label ?? t("chat.searchResults"));
  const shownClose = $derived(closeLabel ?? (label === undefined ? t("chat.closeSearchResults") : t("chat.closePanel", { panel: label })));
  let closeButton: HTMLButtonElement | undefined = $state();
  $effect(() => closeButton?.focus());
</script>

<!-- "<author> in <where> · <time>": the author bold. -->
{#snippet meta(m: TextMessage)}
  {@const [before, after] = around(t("chat.searchResultMeta", { where: where(m), time: formatTimestamp(m.at, now, i18n.locale, settings.value.timeFormat) }), "author")}
  {before}<strong>{m.author}</strong>{after}
{/snippet}

<aside class="search" aria-label={shownLabel} {inert}>
  <header aria-live="polite">
    <span class="ellipsis">
      {#if text === null}{heading ? t("common.loading") : t("chat.searching")}{:else if heading}{heading}{:else}{t("chat.searchResultsFor", { count: messages.length, text })}{/if}
    </span>
    <button type="button" aria-label={shownClose} bind:this={closeButton} onclick={onClose}>×</button>
  </header>
  {#if text !== null && messages.length === 0}
    <p class="none">{none ?? t("chat.noMessagesFound")}</p>
  {:else}
    <ul>
      {#each messages as m (m.id)}
        <li>
          <button
            type="button"
            class="hover-row"
            disabled={!canOpen(m)}
            title={canOpen(m) ? undefined : t("chat.searchResultInVoiceChat")}
            onclick={() => onOpen(m)}
          >
            <Avatar name={m.author} size={24} />
            <span class="body">
              <span class="meta">{@render meta(m)}</span>
              <span class="text">{previewText(m.text) || (messageFiles(m)[0] ? spoilerUpload(messageFiles(m)[0]!).name : "")}</span>
            </span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</aside>

<style>
  .search {
    --avatar-cutout: var(--bg-main);
    overflow-y: auto;
    padding: 8px;
    background: var(--bg-main);
    border-left: 1px solid var(--border);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 4px 8px 8px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-muted);
  }
  header button {
    font-size: 20px;
    line-height: 1;
    color: var(--text-muted);
  }
  .none {
    padding: 8px;
    color: var(--text-muted);
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li button {
    display: flex;
    gap: 8px;
    width: 100%;
    padding: 8px;
    border-radius: 6px;
    text-align: left;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .meta {
    font-size: 12px;
    color: var(--text-muted);
  }
  .meta strong {
    color: var(--text);
  }
  .text {
    font-size: 14px;
    overflow-wrap: anywhere;
  }
</style>
