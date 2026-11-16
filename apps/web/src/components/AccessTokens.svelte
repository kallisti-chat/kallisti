<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import type { AccessToken, AccountActions } from "../lib/accounts";

  // The settings' access tokens for the agent API: programs (agents,
  // scripts) read and post as this account; their posts show a Bot badge.
  // A new token shows once.
  let { tokens }: { tokens: NonNullable<AccountActions["tokens"]> } = $props();

  let list = $state<AccessToken[]>([]);
  let failed = $state(false);
  let error = $state<string | null>(null);
  let busy = $state(false);
  let name = $state("");
  let write = $state(false);
  let made = $state<{ token: string; copied: boolean } | null>(null);

  const load = () => {
    failed = false;
    tokens.list().then(
      (got) => (list = got),
      () => (failed = true),
    );
  };
  $effect(load);
  const run = async (f: () => Promise<void>) => {
    busy = true;
    error = null;
    try {
      await f();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  };
  const make = (event: SubmitEvent) => {
    event.preventDefault();
    void run(async () => {
      const { token, ...entry } = await tokens.make(name.trim(), write);
      list = [...list, entry];
      made = { token, copied: false };
      name = "";
      write = false;
    });
  };
  const revoke = (id: number) =>
    run(async () => {
      await tokens.revoke(id);
      list = list.filter((x) => x.id !== id);
    });
  const copy = () =>
    run(async () => {
      if (!made || !navigator.clipboard) return;
      await navigator.clipboard.writeText(made.token);
      made.copied = true;
    });
</script>

<div class="account">
  <span class="label">{t("settings.accessTokens")}</span>
  <p class="hint">{t("settings.accessTokensHint")}</p>
  {#if failed}
    <p class="hint" role="alert">{t("settings.accessTokensNotLoaded")}</p>
    <div><button type="button" class="secondary" onclick={load}>{t("common.retry")}</button></div>
  {:else}
    {#if list.length > 0}
      <ul aria-label={t("settings.accessTokens")}>
        {#each list as x (x.id)}
          <li>
            <span class="what"><strong>{x.name}</strong> · {x.write ? t("settings.tokenReadWrite") : t("settings.tokenRead")}</span>
            <button type="button" class="secondary" disabled={busy} onclick={() => revoke(x.id)}>{t("common.remove")}</button>
          </li>
        {/each}
      </ul>
    {/if}
    {#if made}
      <div class="row">
        <input readonly value={made.token} aria-label={t("settings.newAccessToken")} onfocus={(e) => e.currentTarget.select()} />
        <button type="button" class="secondary" onclick={copy}>{made.copied ? t("common.copied") : t("common.copy")}</button>
      </div>
      <p class="hint">{t("settings.accessTokenShownOnce")}</p>
    {/if}
    <form class="row" onsubmit={make}>
      <input bind:value={name} required maxlength="64" placeholder={t("settings.accessTokenName")} aria-label={t("settings.accessTokenName")} />
      <label class="check"><input type="checkbox" bind:checked={write} /> {t("settings.tokenMayPost")}</label>
      <button type="submit" class="secondary" disabled={busy || !name.trim()}>{t("settings.makeAccessToken")}</button>
    </form>
  {/if}
  {#if error}<p class="hint" role="alert">{error}</p>{/if}
</div>

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
  li,
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  li {
    justify-content: space-between;
  }
  .what {
    overflow-wrap: anywhere;
  }
  .row input:not([type="checkbox"]) {
    flex: 1;
    min-width: 10rem;
    padding: 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
  }
  .check input {
    accent-color: var(--accent);
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
