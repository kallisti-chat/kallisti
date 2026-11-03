<script lang="ts">
  import type { AccountActions, StreamKey } from "../lib/accounts";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  // Streaming from OBS (WHIP): the steps, the address and the stream key. A
  // new key is shown once (the server keeps only its hash). live: our
  // stream from OBS runs.
  let { account, live = false, onClose }: { account: AccountActions; live?: boolean; onClose: () => void } = $props();

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
  const copy = (text: string) => void navigator.clipboard?.writeText(text).then(() => (copied = text));
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="obs-title">
  <header>
    <h2 id="obs-title">Stream from OBS</h2>
    <button class="close" onclick={() => dialog?.close()} aria-label="Close">×</button>
  </header>
  {#if live}<p class="live" role="status">Your stream from OBS is live.</p>{/if}
  <p>
    OBS 30 or newer can stream into your voice channel: it shows like a screen share. Be connected here and in a voice
    channel where you may speak. In OBS, Settings → Stream: Service "WHIP", Server
    <code>{account.whipUrl}</code>
    <button class="link" onclick={() => copy(account.whipUrl)}>{copied === account.whipUrl ? "Copied" : "Copy"}</button>,
    Bearer Token: your stream key.
  </p>
  <p>
    Settings → Output (Advanced): H.264 video, keyframe interval 1 or 2 s, profile baseline or main, 1080p and 8 Mbit/s
    at most.
  </p>
  <p>
    No B-frames (we forward the video as it is; with them the picture breaks up or turns pink): with x264, type
    <code>bframes=0</code> in "x264 Options"; with NVENC, QuickSync or AMF, set B-Frames to 0.
  </p>
  {#if newKey}
    <p class="key">
      Your stream key (shown only now): <code>{newKey}</code>
      <button class="link" onclick={() => copy(newKey!)}>{copied === newKey ? "Copied" : "Copy"}</button>
    </p>
  {/if}
  {#if note}<p class="warn" role="status">{note}</p>{/if}
  {#if key}
    <div class="buttons">
      <button class="secondary" disabled={busy} onclick={makeKey}>
        {busyLabel(making.state, key.exists ? "Make a new key" : "Make a stream key", "Making…")}
      </button>
      {#if key.exists}
        <button class="secondary danger" disabled={busy} onclick={removeKey}>{busyLabel(removing.state, "Remove the key", "Removing…")}</button>
      {/if}
    </div>
    <SaveStatus save={making.state} done="" />
    <SaveStatus save={removing.state} done="Removed" />
    {#if key.exists}<p>A new key, removing it, a new password or "log out everywhere" stop the old key (and a stream that uses it).</p>{/if}
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
