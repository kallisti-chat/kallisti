<script lang="ts">
  import type { Badge } from "../lib/roles";
  import { t } from "../lib/i18n.svelte";
  // A small label for admins and moderators; a channel's moderators get an
  // outlined one (only in that channel's member list).
  let { role }: { role: Badge | null } = $props();
</script>

{#if role === "channelmod"}
  <span class="tag channel" title={t("sidebar.channelModerator")}>{t("sidebar.tagMod")}</span>
{:else if role}
  <span class="tag" class:admin={role === "admin"}>{t(role === "admin" ? "sidebar.tagAdmin" : "sidebar.tagMod")}</span>
{/if}

<style>
  .tag {
    flex: none;
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--accent-strong);
    color: white;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
  }
  .admin {
    background: var(--danger);
  }
  .channel {
    padding: 0 4px;
    border: 1px solid var(--accent);
    background: none;
    color: var(--accent);
  }
</style>
