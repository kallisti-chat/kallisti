<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import type { AccountActions } from "../lib/accounts";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import { localStore, safeGet, safeSet } from "../lib/storage";
  import SaveStatus from "./SaveStatus.svelte";

  // The settings' fediverse account: link one (a login on the instance),
  // show its handle on the profile, unlink it (not the last way to log in).
  let { account }: { account: AccountActions } = $props();

  let enabled = $state(false);
  $effect(() => {
    void account.fediEnabled().then((on) => (enabled = on));
  });
  let instance = $state(safeGet(localStore(), "fediInstance") ?? "");
  let password = $state("");
  const linkSave = createSave();
  const link = (event: SubmitEvent) => {
    event.preventDefault();
    safeSet(localStore(), "fediInstance", instance.trim());
    linkSave.run(() => account.fediLink(instance.trim(), password));
  };
  const publicSave = createSave();
  const setPublic = (el: HTMLInputElement) => {
    const on = el.checked;
    publicSave.run(
      () => account.fediPublic(on),
      () => (el.checked = !on),
    );
  };
  const unlinkSave = createSave();
</script>

{#if account.fedi}
  <div class="account">
    <span class="label">{t("settings.fediAccount")}</span>
    <p class="handle">{account.fedi.handle}</p>
    <label class="check">
      <input type="checkbox" checked={account.fedi.public} disabled={publicSave.busy} onchange={(e) => setPublic(e.currentTarget)} />
      <span>{t("settings.fediPublic")}</span>
      <SaveStatus save={publicSave.state} />
    </label>
    {#if account.hasPassword}
      <div class="buttons">
        <button type="button" class="secondary" disabled={unlinkSave.busy} onclick={() => unlinkSave.run(account.fediUnlink)}>
          {busyLabel(unlinkSave.state, t("settings.fediUnlink"), t("settings.fediUnlinking"))}
        </button>
        <SaveStatus save={unlinkSave.state} done="" />
      </div>
    {:else}
      <p class="hint">{t("settings.fediOnlyLogin")}</p>
    {/if}
  </div>
{:else if enabled}
  <form class="account" onsubmit={link}>
    <span class="label">{t("settings.fediAccount")}</span>
    <p class="hint">{t("settings.fediLinkHint")}</p>
    <input bind:value={instance} placeholder={t("settings.fediPlaceholder")} autocomplete="url" aria-label={t("settings.fediAddressLabel")} required />
    {#if account.hasPassword && !account.temporary}
      <input type="password" bind:value={password} placeholder={t("settings.currentPassword")} autocomplete="current-password" required />
    {/if}
    <div class="buttons">
      <button type="submit" class="secondary" disabled={linkSave.busy}>{busyLabel(linkSave.state, t("settings.fediLink"), t("settings.fediOpening"))}</button>
      <SaveStatus save={linkSave.state} done="" />
    </div>
  </form>
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
  .account input:not([type="checkbox"]) {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
  }
  .label {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .handle {
    margin: 0;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .check {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    column-gap: 8px;
    text-transform: none;
    font-weight: normal;
  }
  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
  }
  .secondary {
    padding: 8px 14px;
    border-radius: 6px;
    background: var(--bg-input);
    font-weight: 600;
  }
  .hint {
    margin: 0;
    text-transform: none;
    font-weight: normal;
    font-size: 12px;
    color: var(--text-faint);
  }
</style>
