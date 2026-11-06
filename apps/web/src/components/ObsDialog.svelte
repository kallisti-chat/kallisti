<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import { around } from "../lib/i18n";
  import type { AccountActions, StreamKey } from "../lib/accounts";
  import type { QualityLimits } from "../lib/textChannels";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  // Streaming from OBS (WHIP): the steps, the address and the stream key. A
  // new key is shown once (the server keeps only its hash). live: our
  // stream from OBS runs.
  let {
    account,
    live = false,
    qualityLimits = null,
    onClose,
  }: { account: AccountActions; live?: boolean; qualityLimits?: QualityLimits | null; onClose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let key = $state<StreamKey | null>(null);
  let newKey = $state<string | null>(null);
  let note = $state<string | null>(null);
  let copied = $state<string | null>(null);
  const run = (fn: () => Promise<void>) => () =>
    fn().then(
      () => (note = null),
      (err) => (note = err instanceof Error ? err.message : String(err)),
    );
  $effect(() => dialog?.showModal());
  $effect(() => {
    void run(async () => {
      key = await account.streamKey();
    })();
  });
  // The key changes: pending, then the new key (or "Removed"), or the error.
  const making = createSave();
  const removing = createSave();
  const busy = $derived(making.busy || removing.busy);
  const makeKey = () =>
    making.run(async () => {
      newKey = await account.newStreamKey();
      key = { enabled: true, exists: true };
    });
  const removeKey = () =>
    removing.run(async () => {
      await account.removeStreamKey();
      key = { enabled: true, exists: false };
      newKey = null;
    });
  const steps = $derived(around(t("settings.obsSteps"), "url"));
  const bFrames = $derived(around(t("settings.obsBFrames"), "option"));
  const newKeyText = $derived(around(t("settings.obsNewKey"), "key"));
  const copy = (text: string) => void navigator.clipboard?.writeText(text).then(() => (copied = text));
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="obs-title">
  <header>
    <h2 id="obs-title">{t("settings.obsTitle")}</h2>
    <button class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  {#if live}<p class="live" role="status">{t("settings.obsLive")}</p>{/if}
  <p>
    {steps[0]}<code>{account.whipUrl}</code>
    <button class="link" onclick={() => copy(account.whipUrl)}>{copied === account.whipUrl ? t("common.copied") : t("common.copy")}</button>{steps[1]}
  </p>
  <p>{t("settings.obsOutput")}</p>
  {#if qualityLimits}
    <p>
      Server limits: up to {qualityLimits.maxSide}p, {qualityLimits.maxFps} fps, {((qualityLimits.bytesPerSecond * 8) / 1024 / 1024).toFixed(1)} Mbit/s.
    </p>
  {/if}
  <p>{bFrames[0]}<code>bframes=0</code>{bFrames[1]}</p>
  {#if newKey}
    <p class="key">
      {newKeyText[0]}<code>{newKey}</code>
      <button class="link" onclick={() => copy(newKey!)}>{copied === newKey ? t("common.copied") : t("common.copy")}</button>{newKeyText[1]}
    </p>
  {/if}
  {#if note}<p class="warn" role="status">{note}</p>{/if}
  {#if key}
    <div class="buttons">
      <button class="secondary" disabled={busy} onclick={makeKey}>
        {busyLabel(making.state, key.exists ? t("settings.obsMakeNewKey") : t("settings.obsMakeKey"), t("settings.obsMaking"))}
      </button>
      {#if key.exists}
        <button class="secondary danger" disabled={busy} onclick={removeKey}>{busyLabel(removing.state, t("settings.obsRemoveKey"), t("settings.removing"))}</button>
      {/if}
    </div>
    <SaveStatus save={making.state} done="" />
    <SaveStatus save={removing.state} done={t("settings.obsRemoved")} />
    {#if key.exists}<p>{t("settings.obsKeyHint")}</p>{/if}
  {/if}
</dialog>

<style>
  dialog {
    width: min(440px, 92vw);
    padding: 24px;
    border: none;
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-bottom: 16px;
  }
  h2 {
    margin: 0;
    font-size: 18px;
  }
  .close {
    font-size: 24px;
    line-height: 1;
    color: var(--text-muted);
  }
  p {
    margin: 0 0 12px;
    font-size: 14px;
    color: var(--text-muted);
  }
  .live {
    color: var(--accent);
    font-weight: 600;
  }
  .key {
    color: var(--text);
  }
  .warn {
    color: var(--danger);
  }
  code {
    word-break: break-all;
    user-select: all;
  }
  .link {
    padding: 0;
    color: var(--link);
  }
  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 12px;
  }
  .secondary {
    padding: 8px 14px;
    border-radius: 6px;
    background: var(--bg-input);
    font-weight: 600;
  }
  .secondary:hover {
    background: var(--bg-hover);
  }
  .danger {
    color: var(--danger);
  }
</style>
