<script lang="ts">
  import { untrack } from "svelte";
  import { t } from "../lib/i18n.svelte";

  // A thread's name: for a new thread (from a message) or a rename.
  let {
    title,
    name,
    action,
    onSubmit,
    onClose,
  }: {
    title: string;
    name: string; // to start with
    action: string; // the submit button
    onSubmit: (name: string) => void;
    onClose: () => void;
  } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let value = $state(untrack(() => name));
  $effect(() => dialog?.showModal());
  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    if (!value.trim()) return;
    onSubmit(value.trim());
    dialog?.close();
  };
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="thread-name-title">
  <form onsubmit={submit}>
    <header>
      <h2 id="thread-name-title">{title}</h2>
      <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
    </header>
    <label>
      {t("chat.threadName")}
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value maxlength="100" required autofocus onfocus={(e) => e.currentTarget.select()} />
    </label>
    <footer>
      <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
      <button type="submit" class="primary" disabled={!value.trim()}>{action}</button>
    </footer>
  </form>
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
  header,
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  header {
    padding-bottom: 16px;
  }
  footer {
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
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
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0 0 16px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  input {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    text-transform: none;
  }
  footer button {
    padding: 10px 16px;
    border-radius: 4px;
    color: white;
    font-weight: 600;
  }
  footer .secondary {
    background: none;
    color: var(--text);
  }
  footer .primary {
    background: var(--accent-strong);
  }
  footer .primary:disabled {
    opacity: 0.5;
  }
</style>
