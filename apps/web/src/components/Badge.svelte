<script lang="ts">
  import { t } from "../lib/i18n.svelte";

  // Shows nothing for 0 or a missing count.
  let { count, label = "unread" }: { count: number | undefined; label?: "unread" | "mentions" | "unread messages" } = $props();
  const KEYS = { unread: "sidebar.badgeUnread", mentions: "sidebar.badgeMentions", "unread messages": "sidebar.badgeUnreadMessages" } as const;
</script>

{#if count}
  <span class="badge" title={t(KEYS[label], { count })}>{count}</span>
{/if}

<style>
  .badge {
    min-width: 16px;
    padding: 0 4px;
    border-radius: 8px;
    background: var(--danger);
    color: white;
    font-size: 11px;
    font-weight: 700;
    line-height: 16px;
    text-align: center;
  }
</style>
