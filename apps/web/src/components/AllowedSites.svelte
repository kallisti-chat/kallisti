<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import type { AccountActions, AllowedSite } from "../lib/accounts";

  // The settings' list of other servers this account logs in to (Kallisti
  // as a login provider); one taken back asks again at its next login.
  // Nothing shown while there are none (or until they loaded).
  let { sites }: { sites: NonNullable<AccountActions["sites"]> } = $props();

  let list = $state<AllowedSite[]>([]);
  let error = $state<string | null>(null);
  let busy = $state<string | null>(null);
  // Not loaded: the section shows the error, to try again.
  let failed = $state(false);
  const load = () => {
    failed = false;
    sites.list().then(
      (got) => (list = got),
      () => (failed = true),
    );
  };
  $effect(load);
  const revoke = async (redirect: string) => {
    busy = redirect;
    error = null;
    try {
      await sites.revoke(redirect);
      list = list.filter((s) => s.redirect !== redirect);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = null;
    }
  };
</script>

{#if failed}
  <div class="account">
    <span class="label">{t("settings.allowedSites")}</span>
    <p class="hint" role="alert">{t("settings.allowedSitesNotLoaded")}</p>
    <div><button type="button" class="secondary" onclick={load}>{t("common.retry")}</button></div>
  </div>
{:else if list.length > 0}
  <div class="account">
    <span class="label">{t("settings.allowedSites")}</span>
    <ul>
      {#each list as s (s.redirect)}
        <li>
          <span class="site">{new URL(s.site).host}</span>
          <button type="button" class="secondary" disabled={busy !== null} onclick={() => revoke(s.redirect)}>
            {busy === s.redirect ? t("settings.removing") : t("common.remove")}
          </button>
        </li>
      {/each}
    </ul>
    {#if error}<p class="hint" role="alert">{error}</p>{/if}
    <p class="hint">{t("settings.allowedSitesHint")}</p>
  </div>
{/if}

<style>
  .account {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 12px;
    margin-bottom: 16px;
    border-top: 1px solid var(--border-subtle);
  }
  .label {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .site {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .secondary {
    padding: 6px 12px;
    border-radius: 6px;
    background: var(--bg-input);
    font-weight: 600;
  }
  .hint {
    margin: 0;
    font-size: 12px;
    color: var(--text-faint);
  }
</style>
