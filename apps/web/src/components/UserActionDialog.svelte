<script lang="ts">
  import { untrack } from "svelte";
  import { t } from "../lib/i18n.svelte";
  import { BAN_TIMES, DELETE_TIMES, TIMEOUT_TIMES, type ConfirmKind, type ModerationAction } from "../lib/moderation";

  // Asks before a kick, ban, time-out or deleting messages: a reason (kick,
  // ban) and a time (ban, time-out; how far back to delete). timedBans:
  // bans can have a time (else for ever); deletes: offer deleting the
  // user's recent messages too.

  let {
    kind,
    name,
    timedBans = false,
    deletes = false,
    where,
    onSubmit,
    onClose,
  }: {
    kind: ConfirmKind;
    name: string;
    timedBans?: boolean;
    deletes?: boolean;
    // A time-out in this channel only.
    where?: string;
    onSubmit: (a: ModerationAction) => void;
    onClose: () => void;
  } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  const k = untrack(() => kind);
  const times = k === "timeout" ? TIMEOUT_TIMES : k === "delete" ? DELETE_TIMES : untrack(() => timedBans) ? BAN_TIMES : [];
  const alsoDelete = k !== "delete" && untrack(() => deletes);
  let reason = $state("");
  let seconds = $state<number>(times[0]?.seconds ?? 0);
  let deleteSeconds = $state(0);
  $effect(() => dialog?.showModal());

  const w = untrack(() => where);
  const title = $derived(
    k === "kick"
      ? t("admin.kickTitle", { name })
      : k === "ban"
        ? t("admin.banTitle", { name })
        : k === "timeout"
          ? w ? t("admin.timeoutInTitle", { name, where: w }) : t("admin.timeoutTitle", { name })
          : t("admin.deleteMessagesTitle", { name }),
  );
  const BUTTONS = { kick: "admin.kickButton", ban: "admin.banButton", timeout: "admin.timeoutButton", delete: "admin.deleteMessagesButton" } as const;
  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    const r = reason.trim();
    const also = deleteSeconds ? { deleteSeconds } : {};
    onSubmit(
      k === "delete"
        ? { kind: k, seconds }
        : k === "kick"
          ? { kind: k, reason: r, ...also }
          : k === "ban"
            ? { kind: k, reason: r, seconds, ...also }
            : { kind: k, seconds, ...also },
    );
    dialog?.close();
  };
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="user-action-title">
  <form onsubmit={submit}>
    <header>
      <h2 id="user-action-title">{title}</h2>
      <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
    </header>
    <p>
      {k === "kick"
        ? t("admin.kickNote", { name })
        : k === "ban"
          ? t(times.length ? "admin.banTimedNote" : "admin.banNote", { name })
          : k === "timeout"
            ? w ? t("admin.timeoutInNote", { name, where: w }) : t("admin.timeoutNote", { name })
            : t("admin.deleteMessagesNote", { name })}
    </p>
    {#if times.length}
      <label>
        {k === "ban" ? t("admin.banFor") : k === "timeout" ? t("admin.timeoutFor") : t("admin.deleteFrom")}
        <select bind:value={seconds}>
          {#each times as c (c.seconds)}<option value={c.seconds}>{t(c.label)}</option>{/each}
        </select>
      </label>
    {/if}
    {#if alsoDelete}
      <label>
        {t("admin.alsoDelete")}
        <select bind:value={deleteSeconds}>
          <option value={0}>{t("admin.deleteNone")}</option>
          {#each DELETE_TIMES as c (c.seconds)}<option value={c.seconds}>{t(c.label)}</option>{/each}
        </select>
      </label>
    {/if}
    {#if k === "kick" || k === "ban"}
      <label>
        {t("admin.reasonOptional")}
        <!-- svelte-ignore a11y_autofocus -->
        <input bind:value={reason} maxlength="200" autofocus />
      </label>
    {/if}
    <footer>
      <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
      <button type="submit" class="danger">{t(BUTTONS[k])}</button>
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
  p {
    margin: 0 0 16px;
    font-size: 14px;
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
  input,
  select {
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
  footer .danger {
    background: var(--danger);
  }
</style>
