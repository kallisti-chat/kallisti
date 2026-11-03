<script lang="ts">
  import { untrack } from "svelte";
  import { expiryChoices, inviteExpiry, inviteUses, useChoices, type Invite } from "../lib/invites";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";

  let { on, invites, onSet, onLoad, onCreate, onDelete }: {
    on: boolean;
    invites: readonly Invite[] | null;
    onSet: (on: boolean) => Promise<void>;
    onLoad: () => Promise<void>;
    onCreate: (seconds: number, uses: number) => Promise<string>;
    onDelete: (id: number) => Promise<void>;
  } = $props();

  const policy = createSave();
  const links = createSave();
  let seconds = $state<number>(7 * 86400);
  let uses = $state<number>(1);
  let link = $state("");
  let copied = $state(false);
  $effect(() => { untrack(() => links.run(onLoad)); });

  const change = (el: HTMLInputElement) => policy.run(() => onSet(el.checked), () => (el.checked = on));
  const create = () => links.run(async () => {
    link = await onCreate(seconds, uses);
    copied = false;
  });
  const copy = () => links.run(async () => {
    if (!navigator.clipboard) throw new Error("Select and copy the link below.");
    await navigator.clipboard.writeText(link);
    copied = true;
  });
</script>

<label class="toggle">
  <input type="checkbox" checked={on} disabled={policy.busy} onchange={(e) => change(e.currentTarget)} />
  Invite-only server
</label>
<p>New accounts need an invitation. New guests cannot join. Existing accounts and registered Mumble users keep their access.</p>
<SaveStatus save={policy.state} />

<h3>Account invitations</h3>
<p>Share a link with a friend to let them create an account. These links do not grant access to private channels.</p>
<div class="choices">
  <label>Expires in
    <select bind:value={seconds} disabled={links.busy}>
      {#each expiryChoices as choice}<option value={choice.seconds}>{choice.label}</option>{/each}
    </select>
  </label>
  <label>Uses
    <select bind:value={uses} disabled={links.busy}>
      {#each useChoices as choice}<option value={choice.uses}>{choice.label}</option>{/each}
    </select>
  </label>
  <button type="button" disabled={links.busy} onclick={create}>Create invitation</button>
</div>
{#if link}
  <label>New account invitation
    <input readonly value={link} onfocus={(e) => e.currentTarget.select()} />
  </label>
  <button type="button" disabled={links.busy} onclick={copy}>{copied ? "Copied" : "Copy link"}</button>
  <p>Copy it now; the link is shown only here until you close this page or create another invitation.</p>
{/if}
<SaveStatus save={links.state} />
<button type="button" disabled={links.busy} onclick={() => links.run(onLoad)}>Refresh invitations</button>
{#if invites === null}
  <p>Loading invitations…</p>
{:else if invites.length === 0}
  <p>No account invitations.</p>
{:else}
  <ul aria-label="Account invitations">
    {#each invites as invite (invite.id)}
      <li>
        <span>By {invite.creator}: {inviteUses(invite)}, {inviteExpiry(invite.expires, Date.now())}</span>
        <button type="button" disabled={links.busy} onclick={() => links.run(async () => { await onDelete(invite.id); link = ""; })}>Revoke</button>
      </li>
    {/each}
  </ul>
{/if}

<style>
  p { color: var(--text-muted); }
  label { display: flex; flex-direction: column; gap: 6px; }
  .toggle { flex-direction: row; align-items: center; }
  .choices { display: flex; flex-wrap: wrap; align-items: end; gap: 12px; margin-bottom: 16px; }
  ul { list-style: none; padding: 0; }
  li { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-block: 12px; }
  input[readonly] { width: 100%; min-width: 0; }
</style>
