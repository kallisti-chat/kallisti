<script lang="ts">
  import type { Registered } from "@kallisti/protocol";
  import { untrack } from "svelte";
  import { memberCandidates, type Member } from "../lib/privateChannels";
  import { expiryChoices, inviteExpiry, inviteUses, useChoices, type Invite } from "../lib/invites";
  import { t } from "../lib/i18n.svelte";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  // The members of a private channel, for admins, moderators and the
  // channel's moderators: remove them, or add registered users (the server
  // checks again). Invite links apply at once (not on Save). moderators:
  // the channel's moderators instead (staff appoint them), no links.
  let {
    name,
    members,
    registered,
    invites = null,
    inherited = [],
    link = null,
    moderators = false,
    onLoad,
    onSubmit,
    onCreateInvite = async () => {},
    onDeleteInvite = async () => {},
    onClose,
  }: {
    name: string;
    members: readonly Member[];
    // Registered users and invite links (null: not loaded yet; onLoad asks
    // for them).
    registered: readonly Registered[] | null;
    invites?: readonly Invite[] | null;
    // Members by the channel's private category: shown, changed there.
    inherited?: readonly Member[];
    // A link just made: shown only now.
    link?: string | null;
    moderators?: boolean;
    onLoad: () => void;
    onSubmit: (members: Member[]) => Promise<void>;
    onCreateInvite?: (seconds: number, uses: number) => Promise<void>;
    onDeleteInvite?: (id: number) => Promise<void>;
    onClose: () => void;
  } = $props();
  const mods = untrack(() => moderators);

  let dialog: HTMLDialogElement | undefined = $state();
  // The list starts from the current members; Save sends it.
  let list = $state<Member[]>(untrack(() => [...members]));
  const addable = $derived(memberCandidates(registered, list));
  $effect(() => dialog?.showModal());
  $effect(() => untrack(onLoad));

  let seconds = $state<number>(expiryChoices[0].seconds);
  let uses = $state<number>(useChoices[0].uses);
  let copied = $state<string | null>(null); // the link copied
  const copy = (text: string) => void navigator.clipboard?.writeText(text).then(() => (copied = text));
  const now = Date.now();

  const add = (event: Event & { currentTarget: HTMLSelectElement }) => {
    const u = addable.find((a) => String(a.id) === event.currentTarget.value);
    if (u) list = [...list, u];
    event.currentTarget.value = "";
  };
  // Save closes once the server confirms; links show their own status.
  const save = createSave();
  const links = createSave();
  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    save.run(() => onSubmit(list).then(() => dialog?.close()));
  };
</script>

<dialog bind:this={dialog} onclose={onClose} aria-labelledby="members-dialog-title">
  <form onsubmit={submit}>
    <header>
      <h2 id="members-dialog-title">{t(moderators ? "admin.moderatorsOf" : "admin.membersOf", { name })}</h2>
      <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
    </header>
    <p class="note">
      {moderators ? t("admin.moderatorsNote") : t("admin.membersNote")}
    </p>
    {#if list.length}
      <ul aria-label={moderators ? t("admin.moderatorsList") : t("admin.membersList")}>
        {#each list as m (m.id)}
          <li>
            <span class="ellipsis">{m.name}</span>
            <button type="button" aria-label={t("admin.removeNamed", { name: m.name })} onclick={() => (list = list.filter((x) => x.id !== m.id))}>×</button>
          </li>
        {/each}
      </ul>
    {:else}
      <p>{mods ? t("admin.noModerators") : t("admin.noMembers")}</p>
    {/if}
    {#if inherited.length}
      <p class="note">{t("admin.inheritedMembers", { names: inherited.map((m) => m.name).join(", ") })}</p>
    {/if}
    {#if registered === null}
      <p class="note">{t("admin.loadingRegistered")}</p>
    {:else if addable.length}
      <select aria-label={mods ? t("admin.addModerator") : t("admin.addMember")} value="" onchange={add}>
        <option value="" disabled>{mods ? t("admin.addModeratorOption") : t("admin.addMemberOption")}</option>
        {#each addable as u (u.id)}<option value={String(u.id)}>{u.name}</option>{/each}
      </select>
    {:else}
      <p class="note">{mods ? t("admin.allModerators") : t("admin.allMembers")}</p>
    {/if}
    {#if !moderators}
      <h3>{t("admin.inviteLinks")}</h3>
      {#if link}
        <div class="row">
          <input readonly value={link} aria-label={t("admin.newInviteLink")} onfocus={(e) => e.currentTarget.select()} />
          <button type="button" class="action" onclick={() => copy(link)}>{copied === link ? t("common.copied") : t("common.copy")}</button>
        </div>
        <p class="note">{t("admin.copyNowOnce")}</p>
      {/if}
      <div class="row">
        <select aria-label={t("admin.validFor")} bind:value={seconds}>
          {#each expiryChoices as c (c.seconds)}<option value={c.seconds}>{c.label}</option>{/each}
        </select>
        <select aria-label={t("admin.uses")} bind:value={uses}>
          {#each useChoices as c (c.uses)}<option value={c.uses}>{c.label}</option>{/each}
        </select>
        <button type="button" class="action" disabled={links.busy} onclick={() => links.run(() => onCreateInvite(seconds, uses))}>
          {busyLabel(links.state, t("admin.createLink"), t("admin.creating"))}
        </button>
      </div>
      <SaveStatus save={links.state} />
      {#if invites === null}
        <p class="note">{t("admin.loadingInviteLinks")}</p>
      {:else if invites.length}
        <ul class="links" aria-label={t("admin.activeInviteLinks")}>
          {#each invites as i (i.id)}
            <li>
              <span class="ellipsis">{t("admin.inviteLinkLine", { uses: inviteUses(i), expiry: inviteExpiry(i.expires, now), creator: i.creator })}</span>
              <button
                type="button"
                aria-label={t("admin.deleteInviteLink", { creator: i.creator, uses: inviteUses(i) })}
                disabled={links.busy}
                onclick={() => links.run(() => onDeleteInvite(i.id))}
              >
                ×
              </button>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="note">{t("admin.noInviteLinks")}</p>
      {/if}
    {/if}
    <footer>
      <SaveStatus save={save.state} />
      <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
      <button type="submit" disabled={save.busy}>{busyLabel(save.state, t("common.save"), t("common.saving"))}</button>
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
    padding-bottom: 8px;
  }
  footer {
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
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
  }
  .note {
    color: var(--text-muted);
  }
  ul {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 0 0 12px;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: 4px;
    max-width: 100%;
    padding: 4px 4px 4px 10px;
    border-radius: 12px;
    background: var(--bg-rail);
    font-size: 14px;
  }
  li button {
    padding: 0 6px;
    color: var(--text-muted);
  }
  h3 {
    margin: 16px 0 8px;
    font-size: 14px;
  }
  .row {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
  }
  .row > * {
    min-width: 0;
  }
  .row input {
    flex: 1;
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  .action {
    flex: none;
    padding: 10px 12px;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
  }
  .links {
    flex-direction: column;
  }
  .links li {
    justify-content: space-between;
    border-radius: 4px;
  }
  select {
    width: 100%;
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  footer button {
    padding: 10px 16px;
    border-radius: 4px;
    background: var(--accent-strong);
    color: white;
    font-weight: 600;
  }
  footer .secondary {
    background: none;
    color: var(--text);
  }
</style>
