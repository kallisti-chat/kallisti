<script lang="ts">
  import { untrack } from "svelte";
  import type { Account, AccountsApi } from "../lib/accounts";
  import { isKallisti } from "../lib/desktop";
  import { connectUrl, type connectWays } from "../lib/popupLogin";
  import { serverOrigin } from "../lib/serverList";
  import { iconHref } from "../lib/serverLogo";
  import { initials } from "../lib/avatar";
  import { t } from "../lib/i18n.svelte";
  import { around } from "../lib/i18n";

  // Another Kallisti server for the rail (several servers in one client):
  // its address, then a login there. New accounts and guests are made on
  // that server's own site (it refuses other sites). The login's token
  // stays here until onAdded takes it (a cancelled or late login is logged
  // out again).
  let {
    home,
    homeCookie = true,
    ways = { home: true, fedi: null },
    taken,
    server,
    address: startAddress = "",
    apiFor,
    onAdded,
    onClose,
  }: {
    home: string;
    // The home login is a cookie (the web app): see serverOrigin.
    homeCookie?: boolean;
    // How the home account can log in there without typing (see
    // lib/popupLogin's connectWays).
    ways?: ReturnType<typeof connectWays>;
    // Origins already in the rail.
    taken: readonly string[];
    // A server of the list without a login on this device: straight to its
    // login, with the name it had.
    server?: { origin: string; name: string };
    // An address to check at once (an invite link's server).
    address?: string;
    // A server's account API whose token stays in memory (see token()),
    // starting with this one.
    apiFor: (origin: string, token?: string) => { api: AccountsApi; token: () => string | null };
    // False: not taken (no longer logged in at home, or added meanwhile).
    onAdded: (origin: string, account: Account, token: string) => boolean;
    onClose: () => void;
  } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  // svelte-ignore state_referenced_locally
  const preset = server;
  // svelte-ignore state_referenced_locally
  let address = $state(startAddress);
  let found = $state<{ origin: string } | null>(preset ? { origin: preset.origin } : null);
  // The server's name and logo (its public /api/server), once found.
  let info = $state<{ name: string | null; logo: string | null } | null>(null);
  $effect(() => {
    const origin = found?.origin;
    info = null;
    if (origin)
      void untrack(() => apiFor(origin))
        .api.serverInfo()
        .then((i) => found?.origin === origin && (info = i));
  });
  let name = $state(preset?.name ?? "");
  // The name and password fields: hidden until asked for (no home account
  // to use in the desktop app: shown).
  // svelte-ignore state_referenced_locally
  let withPassword = $state(!homeCookie);
  let password = $state("");
  let error = $state<string | null>(null);
  let busy = $state(false);
  // Each check and login gets a number: Back, Close and a new try make
  // older ones count for nothing, and log out a login they got (each login
  // has its own token: a late one cannot touch a newer one).
  let attempt = 0;
  let holding: { logout: () => void } | null = null;
  const cancel = () => {
    attempt++;
    holding?.logout();
    holding = null;
  };
  $effect(() => dialog?.showModal());
  // Once, after the setup (check is below).
  $effect(() => {
    if (startAddress && !preset) untrack(() => void check());
  });
  $effect(() => cancel);

  const back = () => {
    cancel();
    found = null;
    name = password = "";
    withPassword = !homeCookie;
    error = null;
    busy = false;
  };

  const check = async (event?: SubmitEvent) => {
    event?.preventDefault();
    error = null;
    const parsed = serverOrigin(address, home, homeCookie);
    if ("error" in parsed) return void (error = parsed.error);
    const { origin } = parsed;
    if (taken.includes(origin)) return void (error = t("login.serverInList"));
    const mine = ++attempt;
    busy = true;
    const kind = await isKallisti(origin);
    if (mine !== attempt) return;
    busy = false;
    if (kind === "unreachable") error = t("login.cannotReach", { origin });
    else if (kind === "other") error = t("login.notKallisti", { origin });
    else found = { origin };
  };

  // With the home account, no typing (web only): the server's page in a
  // popup logs in through the home server (or with the linked fediverse
  // account: via, its server) and posts the token back (see Connect
  // there). Only that popup, from that server's origin, counts.
  const viaHome = (via?: string) => {
    if (!found) return;
    const origin = found.origin;
    cancel();
    const mine = attempt;
    error = null;
    const popup = window.open(connectUrl(origin, home, via), "kallisti-connect", "popup,width=480,height=720");
    if (!popup) return void (error = t("login.popupBlocked"));
    busy = true;
    let result = false;
    const stop = () => {
      window.removeEventListener("message", onMessage);
      clearInterval(watch);
      if (mine === attempt) busy = false;
    };
    const onMessage = (e: MessageEvent) => {
      const d = e.data as { type?: unknown; token?: unknown; name?: unknown } | null;
      if (e.origin !== origin || e.source !== popup || d?.type !== "kallisti-login" || typeof d.token !== "string" || typeof d.name !== "string") return;
      result = true;
      stop();
      if (!onAdded(origin, { name: d.name, temporary: false }, d.token)) {
        void apiFor(origin, d.token).api.logout();
        error = t("login.serverNotAdded");
        return;
      }
      dialog?.close();
    };
    const watch = setInterval(() => {
      if (!popup.closed) return;
      stop();
      if (!result && mine === attempt) error = t("login.popupClosed");
    }, 500);
    window.addEventListener("message", onMessage);
    holding = { logout: () => (stop(), popup.close()) };
  };

  const logIn = async (event: SubmitEvent) => {
    event.preventDefault();
    if (!found) return;
    const origin = found.origin;
    cancel();
    const mine = attempt;
    const { api, token } = apiFor(origin);
    // Until it is taken, this login's token is ours to log out.
    const mineHolds = { logout: () => void (token() && api.logout()) };
    holding = mineHolds;
    const drop = () => {
      if (holding === mineHolds) holding = null;
      mineHolds.logout();
    };
    const cancelled = () => mine !== attempt;
    error = null;
    busy = true;
    try {
      const result = await api.login(name.trim(), password);
      if (cancelled()) return drop();
      // A Mumble registration has no session for other sites (the server
      // refuses it there; an older one may not).
      if (result.mumble) throw new Error(t("login.mumbleOwnSite"));
      const account = await api.me();
      if (cancelled()) return drop();
      const tok = token();
      if (!account || !tok) throw new Error(t("login.loginDidNotLast"));
      holding = null;
      if (!onAdded(origin, account, tok)) {
        mineHolds.logout();
        throw new Error(t("login.serverNotAdded"));
      }
      dialog?.close();
    } catch (err) {
      if (holding === mineHolds) drop();
      if (!cancelled()) error = err instanceof Error ? err.message : String(err);
    } finally {
      if (!cancelled()) busy = false;
    }
  };
