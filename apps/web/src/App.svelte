<script lang="ts">
  import { leaving, loadLost, saveLost, type StreamMemory } from "./lib/stream/rewatch";
  import Client from "./components/Client.svelte";
  import Login from "./components/Login.svelte";
  import { createAccountsApi, type Account, type AccountActions } from "./lib/accounts";
  import { connect, type Connection, type Credentials } from "./lib/connection.svelte";
  import {
    bridgeUrl,
    certificateProblem,
    getBridge,
    isDesktop,
    isKallisti,
    kallistiOrigin,
    kallistiSocket,
    linuxApp,
    parseAddress,
    trustServer,
    type CertificateProblem,
  } from "./lib/desktop";
  import { httpBase } from "./lib/textChannels";
  import { safeGet, safeSet } from "./lib/storage";
  import { connectionError, proxyUrl } from "./lib/status";
  import { loadPlace, placeKey, savePlace, type Place } from "./lib/place";
  import type { Drafts } from "./lib/composer";
  import { createPendingFiles, indexedDbStore } from "./lib/pendingFiles";
  import { busyName, loggedOut, lossNotice, nextDelay, QUIET_MS, shouldReconnect } from "./lib/reconnect";
  import { untrack } from "svelte";
  import { checkForUpdate } from "./lib/update";
  import { browserPushSupport, turnOffPush } from "./lib/push";
  import { voiceSupported } from "./lib/voice/support";
  import { loadCaptureWorklet, startAfter } from "./lib/voice/worklet";
  import { closeVoiceOutput } from "./lib/voice/output";
  import { swapContext } from "./lib/voice/restart";
  import { settings } from "./lib/settings.svelte";
  import { themeOf } from "./lib/settings";
  import { fediResult, reauthInstance, withoutFediResult } from "./lib/fedi";
  import Toast from "./components/Toast.svelte";

  // The colour theme (see app.css), also before the login.
  const systemDark = matchMedia("(prefers-color-scheme: dark)");
  let isSystemDark = $state(systemDark.matches);
  $effect(() => {
    const update = () => (isSystemDark = systemDark.matches);
    systemDark.addEventListener("change", update);
    return () => systemDark.removeEventListener("change", update);
  });
  $effect(() => {
    const theme = themeOf(settings.value.theme, isSystemDark);
    document.documentElement.dataset.theme = theme;
    const rail = getComputedStyle(document.documentElement).getPropertyValue("--bg-rail").trim();
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", rail);
  });

  const PROXY_URL = import.meta.env.VITE_PROXY_URL ?? proxyUrl(location);

  let connection = $state<Connection | null>(null);
  // The account API lives on the Kallisti server (same host as the bridge).
  // The desktop app's server is chosen on the login screen (see below).
  let api = $state.raw(createAccountsApi(httpBase(PROXY_URL)));
  let account = $state<Account | null>(null);
  // Until the server says whether a session is saved: no login screen (it
  // would flash). A saved session connects at once; logging out is the way
  // to another account. Only if the user has not acted yet: a late answer
  // must not undo a login. The desktop app asks its server when one is
  // chosen (see chooseServer).
  let checking = $state(true);
  const autoLogin = (acc: Account | null) => {
    if (!acc || connection || account !== null) return void (checking = false);
    account = acc;
    // Still "checking" until the connection exists (start is async).
    void start({ username: acc.name, account: true, ...(desktop && { server: desktopServer }) }).finally(() => (checking = false));
  };
  // An account connection failed (for example an expired session): ask the
  // server again, so the login form comes back instead of a dead end.
  $effect(() => {
    if (error && connection?.account) void api.me().then((acc) => (account = acc));
  });

  // A lost connection comes back by itself (see shouldReconnect): until
  // then the old one stays on screen, with a note.
  const lost = $derived(
    !!connection &&
      shouldReconnect({
        status: connection.state.status,
        closed: connection.closed,
        closeReason: connection.closeReason,
        wasSynced: connection.wasSynced,
      }),
  );
  const error = $derived(connection && !lost ? connectionError(connection.state, connection.closed) : null);
  // Since when it is lost, and a clock while it is (see lossNotice).
  let lostAt = $state<number | null>(null);
  let now = $state(Date.now());
  $effect(() => {
    if (!lost) return void (lostAt = null);
    untrack(() => (lostAt = now = Date.now()));
    const tick = setInterval(() => (now = Date.now()), 500);
    return () => clearInterval(tick);
  });
  const notice = $derived(lostAt === null ? "none" : lossNotice(now - lostAt));

  // Rejected or kicked: do not wait for the server to close the socket.
  $effect(() => {
    if (error) connection?.close();
  });

  // Shown on the login screen after leaving (for example a failed logout).
  let loginNote = $state<string | null>(null);
  // Back from a fediverse login (see lib/fedi): its result, once (a reload
  // must not show it again).
  const fediBack = isDesktop() ? null : fediResult(location.search);
  if (fediBack) history.replaceState(history.state, "", withoutFediResult(location.pathname + location.search + location.hash));
  let fediSignup = $state(fediBack?.kind === "register");
  let fediNotice = $state<string | null>(null);
  // Back from a fresh login for the first password: to the settings again.
  let reopenSettings = $state(fediBack?.kind === "reauth");
  if (!isDesktop())
    void untrack(() => api)
      .me()
      .then((acc) => {
        // Logged in: a notice over the app (and no name step); else errors
        // on the login screen.
        if (acc) fediSignup = false;
        if (fediBack && fediBack.kind !== "register") {
          if (acc) fediNotice = fediBack.text;
          else if (fediBack.kind === "error") loginNote = fediBack.text;
        }
        autoLogin(acc);
      });
  const leave = (note: string | null) => {
    loginNote = note;
    stopReconnecting();
    connection?.close();
    connection = null;
    keepPlace(null);
    // Nothing to watch again after a logout.
    streams = { watching: [], lost: [] };
    if (lastCredentials) saveLost(tabStorage(), streamsKeyOf(lastCredentials), []);
    closeAudio(audio);
    audio = null;
  };
  // The session ended on the server (a logout or password change on
  // another device): back to the login screen, as after a logout.
  $effect(() => {
    if (connection?.closed && loggedOut(connection.closeReason))
      untrack(() => {
        account = null;
        void stopPush();
        leave("You were logged out.");
      });
  });
  // This browser's push notifications end with the login (see lib/push).
  const stopPush = () => (browserPushSupport() === "ok" ? turnOffPush(httpBase(PROXY_URL)) : Promise.resolve());
  const accountActions: AccountActions = {
    get temporary() {
      return account?.temporary ?? false;
    },
    logout: async () => {
      void stopPush(); // not waited for: the logout ends it on the server too
      const ok = await api.logout();
      if (ok) account = null;
      leave(ok ? null : "Logging out did not finish: you may still be logged in.");
    },
    logoutAll: async () => {
      void stopPush();
      await api.logoutAll();
      account = null;
      leave("Logged out on all devices.");
    },
    changePassword: async (old, next) => {
      await api.changePassword(old, next);
      if (account) account = { ...account, temporary: false, password: true }; // a guest account is kept now
    },
    get hasPassword() {
      return account?.password !== false;
    },
    get fedi() {
      return account?.fedi ?? null;
    },
    fediEnabled: () => api.fediEnabled(),
    registration: () => api.registration(),
    fediLink: async (instance, password) => {
      location.assign(await api.fediStart(instance, password)); // back with ?fedi=linked
    },
    fediUnlink: async () => {
      await api.fediUnlink();
      if (account) account = { ...account, fedi: null };
    },
    fediPublic: async (on) => {
      await api.fediPublic(on);
      if (account?.fedi) account = { ...account, fedi: { ...account.fedi, public: on } };
    },
    fediReauth: async () => {
      if (!account?.fedi) return;
      location.assign(await api.fediReauth(reauthInstance(account.fedi.handle, safeGet(localStorage, "fediInstance")))); // back with ?fedi=reauth
    },
    get whipUrl() {
      return api.whipUrl;
    },
    streamKey: () => api.streamKey(),
    newStreamKey: () => api.newStreamKey(),
    removeStreamKey: () => api.removeStreamKey(),
  };

  // The desktop app: a Kallisti server works as in the web app (with a
  // session token instead of the cookie); any other address through the
  // app's local Mumble bridge. The web app: through its Kallisti server.
  const desktop = isDesktop();
  let desktopServer = $state(safeGet(localStorage, "server") ?? "");
  let desktopKind = $state<"kallisti" | "mumble" | null>(null);
  let kallisti = $state<string | null>(null); // the Kallisti server's origin
  const sessionStore = (origin: string) => {
    const key = `session:${origin}`;
    return { get: () => safeGet(localStorage, key) || null, set: (token: string | null) => void safeSet(localStorage, key, token ?? "") };
  };
  // The newest choice wins (the one from last time is looked at on start).
  let choices = 0;
  const chooseServer = async (address: string) => {
    const mine = ++choices;
    loginNote = null;
    const origin = kallistiOrigin(address);
    const kind = origin === null ? "other" : await isKallisti(origin);
    if (mine !== choices) return;
    if (kind === "unreachable") {
      loginNote = `Cannot reach ${address}. Check the address, or try again.`;
      checking = false;
      return;
    }
    const found = kind === "kallisti";
    desktopServer = address;
    safeSet(localStorage, "server", address);
    kallisti = found ? origin : null;
    if (found) {
      api = createAccountsApi(origin!, fetch, sessionStore(origin!));
      const acc = await api.me();
      if (mine !== choices) return;
      desktopKind = "kallisti";
      return autoLogin(acc);
    }
    desktopKind = "mumble";
    checking = false;
  };
  const changeServer = () => {
    choices++;
    desktopKind = null;
    kallisti = null;
    account = null;
  };
  // A server from last time: looked at again at once.
  untrack(() => desktop && (desktopServer ? void chooseServer(desktopServer) : (checking = false)));
  const urlFor = async (credentials: Credentials): Promise<{ url: string; protocols?: string[] }> => {
    if (!desktop) return { url: PROXY_URL };
    if (kallisti) return kallistiSocket(kallisti, sessionStore(kallisti).get());
    const server = parseAddress(credentials.server ?? "");
    if (!server) throw new Error("Enter a server address, for example voice.example.org or voice.example.org:64738");
    return { url: bridgeUrl(await getBridge(), server) };
  };

  // A new version on the server (web app only: the desktop app carries its
  // own): checked now, every 15 minutes, when the tab shows again, and
  // after each (re)connect, as a deploy restarts the server. No automatic
  // reload: it would end voice.
  let updateReady = $state(false);
  let updateClosed = $state(false);
  const checkUpdate = async () => {
    if (!desktop && !updateReady && document.visibilityState === "visible") updateReady = await checkForUpdate(location.origin);
  };
  $effect(() => {
    void checkUpdate();
    const timer = setInterval(checkUpdate, 15 * 60_000);
    const onShow = () => void checkUpdate();
    document.addEventListener("visibilitychange", onShow);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onShow);
    };
  });
  $effect(() => {
    if (connection?.state.status === "synced") untrack(() => void checkUpdate());
  });

  // Each start gets a number: a slower earlier start must not win.
  let starts = 0;
  // Desktop: the server's certificate is new or changed. The user decides.
  let lastCredentials: Credentials | null = null;
  const certificate = $derived.by((): (CertificateProblem & { server: string }) | null => {
    const problem = connection?.closed ? certificateProblem(connection.closeReason) : null;
    return problem && lastCredentials ? { ...problem, server: lastCredentials.server ?? "" } : null;
  });
  const trustAndConnect = async () => {
    const server = parseAddress(certificate?.server ?? "");
    if (!certificate || !server || !lastCredentials) return;
    const credentials = lastCredentials;
    try {
      await trustServer(server, certificate.fingerprint);
    } catch (err) {
      connection = null;
      loginNote = `Could not trust the certificate: ${err instanceof Error ? err.message : String(err)}`;
      return;
    }
    await start(credentials);
  };

  // The audio context lives as long as the user stays: reconnects use it
  // again (a new one would need another click).
  let audio: AudioContext | null = null;
  // Started without a click (a saved login, a refresh), browsers may hold
  // it suspended: the first click or key turns the sound on. Not in the
  // Linux app: it holds the context itself for a while (see startAfter).
  let audioState = $state<AudioContextState | null>(null);
  const wakeSound = () => {
    if (audioState === "suspended") void audio?.resume().catch(() => {});
  };
  // Made in a click where possible: browsers allow audio only after a
  // user gesture.
  const makeAudio = () => {
    const ctx = new AudioContext({ sampleRate: 48_000, latencyHint: "interactive" });
    // Linux app: the capture module first, then the output (see startAfter).
    if (linuxApp()) startAfter(ctx, loadCaptureWorklet(ctx));
    audioState = linuxApp() ? null : ctx.state;
    if (!linuxApp()) ctx.onstatechange = () => audio === ctx && (audioState = ctx.state);
    return ctx;
  };
  const closeAudio = (ctx: AudioContext | null) => {
    if (ctx) closeVoiceOutput(ctx);
    void ctx?.close().catch(() => {});
  };
  // Into voice, or a new output device: everything moves to a fresh
  // context (see lib/voice/restart). Not on reconnects. keepMic: the
  // microphone stays open (a new output device).
  const restartAudio = (keepMic = false) => {
    const move = (ctx: AudioContext) => [connection, attempt].forEach((c) => c?.setAudio(ctx, keepMic));
    audio = swapContext(audio, makeAudio, move, closeAudio);
  };
  const start = async (credentials: Credentials) => {
    lastCredentials = credentials;
    const mine = ++starts;
    loginNote = null;
    stopReconnecting();
    // After a refresh: back where this tab was (see lib/place).
    place = loadPlace(tabStorage(), placeKeyOf(credentials));
    drafts = { value: new Map() };
    streams = { watching: [], lost: loadLost(tabStorage(), streamsKeyOf(credentials), Date.now()) };
    // Per server and user: another account here does not get the files.
    pendingFiles = createPendingFiles(indexedDbStore(), [kallisti ?? location.origin, credentials.server ?? "", credentials.username].join(" "));
    connection?.close();
    closeAudio(audio);
    // Created here: a login is a user gesture.
    audio = voiceSupported() ? makeAudio() : null;
    if (!audio) audioState = null;
    try {
      const { url, protocols } = await urlFor(credentials);
      if (mine !== starts) return;
      connection = connect(url, credentials, audio, protocols);
    } catch (err) {
      loginNote = err instanceof Error ? err.message : String(err);
    }
  };

  // Reconnecting: one attempt at a time, with growing waits; at once when
  // the tab shows again or the network comes back. A generation number
  // stops attempts that started before a logout or a new login.
  const ATTEMPT_TIMEOUT_MS = 15_000;
  const BUSY_NAME_TRIES = 5;
  let place = $state<Place | null>(null);
  // Per tab: a refresh comes back, a new tab starts fresh (two tabs must
  // not fight over voice). Reading sessionStorage can throw (blocked storage).
  const tabStorage = () => {
    try {
      return sessionStorage;
    } catch {
      return undefined;
    }
  };
  const placeKeyOf = (c: Credentials) => placeKey(kallisti ?? location.origin, c.server ?? "", c.username);
  const keepPlace = (p: Place | null, left = false) => {
    place = p;
    if (lastCredentials) savePlace(tabStorage(), placeKeyOf(lastCredentials), p, left);
  };
  // The streams watched, kept over reconnects and reloads, per tab (see
  // lib/stream/rewatch): they are watched again when they come back soon.
  let streams = $state.raw<StreamMemory>({ watching: [], lost: [] });
  const streamsKeyOf = (c: Credentials) => `streams:${placeKeyOf(c)}`;
  const keepStreams = () => {
    if (lastCredentials) saveLost(tabStorage(), streamsKeyOf(lastCredentials), leaving(streams.watching, streams.lost, Date.now()));
  };
  // Unsent texts, kept over reconnects (the Client is new each time); a
  // login starts without.
  let drafts = $state.raw<Drafts>({ value: new Map() });
  // Files picked but not sent, by chat: also over reloads (see lib/pendingFiles).
  let pendingFiles = $state.raw(createPendingFiles(null, ""));
  let attempt: Connection | null = $state(null);
  let attempts = 0;
  let busyTries = 0;
  let starting = false;
  let generation = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const stopReconnecting = () => {
    generation++;
    clearTimeout(timer);
    timer = undefined;
    attempt?.close();
    attempt = null;
    attempts = busyTries = 0;
  };
  const tryNow = async () => {
    clearTimeout(timer);
    timer = undefined;
    if (attempt || starting || !lastCredentials || !lost) return;
    starting = true;
    const mine = generation;
    try {
      const { url, protocols } = await urlFor(lastCredentials);
      if (mine !== generation || !lost) return;
      const next = connect(url, lastCredentials, audio, protocols);
      attempt = next;
      // A connection that never gets anywhere: give up on it, try again.
      setTimeout(() => {
        if (attempt === next && next.state.status !== "synced") next.close();
      }, ATTEMPT_TIMEOUT_MS);
    } catch {
      schedule();
    } finally {
      starting = false;
    }
  };
  // Each second while quiet; the growing waits count only after it.
  const schedule = () => {
    if (timer !== undefined) return;
    const lostFor = Date.now() - (lostAt ?? Date.now());
    timer = setTimeout(tryNow, nextDelay(lostFor, attempts));
    if (lostFor >= QUIET_MS) attempts++;
  };
  $effect(() => {
    if (lost && !attempt) untrack(schedule);
  });
  $effect(() => {
    const next = attempt;
    if (!next) return;
    if (next.state.status === "synced") {
      untrack(() => {
        connection = next; // the client comes back where it was (place)
        attempt = null;
        attempts = busyTries = 0;
      });
    } else if (next.closed || next.state.status === "rejected" || next.state.status === "kicked") {
      untrack(() => {
        attempt = null;
        next.close();
        // Our old session may still hold the name: try a few more times.
        if (busyName(next.state) && ++busyTries < BUSY_NAME_TRIES) return schedule();
        // Refused now (for example banned meanwhile), logged out, or a
        // certificate question: show it; else try again.
        const refused = next.state.status === "rejected" || next.state.status === "kicked";
        if (refused || loggedOut(next.closeReason) || certificateProblem(next.closeReason)) connection = next;
        else schedule();
      });
    }
  });
  const wake = () => {
    if (lost && document.visibilityState === "visible") void tryNow();
  };
