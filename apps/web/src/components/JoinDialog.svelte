<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import { around } from "../lib/i18n";

  // Before a link (or an invite) joins a voice channel: asked first, with
  // the server it goes to (a link on one server may lead to another), and
  // told when it ends a call here or takes voice from another device.
  let {
    name,
    server,
    call,
    onJoin,
    onClose,
  }: { name: string; server: string; call: "none" | "here" | "device"; onJoin: () => void; onClose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => dialog?.showModal());
  const join = () => {
    onJoin();
    dialog?.close();
  };
  const [before, after] = $derived(around(t("login.joinChannelText"), "server"));
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="join-dialog-title">
  <h2 id="join-dialog-title">{t("login.joinChannelTitle", { name })}</h2>
  <p>{before}<strong>{server}</strong>{after}</p>
  {#if call === "here"}<p class="note">{t("login.joinEndsCall")}</p>{/if}
  {#if call === "device"}<p class="note">{t("login.joinMovesVoice")}</p>{/if}
  <footer>
    <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
    <!-- svelte-ignore a11y_autofocus -->
    <button type="button" class="primary" onclick={join} autofocus>{t("login.join")}</button>
  </footer>
</dialog>

<style>
  dialog {
    width: min(400px, 92vw);
    padding: 24px;
    border: none;
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  h2 {
    margin: 0 0 12px;
    font-size: 18px;
    overflow-wrap: anywhere;
  }
  p {
    margin: 0 0 12px;
    font-size: 14px;
  }
  .note {
    color: var(--text-muted);
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }
  footer button {
    padding: 8px 16px;
    border-radius: 6px;
    font-weight: 600;
  }
  .secondary {
    background: var(--bg-input);
  }
  .primary {
    background: var(--accent-strong);
    color: white;
  }
</style>
