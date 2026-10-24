<script lang="ts">
  import { untrack } from "svelte";
  import { modeForError, type Account, type AccountsApi } from "../lib/accounts";
  import { solveInWorker } from "../lib/captcha";
  import { formatFingerprint, type CertificateProblem } from "../lib/desktop";
  import type { Credentials } from "../lib/connection.svelte";
  import { safeGet, safeSet } from "../lib/storage";

  let {
    error,
    api,
    account,
    onAccountChange,
    onConnect,
    fediSignup = false,
    onFediSignupDone,
    desktop = null,
    certificate = null,
    onTrust,
    onCancelTrust,
  }: {
    // Desktop: the server's certificate needs the user's decision.
    certificate?: (CertificateProblem & { server: string }) | null;
    onTrust?: () => void;
    onCancelTrust?: () => void;
    // The desktop app: first the server address (onServer finds out what it
    // is); a Kallisti server then works as on the web, a Mumble server with
    // a name and a password (see lib/desktop).
    desktop?: { server: string; kind: "kallisti" | "mumble" | null; onServer: (address: string) => Promise<void>; onChangeServer: () => void } | null;
    error: string | null;
    api: AccountsApi;
    // The logged-in account (null: none, or a server without accounts).
    account: Account | null;
    onAccountChange: (account: Account | null) => void;
    onConnect: (c: Credentials) => void;
    // Back from a fediverse login without an account here: choose a name
    // (see lib/fedi). onFediSignupDone: made, or given up.
    fediSignup?: boolean;
    onFediSignupDone?: () => void;
  } = $props();

  type Mode = "guest" | "login" | "create";
  const MODES: [Mode, string][] = [
    ["login", "Log in"],
    ["create", "Create account"],
    ["guest", "Guest"],
  ];
  // A guest who tried an account's name gets the Log in tab right away.
  let mode = $state<Mode>(untrack(() => modeForError(error)));
  let username = $state(safeGet(localStorage, "username") ?? "");
  let password = $state("");
  let serverPassword = $state("");
  let server = $state(untrack(() => desktop?.server) ?? "");
  // Desktop and a plain Mumble server: no accounts, one password field.
  const mumbleOnly = $derived(desktop?.kind === "mumble");
  const pickServer = async (event: SubmitEvent) => {
    event.preventDefault();
    if (!desktop || !server.trim()) return;
    busy = true;
    await desktop.onServer(server.trim());
    busy = false;
  };
  let busy = $state(false);
  let checking = $state(false);
  let apiError = $state<string | null>(null);

  // The server's browser check, where it has one: by itself, a second or
  // two (a failure is an error; submitting again retries).
  const prove = async () => {
    const challenge = await api.challenge();
    if (!challenge) return undefined;
    checking = true;
    try {
      return await solveInWorker(challenge);
    } finally {
      checking = false;
    }
  };

  const connectAs = (name: string, isAccount: boolean, pw = serverPassword) => {
    safeSet(localStorage, "username", name);
    onConnect({ username: name, password: pw, account: isAccount, ...(desktop && { server: desktop.server }) });
  };

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    const name = username.trim();
    if (!name) return;
    if (mumbleOnly) return connectAs(name, false, password);
    busy = true;
    apiError = null;
    try {
      // A guest gets a temporary account where the server has them (its
      // name is kept, and it can keep the account with a password).
      if (mode === "guest") {
        // A browser that cannot do the check joins as a classic guest (as
        // Mumble clients do).
        const proof = await prove().catch(() => null);
        const guest = proof === null ? null : await api.guest(name, proof);
        if (!guest) return connectAs(name, false);
        onAccountChange(guest);
        return connectAs(guest.name, true);
      }
      const result =
        mode === "login" ? await api.login(name, password) : { name: await api.create(name, password, await prove()), mumble: false };
      const pw = password;
      password = "";
      // A Mumble registration (SuperUser, for example) logs in to Mumble with
      // its password.
      if (result.mumble) return connectAs(result.name, false, pw);
      // With its password state and fediverse link, for the settings.
      onAccountChange((await api.me()) ?? { name: result.name, temporary: false });
      connectAs(result.name, true);
    } catch (err) {
      apiError = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  };

  // The server password field (guests only: accounts do not need it),
  // once the server says guests may need one (most have none: no field
  // that comes and goes).
  let askServerPassword = $state(false);
  $effect(() => {
    if (desktop?.kind !== "mumble") void api.serverPassword().then((ask) => (askServerPassword = ask));
  });

  // New accounts: the server's switch (closed: no Create account tab).
  let registration = $state(true);
  $effect(() => {
    if (desktop?.kind !== "mumble")
      void api.registration().then((on) => {
        registration = on;
        if (!on && mode === "create") mode = "login";
      });
  });
  const modes = $derived(registration ? MODES : MODES.filter(([value]) => value !== "create"));

  // Fediverse logins: the server's switch; the instance from last time.
  let fediOn = $state(false);
  $effect(() => {
    if (!desktop) void api.fediEnabled().then((on) => (fediOn = on));
  });
  let instance = $state(safeGet(localStorage, "fediInstance") ?? "");
  const fediLogin = async (event: SubmitEvent) => {
    event.preventDefault();
    if (!instance.trim()) return;
    busy = true;
    apiError = null;
    try {
      const url = await api.fediStart(instance.trim());
      safeSet(localStorage, "fediInstance", instance.trim());
      location.assign(url); // to the instance, and back to the server
    } catch (err) {
      apiError = err instanceof Error ? err.message : String(err);
      busy = false;
    }
  };
  // The name step: the server suggests the fediverse user name.
  let signup = $state<{ name: string; handle: string } | null | undefined>(undefined);
  $effect(() => {
    if (fediSignup && signup === undefined)
      void api.fediSignup().then((s) => {
        signup = s;
        if (s) username = s.name;
      });
  });
  const createFromFedi = async (event: SubmitEvent) => {
    event.preventDefault();
    const name = username.trim();
    if (!name || !signup) return;
    busy = true;
    apiError = null;
    try {
      const made = await api.fediCreate(name, await prove());
      onAccountChange({ name: made, temporary: false, password: false, fedi: { handle: signup.handle, public: false } });
      onFediSignupDone?.();
      connectAs(made, true);
    } catch (err) {
      apiError = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  };

  const logout = async () => {
    if (await api.logout()) onAccountChange(null);
    else apiError = "Logging out did not finish: you may still be logged in. Try again.";
  };
</script>

<div class="screen">
  {#if certificate}
    <div class="card">
      {#if certificate.kind === "unknown"}
        <h1>New server</h1>
        <p class="sub">
          First connection to <strong>{certificate.server}</strong>. Its certificate fingerprint is:
        </p>
      {:else}
        <h1>Certificate changed</h1>
        <p class="error" role="alert">
          The certificate of <strong>{certificate.server}</strong> is not the one you trusted before. This can mean
          that someone is listening in. Continue only if the server admin told you about a new certificate.
        </p>
      {/if}
      <code class="fingerprint">{formatFingerprint(certificate.fingerprint)}</code>
      <p class="hint">If you can, compare it with the server admin.</p>
      {#if certificate.kind === "unknown"}
        <button class="primary" onclick={onTrust}>Trust and connect</button>
        <button class="link" onclick={onCancelTrust}>Cancel</button>
      {:else}
        <!-- A changed certificate: the safe choice is the main button. -->
        <button class="primary" onclick={onCancelTrust}>Cancel</button>
        <button class="link danger" onclick={onTrust}>Trust the new certificate anyway</button>
      {/if}
    </div>
  {:else if account}
    <div class="card">
      <h1>{(apiError ?? error) ? "Could not connect" : "Welcome back"}</h1>
      {@render serverLine()}
      <p class="sub">
        {account.temporary ? "Your guest account" : "Logged in as"} <strong>{account.name}</strong>
      </p>
      {#if apiError ?? error}<p class="error" role="alert">{apiError ?? error}</p>{/if}
      <!-- Shown after a failed connection (a saved session connects by
           itself, see App): again by hand, not in a loop. -->
      <button class="primary" onclick={() => connectAs(account.name, true)}>Try again</button>
      <button class="link" onclick={logout}>Log out</button>
    </div>
  {:else if fediSignup}
    {#if signup}
      <form class="card" onsubmit={createFromFedi}>
        <h1>Choose your name</h1>
        <p class="sub">Welcome, <strong>{signup.handle}</strong>. Choose the name people see here.</p>
        <label>
          Name
          <input bind:value={username} required maxlength="32" autocomplete="username" />
        </label>
        {#if apiError}<p class="error" role="alert">{apiError}</p>{/if}
        {#if checking}<p class="hint" role="status">Checking your browser…</p>{/if}
        <button class="primary" type="submit" disabled={busy}>Create account</button>
        <button type="button" class="link" onclick={onFediSignupDone}>Cancel</button>
      </form>
    {:else}
      <div class="card">
        <h1>Log in with the fediverse</h1>
        <p class="sub" role="status">{signup === null ? "The fediverse login expired. Please try again." : "Loading…"}</p>
        {#if signup === null}<button class="primary" onclick={onFediSignupDone}>Back</button>{/if}
      </div>
    {/if}
  {:else}
    {#if desktop && !desktop.kind}
    <form class="card" onsubmit={pickServer}>
      <h1>Welcome to Kallisti</h1>
      <label>
        Server address
        <input bind:value={server} required placeholder="chat.example.org" autocomplete="url" />
      </label>
      <p class="hint">A Kallisti server, or any Mumble server (voice.example.org:64738).</p>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button class="primary" type="submit" disabled={busy}>{busy ? "Looking…" : "Continue"}</button>
    </form>
    {:else}
    <form class="card" onsubmit={submit}>
      <h1>Welcome to Kallisti</h1>
      {@render serverLine()}
      {#if !mumbleOnly}
      <div class="tabs" role="group" aria-label="How to join">
        {#each modes as [value, label] (value)}
          <button
            type="button"
            aria-pressed={mode === value}
            class:active={mode === value}
            onclick={() => {
              mode = value;
              apiError = null;
            }}>{label}</button
          >
        {/each}
      </div>
      {/if}
      <label>
        Name
        <input bind:value={username} required maxlength="32" autocomplete="username" />
      </label>
      {#if mumbleOnly}
        <label>
          Password <span class="optional">(if the server or your name needs one)</span>
          <input bind:value={password} type="password" autocomplete="current-password" />
        </label>
      {:else if mode !== "guest"}
        <label>
          Password
          <input
            bind:value={password}
            type="password"
            required
            minlength={mode === "create" ? 8 : undefined}
            autocomplete={mode === "create" ? "new-password" : "current-password"}
          />
        </label>
      {/if}
      {#if !mumbleOnly && mode === "guest" && askServerPassword}
      <label>
        Server password <span class="optional">(only if the server has one)</span>
        <input bind:value={serverPassword} type="password" autocomplete="off" />
      </label>
      {/if}
      {#if apiError ?? error}<p class="error" role="alert">{apiError ?? error}</p>{/if}
      {#if mode === "create"}
        <p class="hint">An account keeps your name: nobody else can use it.</p>
      {/if}
      {#if checking}<p class="hint" role="status">Checking your browser…</p>{/if}
      <button class="primary" type="submit" disabled={busy}>
        {mumbleOnly ? "Connect" : mode === "guest" ? "Join as guest" : mode === "login" ? "Log in" : "Create account"}
      </button>
    </form>
    {/if}
    {#if fediOn && !desktop}
      <form class="card fedi" onsubmit={fediLogin}>
        <label>
          Or use your fediverse account
          <input bind:value={instance} placeholder="mastodon.social or @you@mastodon.social" autocomplete="url" aria-describedby="fedi-hint" />
        </label>
        <p class="hint" id="fedi-hint">
          Mastodon, Pleroma, Akkoma, GoToSocial and others.{registration ? " New here? You choose a name next." : ""}
        </p>
        <button class="secondary" type="submit" disabled={busy || !instance.trim()}>Log in with the fediverse</button>
      </form>
    {/if}
  {/if}
</div>

<!-- Desktop: the chosen server, with a way back to the address step. -->
{#snippet serverLine()}
  {#if desktop}
    <p class="server">
      <span class="ellipsis">{desktop.server}{mumbleOnly ? " (Mumble server)" : ""}</span>
      <button type="button" class="link" onclick={desktop.onChangeServer}>Change</button>
    </p>
  {/if}
{/snippet}

<style>
  .server {
    display: flex;
    gap: 8px;
    align-items: center;
    justify-content: space-between;
    margin: 0 0 12px;
    color: var(--text-muted);
    font-size: 14px;
  }
  .screen {
    display: flex;
    flex-direction: column;
    gap: 16px;
    align-items: center;
    justify-content: safe center;
    overflow-y: auto;
    height: 100%;
    padding: var(--safe-top) var(--safe-right) var(--safe-bottom) var(--safe-left);
    background: var(--bg-rail);
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: min(400px, 90vw);
    padding: 32px;
    border-radius: 8px;
    background: var(--bg-main);
  }
  h1 {
    margin: 0;
    font-size: 24px;
    text-align: center;
  }
  .sub {
    margin: -8px 0 8px;
    color: var(--text-muted);
    text-align: center;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .optional {
    text-transform: none;
    font-weight: normal;
  }
  input {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    outline: none;
    /* Not the label's small caps; 16px also keeps phones from zooming in. */
    font-size: 16px;
    font-weight: normal;
    color: var(--text);
  }
  .primary {
    padding: 12px;
    border-radius: 4px;
    background: var(--accent-strong);
    color: white;
    font-weight: 600;
  }
  .secondary {
    padding: 10px;
    border-radius: 4px;
    background: var(--bg-rail);
    font-weight: 600;
  }
  .card.fedi {
    gap: 10px;
    padding: 20px 32px;
  }
  .link {
    color: var(--text-muted);
    font-size: 14px;
  }
  .link.danger {
    color: var(--danger);
  }
  .link:hover {
    color: var(--text);
    text-decoration: underline;
  }
  .tabs {
    display: flex;
    gap: 4px;
    padding: 4px;
    border-radius: 8px;
    background: var(--bg-rail);
  }
  .tabs button {
    flex: 1;
    padding: 8px;
    border-radius: 6px;
    color: var(--text-muted);
    font-size: 14px;
    font-weight: 600;
  }
  .tabs button.active {
    background: var(--bg-active);
    color: var(--text);
  }
  .fingerprint {
    padding: 10px;
    border-radius: 6px;
    background: var(--bg-rail);
    font-size: 13px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .hint {
    margin: 0;
    color: var(--text-muted);
    font-size: 13px;
  }
  .error {
    margin: 0;
    color: var(--danger);
  }
</style>
