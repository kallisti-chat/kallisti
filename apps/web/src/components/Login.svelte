<script lang="ts">
  import { untrack } from "svelte";
  import QrShowDialog from "./QrShowDialog.svelte";
  import { thisDevice } from "../lib/qrLogin";
  import { registrationToken } from "../lib/registration";
  import { parseInviteLink } from "../lib/invites";
  import UserName from "./UserName.svelte";
  import { serverOrigin } from "../lib/serverList";
  import { modeForError, type Account, type AccountsApi } from "../lib/accounts";
  import { solveInWorker } from "../lib/captcha";
  import { formatFingerprint, isDesktop, serverHint, type CertificateProblem } from "../lib/desktop";
  import type { Credentials } from "../lib/connection.svelte";
  import { iconHref, showIcons } from "../lib/serverLogo";
  import { localStore, safeGet, safeSet, tabStorage } from "../lib/storage";

  let {
    error,
    api,
    account,
    onAccountChange,
    onConnect,
    onScan,
    fediSignup = false,
    onFediSignupDone,
    onHandoff,
    keep,
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
    // bridge: plain Mumble servers too (not in the iOS app; null: not known yet).
    desktop?: { server: string; kind: "kallisti" | "mumble" | null; bridge: boolean | null; onServer: (address: string) => Promise<void>; onChangeServer: () => void } | null;
    error: string | null;
    api: AccountsApi;
    // The logged-in account (null: none, or a server without accounts).
    account: Account | null;
    onAccountChange: (account: Account | null) => void;
    onConnect: (c: Credentials) => void;
    // The phone apps: scan a login code (see lib/qrScan).
    onScan?: () => void;
    // Back from a fediverse login without an account here: choose a name
    // (see lib/fedi). onFediSignupDone: made, or given up.
    fediSignup?: boolean;
    onFediSignupDone?: () => void;
    // A handoff's account was made (see Connect): its token waits.
    onHandoff?: () => void;
    // An address to come back to after a login through another server.
    keep?: string;
  } = $props();

  // An answer counts only for the server it was asked of, while this
  // screen is open and the desktop app's server is the same ("Change"
  // cancels it).
  let open = true;
  $effect(() => () => void (open = false));
  const shown = () => `${desktop?.server ?? ""} ${desktop?.kind ?? ""}`;
  const current = (used: AccountsApi, at: string) => open && used === api && shown() === at;

  type Mode = "guest" | "login" | "create";
  const MODES: [Mode, string][] = [
    ["login", "Log in"],
    ["create", "Create account"],
    ["guest", "Guest"],
  ];
  // An account invitation opens signup; a guest who tried an account's
  // name gets the Log in tab right away.
  // From the address, or kept over a fediverse login (see fediLogin), once.
  const keptInvite = () => {
    const kept = safeGet(tabStorage(), "kallisti-account-invite") ?? "";
    if (kept) safeSet(tabStorage(), "kallisti-account-invite", "");
    return kept;
  };
  let accountInvite = $state(untrack(() => {
    if (desktop) return "";
    const origin = api.base || location.origin;
    return registrationToken(location.href, origin) ?? registrationToken(keptInvite(), origin) ?? "";
  }));
  let inviteOnly = $state(false);
  const inviteToken = () => {
    if (!accountInvite.trim()) return undefined;
    const token = registrationToken(accountInvite.trim(), api.base || location.origin);
    if (!token) throw new Error("Enter an account invitation for this server.");
    return token;
  };
  const clearInvite = () => {
    accountInvite = "";
    if (location.pathname === "/register") history.replaceState(history.state, "", "/");
  };
  let mode = $state<Mode>(untrack(() => accountInvite ? "create" : modeForError(error)));
  let username = $state(safeGet(localStore(), "username") ?? "");
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

  // Log in with a QR code (see lib/qrLogin): the phone confirmed.
  let qrShown = $state(false);
  const qrDone = async (name: string) => {
    qrShown = false;
    const used = api;
    const account = (await used.me()) ?? { name, temporary: false };
    if (used !== api) return; // another server chosen meanwhile
    onAccountChange(account);
    connectAs(account.name, true);
  };

  const connectAs = (name: string, isAccount: boolean, pw = serverPassword) => {
    safeSet(localStore(), "username", name);
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
      const used = api;
      const at = shown();
      if (mode === "guest") {
        // A browser that cannot do the check joins as a classic guest (as
        // Mumble clients do).
        const proof = await prove().catch(() => null);
        const guest = proof === null ? null : await used.guest(name, proof);
        if (!current(used, at)) return;
        if (!guest) return connectAs(name, false);
        onAccountChange(guest);
        return connectAs(guest.name, true);
      }
      const result =
        mode === "login" ? await used.login(name, password) : { name: await used.create(name, password, await prove(), inviteToken()), mumble: false };
      if (!current(used, at)) return;
      if (mode === "create") clearInvite();
      const pw = password;
      password = "";
      // A Mumble registration (SuperUser, for example) logs in to Mumble with
      // its password.
      if (result.mumble) return connectAs(result.name, false, pw);
      // With its password state and fediverse link, for the settings.
      const account = (await used.me()) ?? { name: result.name, temporary: false };
      if (!current(used, at)) return;
      onAccountChange(account);
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
  // The server's name (admins set it), also as the tab's title.
  let serverName = $state<string | null>(null);
  let logo = $state<string | null>(null);
  $effect(() => {
    if (desktop?.kind === "mumble") return;
    const used = api;
    const at = shown();
    void used.serverInfo().then((info) => {
      if (!current(used, at)) return;
      inviteOnly = info.inviteOnly === true;
      if (inviteOnly && mode === "guest") mode = "login";
      askServerPassword = info.password;
      serverName = info.name;
      logo = info.logo && iconHref(api.base, 192, info.logo);
      showIcons(document, api.base, info.logo);
      document.title = info.name ?? "Kallisti";
    });
  });

  // Fediverse logins: the server's switch; the instance from last time.
  let fediOn = $state(false);
  $effect(() => {
    if (!desktop) void api.fediEnabled().then((on) => (fediOn = on));
  });
  let instance = $state(safeGet(localStore(), "fediInstance") ?? "");
  const fediLogin = async (event: SubmitEvent) => {
    event.preventDefault();
    if (!instance.trim()) return;
    busy = true;
    apiError = null;
    try {
      const token = inviteToken();
      const url = await api.fediStart(instance.trim());
      safeSet(localStore(), "fediInstance", instance.trim());
      // A link to come back to (an invite here, a ?join): kept over the
      // login's pages (see App keptInvite); none: none. An account
      // invitation beside it, for the signup after the login.
      safeSet(tabStorage(), "kallisti-invite", keep ?? (invite ? location.pathname : ""));
      safeSet(tabStorage(), "kallisti-account-invite", token ?? "");
      location.assign(url); // to the instance, and back to the server
    } catch (err) {
      apiError = err instanceof Error ? err.message : String(err);
      busy = false;
    }
  };
  // An invite to this server (the address /invite/...): people with an
  // account on another Kallisti server open it there, at their home (its
  // address remembered by this site).
  const invite = !isDesktop() && !!parseInviteLink(location.href, location.origin);
  let homeAddress = $state(safeGet(localStore(), "kallisti-home") ?? "");
  let homeError = $state<string | null>(null);
  const openAtHome = (event: SubmitEvent) => {
    event.preventDefault();
    const parsed = serverOrigin(homeAddress, "", false);
    if ("error" in parsed) return void (homeError = parsed.error);
    if (parsed.origin === location.origin) return void (homeError = "That is this server.");
    safeSet(localStore(), "kallisti-home", homeAddress.trim());
    location.assign(`${parsed.origin}/?join=${encodeURIComponent(location.href)}`);
  };
  // A new account from another server: named as at home (name@home), made
  // at once; only a taken name asks for one (the server suggests it).
  let signup = $state<{ name: string; handle: string; host: string; remote: string } | null | undefined>(undefined);
  let chooseName = $state(false);
  $effect(() => {
    if (fediSignup && signup === undefined)
      void api.fediSignup().then((s) => {
        signup = s;
        if (s) username = s.name;
        if (s) void createFromFedi();
      });
  });
  const createFromFedi = async (event?: SubmitEvent) => {
    event?.preventDefault();
    const name = chooseName ? username.trim() : "";
    if ((chooseName && !name) || !signup) return;
    busy = true;
    apiError = null;
    try {
      const { name: made, handoff } = await api.fediCreate(name, await prove(), inviteToken());
      clearInvite();
      if (handoff) return void onHandoff?.();
      onAccountChange({ name: made, temporary: false, password: false, fedi: { handle: signup.handle, public: false, host: signup.host } });
      onFediSignupDone?.();
      connectAs(made, true);
    } catch (err) {
      apiError = err instanceof Error ? err.message : String(err);
      // Its remote name is taken here: the name step (other errors show).
      if (!chooseName && err instanceof Error && "status" in err && err.status === 409) {
        chooseName = true;
        apiError = null;
      }
    } finally {
      busy = false;
    }
  };

  const logout = async () => {
    const used = api;
    const at = shown();
    const ok = await used.logout({ session: account?.session, name: account?.name });
    if (!current(used, at)) return;
    if (ok === "other") {
      // Another login is here now (another tab): show it, or none.
      const answer = await used.check();
      if (!current(used, at)) return;
      if (answer === "unknown") apiError = "Another login is active in this browser now. Reload the page.";
      else onAccountChange(answer);
    } else if (ok) onAccountChange(null);
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
    {#if signup && !chooseName}
      <div class="card">
        <h1>Welcome</h1>
        <p class="sub" role="status">Making your account <strong><UserName name={signup.remote || signup.handle} /></strong>…</p>
        {#if checking}<p class="hint" role="status">Checking your browser…</p>{/if}
        {#if apiError}
          <p class="error" role="alert">{apiError}</p>
          {@render inviteField(false)}
          <button class="primary" disabled={busy} onclick={() => void createFromFedi()}>Try again</button>
          <button type="button" class="link" onclick={onFediSignupDone}>Cancel</button>
        {/if}
      </div>
    {:else if signup}
      <form class="card" onsubmit={createFromFedi}>
        <h1>Choose your name</h1>
        <p class="sub">Welcome, <strong>{signup.handle}</strong>. {signup.remote} is taken here: choose the name people see here.</p>
        <label>
          Name
          <input bind:value={username} required maxlength="32" autocomplete="username" />
        </label>
        {#if inviteOnly || accountInvite}{@render inviteField(inviteOnly)}{/if}
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
      {#if desktop.bridge !== null}<p class="hint">{serverHint(desktop.bridge)}</p>{/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button class="primary" type="submit" disabled={busy}>{busy ? "Looking…" : "Continue"}</button>
      {#if onScan}
        <p class="hint">Or scan a login code from a device where you are logged in (its Settings): the code knows the server.</p>
        <button class="secondary" type="button" onclick={onScan}>Scan a QR code</button>
      {/if}
    </form>
    {:else}
    <form class="card" onsubmit={submit}>
      {#if logo}<img class="logo" src={logo} alt="" />{/if}
      <h1>Welcome to {serverName ?? "Kallisti"}</h1>
      {@render serverLine()}
      {#if !mumbleOnly}
      <div class="tabs" role="group" aria-label="How to join">
        {#each MODES.filter(([value]) => !inviteOnly || value !== "guest") as [value, label] (value)}
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
      {#if inviteOnly}<p class="hint">This server is invite-only. Existing users can log in; new accounts need an invitation from an admin.</p>{/if}
      <label>
        Name
        <input bind:value={username} required maxlength={mode === "login" ? 128 : 32} autocomplete="username" />
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
      {#if mode === "create" && (inviteOnly || accountInvite)}{@render inviteField(inviteOnly)}{/if}
      {#if mode === "create"}
        <p class="hint">An account keeps your name: nobody else can use it.</p>
      {/if}
      {#if checking}<p class="hint" role="status">Checking your browser…</p>{/if}
      <button class="primary" type="submit" disabled={busy}>
        {mumbleOnly ? "Connect" : mode === "guest" ? "Join as guest" : mode === "login" ? "Log in" : "Create account"}
      </button>
    </form>
    {#if !mumbleOnly}
      <div class="card fedi">
        <p class="hint">Logged in on another device already? No password needed.</p>
        {#if onScan}
          <button class="secondary" type="button" onclick={onScan}>Scan a QR code</button>
        {:else}
          <button class="secondary" type="button" onclick={() => (qrShown = true)}>Log in with a QR code</button>
        {/if}
      </div>
    {/if}
    {/if}
    {#if invite}
      <form class="card fedi" onsubmit={openAtHome}>
        <label>
          Have an account on another Kallisti server?
          <input bind:value={homeAddress} placeholder="chat.example.org" autocomplete="url" aria-describedby="home-hint" oninput={() => (homeError = null)} />
        </label>
        <p class="hint" id="home-hint">Open this invite there: this server joins your server list.</p>
        {#if homeError}<p class="error" role="alert">{homeError}</p>{/if}
        <button class="secondary" type="submit" disabled={!homeAddress.trim()}>Open in my Kallisti</button>
      </form>
    {/if}
    {#if fediOn && !desktop}
      <form class="card fedi" onsubmit={fediLogin}>
        <label>
          Or use your account on another server
          <input bind:value={instance} placeholder="chat.example.org or @you@mastodon.social" autocomplete="url" aria-describedby="fedi-hint" />
        </label>
        <p class="hint" id="fedi-hint">Another Kallisti server, or the fediverse: Mastodon, Pleroma, Akkoma, GoToSocial and others. New here? You choose a name next.</p>
        <button class="secondary" type="submit" disabled={busy || !instance.trim()}>Continue with that account</button>
      </form>
    {/if}
  {/if}
</div>

{#if qrShown}
  <QrShowDialog
    {api}
    origin={api.base || location.origin}
    device={thisDevice()}
    onLoggedIn={qrDone}
    onClose={() => (qrShown = false)}
  />
{/if}

{#snippet inviteField(required: boolean)}
  <label>Account invitation
    <input bind:value={accountInvite} {required} autocomplete="off" spellcheck={false} placeholder="Paste your invitation link" />
  </label>
{/snippet}

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
    width: min(400px, 90%);
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
  .logo {
    align-self: center;
    width: 64px;
    height: 64px;
    border-radius: 16px;
    object-fit: cover;
  }
</style>