</script>

<dialog bind:this={dialog} onclose={() => (cancel(), onClose())} aria-labelledby="add-server-title">
  <header>
    <h2 id="add-server-title">{preset ? t("login.logInTo", { host: new URL(preset.origin).host }) : t("login.addServer")}</h2>
    <button type="button" class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  {#if !found}
    <form onsubmit={check}>
      <label>
        {t("login.serverAddress")}
        <!-- svelte-ignore a11y_autofocus -->
        <input bind:value={address} required placeholder="chat.example.org" autocomplete="url" autofocus />
      </label>
      <p class="hint">{t("login.addServerHint")}</p>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <footer>
        <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
        <button type="submit" class="primary" disabled={busy || !address.trim()}>{busy ? t("login.looking") : t("login.continue")}</button>
      </footer>
    </form>
  {:else}
    <form onsubmit={logIn}>
      <div class="server">
        {#if info?.logo}
          <img src={iconHref(found.origin, 192, info.logo)} alt="" />
        {:else}
          <span class="initial" aria-hidden="true">{initials(info?.name ?? new URL(found.origin).host)}</span>
        {/if}
        <div class="names">
          {#if info?.name}<strong class="ellipsis">{info.name}</strong>{/if}
          <span class="ellipsis">{found.origin}</span>
        </div>
      </div>
      <!-- The home account first (an account there is made if needed); a
           password there only when asked for (and in the desktop app). -->
      {#if homeCookie && ways.home}
        <p class="hint">{t("login.joinWithHome", { server: new URL(found.origin).host, home: new URL(home).host })}</p>
        <button type="button" class="primary" disabled={busy} onclick={() => viaHome()}>{t("login.continueWithHome", { home: new URL(home).host })}</button>
        {#if ways.fedi}
          {@const via = ways.fedi.host}
          <button type="button" class="link" disabled={busy} onclick={() => viaHome(via)}>{t("login.continueAsInstead", { handle: ways.fedi.handle })}</button>
        {/if}
      {:else if homeCookie && ways.fedi}
        {@const via = ways.fedi.host}
        <p class="hint">{t("login.joinWithFedi", { server: new URL(found.origin).host })}</p>
        <button type="button" class="primary" disabled={busy} onclick={() => viaHome(via)}>{t("login.continueAs", { handle: ways.fedi.handle })}</button>
      {/if}
      {#if withPassword}
        <label>
          {t("login.name")}
          <!-- svelte-ignore a11y_autofocus -->
          <input bind:value={name} required maxlength="128" autocomplete="username" autofocus />
        </label>
        <label>
          {t("login.password")}
          <input bind:value={password} type="password" required autocomplete="current-password" />
        </label>
        {@const [before, after] = around(t("login.makeAccountThere"), "link")}
        <p class="hint">{before}<a href={found.origin} target="_blank" rel="noopener noreferrer">{t("login.ownSite")}</a>{after}</p>
      {:else}
        <button type="button" class="link" onclick={() => (withPassword = true)}>{t("login.havePassword")}</button>
      {/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <footer>
        {#if preset}
          <button type="button" class="secondary" onclick={() => dialog?.close()}>{t("common.cancel")}</button>
        {:else}
          <button type="button" class="secondary" onclick={back}>{t("common.back")}</button>
        {/if}
        {#if withPassword}
          <button type="submit" class="primary" disabled={busy || !name.trim() || !password}>{busy ? t("login.loggingIn") : t("login.logIn")}</button>
        {/if}
      </footer>
    </form>
  {/if}
</dialog>

<style>
  .link {
    align-self: center;
    background: none;
    color: var(--text-muted);
    text-decoration: underline;
  }
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
  form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h2 {
    margin: 0;
    font-size: 18px;
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
  input {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
    font-size: 15px;
    text-transform: none;
  }
  .hint,
  .server {
    margin: 0;
    color: var(--text-muted);
    font-size: 13px;
  }
  .server {
    display: flex;
    gap: 12px;
    align-items: center;
    min-width: 0;
  }
  .server img,
  .initial {
    flex: none;
    width: 48px;
    height: 48px;
    border-radius: 14px;
  }
  .initial {
    display: grid;
    place-items: center;
    background: var(--bg-input);
    font-size: 20px;
    font-weight: 600;
  }
  .names {
    display: flex;
    flex-direction: column;
    min-width: 0;
    color: var(--text-muted);
    font-size: 13px;
  }
  .names strong {
    color: var(--text);
    font-size: 16px;
  }
  .ellipsis {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .error {
    margin: 0;
    color: var(--danger);
    font-size: 14px;
  }
  .close {
    font-size: 24px;
    line-height: 1;
  }
  .primary,
  .secondary {
    padding: 8px 16px;
    border-radius: 4px;
  }
  .primary {
    background: var(--accent-strong);
    color: #fff;
  }
  .primary:disabled {
    opacity: 0.5;
  }
  .secondary {
    background: var(--bg-input);
  }
</style>