</script>

<svelte:window ononline={wake} onpagehide={() => (place && keepPlace(place, true), keepStreams())} onpointerdowncapture={wakeSound} onkeydowncapture={wakeSound} />
<svelte:document onvisibilitychange={wake} />

{#if checking}
  <div class="connecting" aria-busy="true"></div>
{:else if !connection || error}
  <Login
    error={certificate ? null : (error ?? loginNote)}
    {certificate}
    onTrust={trustAndConnect}
    onCancelTrust={() => leave(null)}
    {api}
    {account}
    desktop={desktop ? { server: desktopServer, kind: desktopKind, onServer: chooseServer, onChangeServer: changeServer } : null}
    onAccountChange={(acc) => (account = acc)}
    onConnect={start}
    {fediSignup}
    onFediSignupDone={() => (fediSignup = false)}
  />
{:else if connection.state.status === "synced"}
  <!-- Quiet first (the server is usually back within seconds): a small
       sign, then the banner (see lossNotice). -->
  {#if lost && notice === "banner"}
    <div class="reconnecting" role="status">
      Connection lost. Reconnecting…
      <button type="button" onclick={() => void tryNow()}>Retry now</button>
    </div>
  {:else if lost && notice === "sign"}
    <div class="reconnecting quiet" role="status">Reconnecting…</div>
  {/if}
  <!-- While lost, the old view stays readable but takes no input (it would be lost). -->
  <div class="client" inert={lost}>
    {#key connection}
      <Client {connection} account={account ? accountActions : undefined} resume={place} onPlace={(p) => keepPlace(p)} {audioState} onRestartAudio={restartAudio} onLogIn={() => leave(null)} site={kallisti ?? location.origin} {drafts} {pendingFiles} {streams} openSettings={reopenSettings} onSettingsOpened={() => (reopenSettings = false)} />
    {/key}
  </div>
{:else}
  <div class="connecting">Connecting…</div>
{/if}

<div class="status-bar" aria-hidden="true"></div>

{#if fediNotice}<Toast text={fediNotice} />{/if}

{#if updateReady && !updateClosed}
  <div class="update" role="status">
    A new version of Kallisti is available.
    <button type="button" onclick={() => location.reload()}>Reload</button>
    <button type="button" class="close" aria-label="Close" onclick={() => (updateClosed = true)}>×</button>
  </div>
{/if}

<style>
  /* The installed iOS app (iOS 26 and later) blurs the page below the
     status bar unless a fixed box at least 90% wide, over 10px tall, with
     one background colour sits at the top (WebKit's
     LocalFrameView::fixedContainerEdges); then the status bar gets that
     colour. Only with iOS's own status bar, not black-translucent.
     background-clip: text keeps the box itself invisible. */
  .status-bar {
    display: none;
  }
  @supports (-webkit-touch-callout: none) {
    @media (display-mode: standalone) {
      .status-bar {
        display: block;
        position: fixed;
        inset: 0 0 auto;
        z-index: 40;
        height: 11px;
        background-color: var(--bg-main);
        background-clip: text;
        pointer-events: none;
      }
    }
  }
  .update {
    position: fixed;
    top: calc(8px + var(--safe-top));
    left: 50%;
    transform: translateX(-50%);
    z-index: 60;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 8px 8px 14px;
    border-radius: 8px;
    background: var(--bg-menu);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.4);
    font-size: 14px;
    white-space: nowrap;
  }
  .update button {
    padding: 4px 12px;
    border-radius: 4px;
    background: var(--accent-strong);
    color: #fff;
    font-weight: 600;
  }
  .update .close {
    padding: 0 6px;
    background: none;
    color: var(--text-muted);
    font-size: 18px;
  }
  .reconnecting {
    position: fixed;
    top: calc(8px + var(--safe-top));
    left: 50%;
    transform: translateX(-50%);
    z-index: 50;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 6px 8px 6px 14px;
    border: 1px solid var(--danger);
    border-radius: 6px;
    background: var(--bg-rail);
    color: var(--text);
    font-size: 13px;
    font-weight: 600;
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.3);
  }
  .reconnecting.quiet {
    padding: 3px 10px;
    border-color: var(--border-subtle);
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 400;
    box-shadow: none;
    opacity: 0.85;
  }
  .reconnecting button {
    padding: 4px 10px;
    border-radius: 4px;
    background: var(--bg-active);
    color: var(--text);
  }
  .client {
    display: contents;
  }
  .connecting {
    display: grid;
    place-items: center;
    height: 100%;
    color: var(--text-muted);
  }
</style>
