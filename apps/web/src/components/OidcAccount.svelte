<script lang="ts">
  import type { AccountActions } from "../lib/accounts";
  import type { OidcProvider } from "../lib/oidc";
  import { t } from "../lib/i18n.svelte";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  let { account }: { account: AccountActions } = $props();
  let provider = $state<OidcProvider>({ enabled: false, name: "" });
  let password = $state("");
  const save = createSave();
  $effect(() => {
    const actions = account.oidc;
    let current = true;
    provider = { enabled: false, name: "" };
    if (actions) void actions.provider().then((value) => { if (current) provider = value; });
    return () => { current = false; };
  });
  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    const actions = account.oidc;
    if (!actions) return;
    save.run(async () => {
      if (actions.linked) await actions.unlink(password);
      else await actions.link(password);
      password = "";
    });
  };
</script>

{#if account.oidc && !account.temporary && (account.oidc.linked || provider.enabled)}
  <form class="account" onsubmit={submit}>
    <span class="label">{t("settings.oidcAccount")}</span>
    {#if account.oidc.linked}<p class="provider">{account.oidc.linked.provider}</p>{/if}
    {#if account.oidc.linked?.enabled === false}
      <p class="hint">{t("settings.oidcUnavailable")}</p>
    {/if}
    {#if account.hasPassword}
      <p class="hint">{account.oidc.linked ? t("settings.currentPassword") : t("settings.oidcLinkHint")}</p>
      <input type="password" bind:value={password} placeholder={t("settings.currentPassword")} aria-label={t("settings.currentPassword")} autocomplete="current-password" required />
      <div class="buttons">
        <button type="submit" class="secondary" disabled={save.busy}>
          {t(account.oidc.linked ? "settings.oidcUnlink" : "settings.oidcLink", { provider: account.oidc.linked?.provider ?? provider.name })}
        </button>
        <SaveStatus save={save.state} done="" />
      </div>
    {:else}
      <p class="hint">{t("settings.oidcPasswordFirst")}</p>
    {/if}
  </form>
{/if}

<style>
  .account { display: flex; flex-direction: column; gap: 8px; padding-top: 12px; margin-bottom: 16px; border-top: 1px solid var(--border-subtle); }
  input { padding: 10px; border: none; border-radius: 4px; background: var(--bg-rail); }
  .label { font-size: 12px; font-weight: 600; text-transform: uppercase; color: var(--text-muted); }
  .provider { margin: 0; font-weight: 600; overflow-wrap: anywhere; }
  .buttons { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; }
  .secondary { padding: 8px 14px; border-radius: 6px; background: var(--bg-input); font-weight: 600; }
  .hint { margin: 0; font-size: 12px; color: var(--text-faint); }
</style>
