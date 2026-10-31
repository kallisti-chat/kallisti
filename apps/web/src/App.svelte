<script lang="ts">
  import Client from "./components/Client.svelte";
  import Login from "./components/Login.svelte";
  import Authorize from "./components/Authorize.svelte";
  import Connect from "./components/Connect.svelte";
  import AddServerDialog from "./components/AddServerDialog.svelte";
  import { createFriends } from "./lib/friends.svelte";
  import { friendPresence, isFriend, type FriendServer } from "./lib/friends";
  import { dmLink } from "./lib/messageLink";
  import ServerRail, { type RailServer } from "./components/ServerRail.svelte";
  import { apiWithToken, createAccountsApi, memoryStore, type Account, type AccountActions } from "./lib/accounts";
  import { connect, type Connection, type Credentials } from "./lib/connection.svelte";
  import { createServerSession, type ServerSession, type Target } from "./lib/serverSession.svelte";
  import { createServerHub } from "./lib/serverHub.svelte";
  import { iconHref } from "./lib/serverLogo";
  import {
    bridgeUrl,
    getBridge,
    hasBridge,
    notKallisti,
    isDesktop,
    isKallisti,
    kallistiOrigin,
    kallistiSocket,
    linuxApp,
    parseAddress,
    trustServer,
  } from "./lib/desktop";
  import { httpBase } from "./lib/textChannels";
  import { serverOrigin } from "./lib/serverList";
  import { localStore, safeGet, safeSet, tabStorage } from "./lib/storage";
  import { popupLogin, type ConnectPhase } from "./lib/popupLogin";
  import { proxyUrl } from "./lib/status";
  import { loggedOut } from "./lib/reconnect";
  import { untrack, type ComponentProps, type Snippet } from "svelte";
  import { checkForUpdate } from "./lib/update";
  import { browserPushSupport, dropServerPushes, pushIsOn, syncServerPush, turnOffPush } from "./lib/push";
  import { createVoiceAcross } from "./lib/voiceAcross.svelte";
  import { createServerLinks } from "./lib/serverLinks.svelte";
  import { createServerPush } from "./lib/serverPush.svelte";
  import { inviteOf } from "./lib/invites";
  import type { RailBadge } from "./lib/title";
  import { voiceSupported } from "./lib/voice/support";
  import { audioNeedsResume } from "./lib/voice";
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
  // The home login is known for sure (a check answered, or the user logged
  // in or out): only then may another owner end the added servers (see
  // lib/serverHub follow). A failed check knows nothing.
  let ownerKnown = $state(false);
  // Each login, logout and server change: older account checks count for
  // nothing (see the check after an error).
  let authGeneration = 0;
  const autoLogin = (acc: Account | null) => {
    // The popup of a handoff logs in nobody here.
    if (!acc || connection || account !== null || connectHome) return void (checking = false);
    account = acc;
    // Another server asks who you are: only the question, no chat (a Mumble
    // connection would take over this user's voice elsewhere).
    if (authorizeQuery) return void (checking = false);
    // Still "checking" until the connection exists (start is async).
    void start({ username: acc.name, account: true, ...(desktop && { server: desktopServer }) }).finally(() => (checking = false));
  };
  // An account connection failed (for example an expired session): ask the
  // server again, so the login form comes back instead of a dead end.
  $effect(() => {
    // Unknown (the server or the network failed): no change; an answer
    // from before a login, logout or server change counts for nothing.
    if (session.error && connection?.account) {
      const mine = authGeneration;
      void api.check().then((acc) => {
        if (acc !== "unknown" && mine === authGeneration) account = acc;
      });
    }
  });


  // Shown on the login screen after leaving (for example a failed logout).
  let loginNote = $state<string | null>(null);
  // Back from a fediverse login (see lib/fedi): its result, once (a reload
  // must not show it again).
  const fediBack = isDesktop() ? null : fediResult(location.search);
  // Another server asks who you are (see Authorize): after the login here.
  // (authorizeQuery: read after a kept address is restored, below.)
  // This page is the popup in which a Kallisti page adds this server (see
  // Connect): its home server, kept in this tab over the login's pages.
  // Such a page is only that popup: no account here, no servers of its own.
  const tab = tabStorage();
  const popup = (() => {
    if (isDesktop()) return popupLogin({ asked: null, mark: null, back: null, kept: true });
    const asked = new URLSearchParams(location.search).get("kallisti-connect");
    // kept: it survives the login's pages (until delivered, failed or given up).
    const kept = !asked || safeSet(tab, "kallisti-connect", asked);
    return popupLogin({ asked, mark: safeGet(tab, "kallisti-connect") || null, back: fediBack, kept });
  })();
  const connectHome = popup.home;
  let connectPhase = $state<ConnectPhase>(popup.phase);
  // Delivered or failed: a later login in this tab is a normal one.
  $effect(() => {
    if (connectPhase !== null && connectPhase !== "start") safeSet(tab, "kallisti-connect", "");
  });
  // Back from a login through another server with an invite (see Login):
  // its address again, so the client redeems it.
  // Kept just before the page went to that login (a known account comes
  // back without a result in the address): used by the next page, once.
  const keptInvite = safeGet(tab, "kallisti-invite");
  if (keptInvite) safeSet(tab, "kallisti-invite", "");
  // A link in the address wins (a newer one).
  const explicit = /^\/invite\//.test(location.pathname) || /[?&](join|open|authorize)=/.test(location.search);
  const restore = !explicit && /^\/(invite\/|\?(join|authorize)=)/.test(keptInvite ?? "") ? keptInvite : null;
  if (restore) history.replaceState(history.state, "", restore);
  else if (fediBack) history.replaceState(history.state, "", withoutFediResult(location.pathname + location.search + location.hash));
  const authorizeQuery = isDesktop() ? null : new URLSearchParams(location.search).get("authorize");
  let fediSignup = $state(fediBack?.kind === "register" || popup.resumed);
  let fediNotice = $state<string | null>(null);
  // Back from a fresh login for the first password: to the settings again.
  let reopenSettings = $state(fediBack?.kind === "reauth");
  if (!isDesktop())
    void untrack(() => api)
      .check()
      .then((answer) => {
        // The popup of a handoff: nobody's login here counts (it must not
        // end this browser's other tabs' servers, nor the name step).
        if (connectHome) return void (checking = false);
        // Unknown (the server or the network failed): the login form, but
        // nothing of the last login ends (see ownerKnown).
        if (answer !== "unknown") ownerKnown = true;
        const acc = answer === "unknown" ? null : answer;
        // Logged in: a notice over the app (and no name step); else errors
        // on the login screen.
        if (acc) fediSignup = false;
        if (fediBack && "text" in fediBack) {
          if (acc) fediNotice = fediBack.text;
          else if (fediBack.kind === "error") loginNote = fediBack.text;
        }
        autoLogin(acc);
      });
  // endAdded: the home login ends, and the added servers with it (see
  // lib/serverHub); not for a refused certificate (no login ended).
  const leave = (note: string | null, endAdded = true) => {
    authGeneration++;
    loginNote = note;
    addingFor = null;
    if (endAdded) hub.endAll();
    session.leave();
    closeAudio(audio);
    audio = null;
  };
  // An added server's session ended there (expired, or ended on its site):
  // it asks for a login on this device again, and stays in the list.
  $effect(() => {
    for (const a of hub.servers) {
      const c = a.session?.connection;
      if (a.token && c?.closed && loggedOut(c.closeReason)) untrack(() => hub.expire(a.origin, a.token!));
    }
  });
  // The session ended on the server (a logout or password change on
  // another device): back to the login screen, as after a logout. The
  // added servers close at once; their tokens are logged out there once
  // the home server says nobody is logged in (a tab that wakes late must
  // not end a newer login's, which may use the same tokens).
  $effect(() => {
    if (connection?.closed && loggedOut(connection.closeReason))
      untrack(() => {
        const ended = owner;
        hub.detach();
        if (ended !== null) void api.check().then((acc) => acc === null && hub.endLoginTokens(ended));
        account = null;
        void stopPush();
        leave("You were logged out.", false);
      });
  });
  // This browser's push notifications end with the login (see lib/push).
  const stopPush = () => (browserPushSupport() === "ok" ? turnOffPush(httpBase(PROXY_URL)) : Promise.resolve());
  const accountActions: AccountActions = {
    get temporary() {
      return account?.temporary ?? false;
    },
    // An answer that comes after another login or logout acts on nothing.
    logout: async () => {
      void stopPush(); // not waited for: the logout ends it on the server too
      const mine = authGeneration;
      const ok = await api.logout({ session: account?.session, name: account?.name });
      if (mine !== authGeneration) return;
      if (ok === "other") {
        // Another login is here now (another tab): this one ended there;
        // the added servers' tokens are that login's.
        hub.detach();
        account = null;
        return leave("Another login is active in this browser now.", false);
      }
      if (ok) account = null;
      leave(ok ? null : "Logging out did not finish: you may still be logged in.");
    },
    logoutAll: async () => {
      void stopPush();
      const mine = authGeneration;
      await api.logoutAll();
      if (mine !== authGeneration) return;
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
      location.assign(await api.fediReauth(reauthInstance(account.fedi.handle, safeGet(localStore(), "fediInstance")))); // back with ?fedi=reauth
    },
    get whipUrl() {
      return api.whipUrl;
    },
    streamKey: () => api.streamKey(),
    newStreamKey: () => api.newStreamKey(),
    removeStreamKey: () => api.removeStreamKey(),
    // Only on the own site (not the desktop app: see the server's consentSite).
    sites: isDesktop() ? undefined : { list: () => api.allowedSites(), revoke: (redirect) => api.revokeSite(redirect) },
  };

  // The desktop app: a Kallisti server works as in the web app (with a
  // session token instead of the cookie); any other address through the
  // app's local Mumble bridge. The web app: through its Kallisti server.
  const desktop = isDesktop();
  let desktopServer = $state(safeGet(localStore(), "server") ?? "");
  let desktopKind = $state<"kallisti" | "mumble" | null>(null);
  // Plain Mumble servers through the bridge (not in the iOS app; null:
  // not known yet).
  let bridge = $state<boolean | null>(null);
  if (desktop) void hasBridge().then((b) => (bridge = b));
  let kallisti = $state<string | null>(null); // the Kallisti server's origin
  const sessionStore = (origin: string) => {
    const key = `session:${origin}`;
    return { get: () => safeGet(localStore(), key) || null, set: (token: string | null) => void safeSet(localStore(), key, token ?? "") };
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
    // The iOS app: Kallisti servers only (no bridge).
    if (!found && !(await hasBridge())) {
      if (mine !== choices) return;
      loginNote = notKallisti(address);
      checking = false;
      return;
    }
    if (mine !== choices) return;
    desktopServer = address;
    safeSet(localStore(), "server", address);
    kallisti = found ? origin : null;
    if (found) {
      api = createAccountsApi(origin!, fetch, sessionStore(origin!));
      const generation = authGeneration;
      const answer = await api.check();
      if (mine !== choices || generation !== authGeneration) return;
      desktopKind = "kallisti";
      if (answer !== "unknown") ownerKnown = true;
      return autoLogin(answer === "unknown" ? null : answer);
    }
    // A Mumble server (or a Kallisti server that failed: the app cannot
    // tell): not a sure answer about the home login (see ownerKnown).
    desktopKind = "mumble";
    checking = false;
  };
  const changeServer = () => {
    authGeneration++;
    hub.endAll(); // another home server: not with this one's added servers
    choices++;
    desktopKind = null;
    kallisti = null;
    account = null;
  };
  // A server from last time: looked at again at once.
  untrack(() => desktop && (desktopServer ? void chooseServer(desktopServer) : (checking = false)));

  // local: through the desktop app's own bridge (a Mumble server).
  const urlFor = async (credentials: Credentials): Promise<Target> => {
    if (!desktop) return { url: PROXY_URL };
    if (kallisti) return kallistiSocket(kallisti, sessionStore(kallisti).get());
    const server = parseAddress(credentials.server ?? "");
    if (!server) throw new Error("Enter a server address, for example voice.example.org or voice.example.org:64738");
    return { url: bridgeUrl(await getBridge(), server), local: true };
  };
  // This server's connection, its reconnects and what goes with it (see
  // lib/serverSession).
  const session = createServerSession<Connection>({
    site: () => kallisti ?? location.origin,
    urlFor,
    connect: (target, c) => connect(target.url, c, audio, target.protocols),
    tabStorage,
  });
  $effect(() => session.dispose);
  const connection = $derived(session.connection);

  // Several servers in one client (see lib/serverHub): the home account's
  // list, each server with its own token on this device and its session;
  // all stay connected, one is on screen (null: the home server).
  const homeOrigin = () => kallisti ?? location.origin;
  const hub = createServerHub<ServerSession<Connection>>({
    storage: localStore,
    allowed: (origin) => {
      const r = serverOrigin(origin, homeOrigin(), !desktop);
      return "origin" in r && r.origin === origin;
    },
    // Each call names the account this page shows (a stale window after
    // another login in this browser is refused).
    list: () => {
      const name = owner === null ? null : account?.name;
      return name ? { load: () => api.servers(name), add: (s) => api.addServer(s, name), remove: (o) => api.removeServer(o, name) } : null;
    },
    owner: () => owner,
    open: (origin, name, token) => {
      const s = createServerSession<Connection>({
        site: () => origin,
        urlFor: async () => kallistiSocket(origin, token),
        connect: (target, c) => connect(target.url, c, audio, target.protocols),
        tabStorage,
      });
      void s.start({ username: name, account: true }).catch(() => {});
      return s;
    },
    revoke: async (origin, token) => void (await apiWithToken(origin, token).logout()),
  });
  // Whose the added servers' tokens are: an account at home (not a guest,
  // not a Mumble server in the desktop app). Once the first check is done,
  // every change goes to the hub (another login ends the previous one's).
  const owner = $derived(account && !account.temporary ? `${homeOrigin()} ${account.name}` : null);
  $effect(() => {
    const now = owner;
    if (ownerKnown) untrack(() => hub.follow(now));
  });
  // The add dialog, for the home login that opened it (another login
  // closes it, and a login there that ends later is not taken).
  // server: one of the list that needs a login on this device.
  let addingFor = $state<{ owner: string; server?: { origin: string; name: string }; address?: string } | null>(null);
  const addingServer = $derived(addingFor !== null && addingFor.owner === owner);
  // Closed by another login: forgotten (it must not open again later).
  $effect(() => {
    if (addingFor !== null && addingFor.owner !== owner) addingFor = null;
  });
  // The add dialog's logins: the token stays in memory until the hub
  // takes it.
  const dialogApi = (origin: string, token?: string) => {
    const store = memoryStore(token ?? null);
    return { api: createAccountsApi(origin, fetch, store), token: store.get };
  };
  // Once the home server is in: the saved ones (once per home login).
  $effect(() => {
    if (connection?.state.status === "synced") untrack(() => void hub.sync());
  });
  // Changes from other devices and tabs: when the page shows again.
  $effect(() => {
    // Not on a consent page: it opens no servers.
    const onShow = () => document.visibilityState === "visible" && !authorizeQuery && void hub.sync();
    document.addEventListener("visibilitychange", onShow);
    return () => document.removeEventListener("visibilitychange", onShow);
  });
  $effect(() => () => hub.close());
  const allSessions = () => [session, ...hub.servers.flatMap((a) => (a.session ? [a.session] : []))];

  // The friends list (see lib/friends): with the home account kept at
  // home (as the server list), else on this device. Loaded with the home
  // server, again when the page shows or another login comes.
  // Owned by the home login (owner, see below): no friends without one.
  let friendsNotice = $state<string | null>(null);
  const friends = createFriends({
    owner: () => (owner !== null && account ? account.name : null),
    remote: (name) => ({ load: () => api.friends(name), add: (f) => api.addFriend(f, name), remove: (o, u) => api.removeFriend(o, u, name) }),
    onError: () => (friendsNotice = "Your friends list could not be saved."),
  });
  // At once on another login (no account sees another's list), and when
  // the home server is in.
  $effect(() => {
    void owner;
    void connection?.state.status;
    untrack(() => void friends.sync());
  });
  $effect(() => {
    const onShow = () => document.visibilityState === "visible" && !authorizeQuery && void friends.sync();
    document.addEventListener("visibilitychange", onShow);
    return () => document.removeEventListener("visibilitychange", onShow);
  });
  // The home server takes part when it is a Kallisti server (the desktop
  // app with a plain Mumble server has no origin to keep friends by).
  const homeFriends = $derived(owner !== null && (!desktop || kallisti !== null));
  // Per server of the rail: its friends, for its client's user menus.
  const friendsOn = (origin: string) => ({
    has: (user: number) => isFriend(friends.list, origin, user),
    toggle: (user: number, name: string) => (isFriend(friends.list, origin, user) ? friends.remove(origin, user) : friends.add({ origin, user, name })),
    get rows() {
      return friendRows;
    },
    canMessage,
    message: (o: string, user: number) => void links.openMessageLink(dmLink(o, user)),
    remove: (o: string, user: number) => friends.remove(o, user),
  });
  // Where friends are: what the rail's servers show (nobody else tells).
  const friendRows = $derived.by(() => {
    // A lost connection tells nothing (its last state is old).
    const view = (origin: string, c: Connection | null, name: string): FriendServer[] =>
      c && !c.closed
        ? [{ origin, name, online: c.text.online, users: c.state.users, voiceName: (id) => (id === 0 ? null : (c.state.channels[id]?.name ?? null)) }]
        : [];
    return friendPresence(friends.list, [
      ...(homeFriends ? view(homeOrigin(), connection, railServers[0]?.name ?? "Home") : []),
      ...hub.servers.flatMap((a, i) => view(a.origin, a.session?.connection ?? null, railServers[i + 1]?.name ?? new URL(a.origin).host)),
    ]);
  });
  // A server here with a session (see serverLinks openMessageLink).
  const canMessage = (origin: string) =>
    (homeFriends && origin === homeOrigin() && connection?.state.status === "synced") || hub.servers.some((a) => a.origin === origin && a.session);
  const shownAdded = $derived(hub.shown === null ? null : (hub.servers.find((a) => a.origin === hub.shown) ?? null));
  // Each server's mark (see Client onBadge).
  let badges = $state<Record<string, RailBadge>>({});
  const setBadge = (id: string, b: RailBadge) => {
    const old = untrack(() => badges[id]);
    if (old?.mentions !== b.mentions || old.unread !== b.unread) badges = { ...untrack(() => badges), [id]: b };
  };
  const railEntry = (id: string, c: Connection | null, fallback: string): RailServer => ({
    ...badges[id],
    id,
    name: c?.state.channels[0]?.name ?? fallback,
    logo: c?.text.logo ? iconHref(c.httpBase, 192, c.text.logo) : null,
    active: hub.shown === (id || null),
  });
  const railServers = $derived<RailServer[]>([
    railEntry("", connection, "Home"),
    ...hub.servers.map((a) => railEntry(a.origin, a.session?.connection ?? null, new URL(a.origin).host)),
  ]);
  // A dialog open in the client that goes out of sight would keep the page
  // inert: it closes (a notification's click switches servers).
  const selectServer = (id: string) => {
    if ((id || null) !== hub.shown) for (const d of document.querySelectorAll<HTMLDialogElement>(".view dialog[open]")) d.close();
    hub.show(id || null);
  };

  // Push from added servers (see lib/serverPush).
  const pushOk = browserPushSupport() === "ok";
  const serverPush = createServerPush({
    name: () => (owner !== null && account ? account.name : null),
    // Logged out for sure: none of the servers stays.
    loaded: () => hub.loaded || (ownerKnown && owner === null),
    servers: () => hub.servers,
    isOn: async (name) => pushOk && pushIsOn(httpBase(PROXY_URL), name),
    sync: (server, homeOn, prefs) => (pushOk ? syncServerPush(server, homeOn, prefs) : Promise.resolve()),
    drop: (keep) => (pushOk ? dropServerPushes(keep) : Promise.resolve()),
  });
  $effect(() => serverPush.dispose);

  // Links from elsewhere: invites, ?join=, a push notice's ?open= (see
  // lib/serverLinks).
  const links = createServerLinks({
    servers: () => hub.servers,
    home: homeOrigin,
    owner: () => owner,
    select: selectServer,
    add: (target) => (addingFor = { owner: owner!, ...target }),
  });
  $effect(() => links.dispose);
  const { openInvite, openMessageLink } = links;
  let openUrl = new URLSearchParams(location.search).get("open");
  if (openUrl) history.replaceState(history.state, "", location.pathname);
  // Once the added servers are there (restored after the home connects).
  $effect(() => {
    if (openUrl && (hub.loaded || owner === null) && (connection?.state.status === "synced" || hub.servers.some((s) => openUrl!.startsWith(`${s.origin}/`) && s.session?.connection)))
      untrack(() => {
        openMessageLink(openUrl!);
        openUrl = null;
      });
  });
  // ?join=<an invite link> (another server's invite page sends people to
  // their home): once the home login is known and connected.
  const joinAsked = new URLSearchParams(location.search).get("join");
  if (joinAsked) history.replaceState(history.state, "", location.pathname);
  let joinUrl = $state(joinAsked);
  $effect(() => {
    if (joinUrl && connection?.state.status === "synced" && ownerKnown && (hub.listed || owner === null))
      untrack(() => {
        if (!openInvite(joinUrl!)) fediNotice = inviteOf(joinUrl!) ? "Log in with an account (not as a guest) to join other servers." : "This is not an invite link.";
        joinUrl = null;
      });
  });

  // Voice across servers (see lib/voiceAcross).
  const views = $derived([{ id: "", session }, ...hub.servers.flatMap((a) => (a.session ? [{ id: a.origin, session: a.session }] : []))]);
  const voice = createVoiceAcross({
    views: () => views,
    name: (id) => railServers.find((r) => r.id === id)?.name ?? "",
    key: (id) => id || homeOrigin(),
    show: (id) => selectServer(id),
    shown: () => hub.shown,
  });
  $effect(() => voice.dispose);
  const { remoteVoiceFor, pttOn } = voice;
  // An added server's account in its settings: logging out takes it out
  // of the rail; no fediverse link from here (that is its own site's).
  const addedActions = (origin: string): AccountActions => {
    // Its own token (another tab may store another login's for the origin).
    const api = apiWithToken(origin, hub.servers.find((a) => a.origin === origin)?.token ?? null);
    return {
      temporary: false,
      logout: async () => hub.remove(origin),
      logoutAll: async () => {
        // The entry this was for (another may have taken its place meanwhile).
        const entry = hub.servers.find((a) => a.origin === origin);
        await api.logoutAll();
        if (entry && hub.servers.includes(entry)) hub.remove(origin);
      },
      changePassword: (old, next) => api.changePassword(old, next),
      whipUrl: api.whipUrl,
      streamKey: () => api.streamKey(),
      newStreamKey: () => api.newStreamKey(),
      removeStreamKey: () => api.removeStreamKey(),
      hasPassword: true,
      fedi: null,
      fediEnabled: async () => false,
      fediLink: async () => {},
      fediUnlink: async () => {},
      fediPublic: async () => {},
      fediReauth: async () => {},
    };
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

  // Desktop: the server's certificate is new or changed. The user decides.
  const trustAndConnect = async () => {
    const certificate = session.certificate;
    const server = parseAddress(certificate?.server ?? "");
    const credentials = session.credentials;
    if (!certificate || !server || !credentials) return;
    try {
      await trustServer(server, certificate.fingerprint);
    } catch (err) {
      session.drop();
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
    if (audioNeedsResume(audioState)) void audio?.resume().catch(() => {});
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
    const move = (ctx: AudioContext) => allSessions().forEach((s) => s.setAudio(ctx, keepMic));
    audio = swapContext(audio, makeAudio, move, closeAudio);
  };
  const wakeAll = () => allSessions().forEach((s) => s.wake());
  const start = async (credentials: Credentials) => {
    loginNote = null;
    // A consent page (see Authorize): the login only, no chat (a guest or a
    // Mumble registration cannot answer it).
    if (authorizeQuery) return void (credentials.account || (loginNote = "Log in with an account of this server to answer this request."));
    // The old connection closes at once (before the address is known).
    const started = session.start(credentials);
    closeAudio(audio);
    // Created here: a login is a user gesture.
    audio = voiceSupported() ? makeAudio() : null;
    if (!audio) audioState = null;
    else for (const a of hub.servers) a.session?.setAudio(audio, true); // their microphones stay
    try {
      await started;
    } catch (err) {
      loginNote = err instanceof Error ? err.message : String(err);
    }
  };
</script>

<svelte:window ononline={wakeAll} onpagehide={() => allSessions().forEach((s) => (s.place && s.keepPlace(s.place, true), s.keepStreams()))} onpointerdowncapture={wakeSound} onkeydowncapture={wakeSound} />
<svelte:document onvisibilitychange={wakeAll} />

<!-- The rail of servers: the same while a server connects, fails or
     reconnects (in the client's drawer on phones; beside these screens). -->
{#snippet rail()}
  <ServerRail
    servers={railServers}
    onSelect={selectServer}
    onAdd={owner !== null ? () => (addingFor = { owner: owner! }) : undefined}
  />
{/snippet}
<!-- A screen without the client, with the rail if there are servers to switch to. -->
{#snippet withRail(content: Snippet)}
  {#if hub.servers.length}
    <div class="shell">{@render rail()}<div class="shell-main">{@render content()}</div></div>
  {:else}
    {@render content()}
  {/if}
{/snippet}

{#if connectHome && connectPhase}
  <Connect {api} home={connectHome} phase={connectPhase} />
{:else if authorizeQuery && account && !checking}
  <Authorize {api} query={authorizeQuery} serverName={location.host} />
{:else if checking}
  <div class="connecting" aria-busy="true"></div>
{:else}
  <!-- Every connected server's client, the one on screen visible. -->
  {#snippet serverView(id: string, s: ServerSession<Connection>, own: Partial<ComponentProps<typeof Client>>)}
    {#if s.connection?.state.status === "synced" && !s.error}
      {@const shown = (hub.shown ?? "") === id}
      <div class="view" hidden={!shown} inert={!shown}>
        <!-- Quiet first (the server is usually back within seconds): a small
             sign, then the banner (see lossNotice). -->
        {#if shown && s.lost && s.notice === "banner"}
          <div class="reconnecting" role="status">
            Connection lost. Reconnecting…
            <button type="button" onclick={() => void s.tryNow()}>Retry now</button>
          </div>
        {:else if shown && s.lost && s.notice === "sign"}
          <div class="reconnecting quiet" role="status">Reconnecting…</div>
        {/if}
        <!-- While lost, the old view stays readable but takes no input (it would be lost); the rail does. -->
        {#key s.connection}
          <Client connection={s.connection} link={links.linkFor(id)} onLinkUsed={() => links.used(id)} onInvite={openInvite} onServerLink={openMessageLink} onBadge={(b) => setBadge(id, b)} active={shown} pttHere={pttOn(id)} remoteVoice={remoteVoiceFor(id)} onShow={() => selectServer(id)} lost={s.lost} {rail} resume={s.place} onPlace={(p) => s.keepPlace(p)} {audioState} onRestartAudio={restartAudio} drafts={s.drafts} pendingFiles={s.pendingFiles} streams={s.streams} {...own} />
        {/key}
      </div>
    {/if}
  {/snippet}
  {@render serverView("", session, { friends: homeFriends ? friendsOn(homeOrigin()) : undefined, account: account ? accountActions : undefined, onLogIn: () => leave(null), site: kallisti ?? location.origin, openSettings: reopenSettings, onSettingsOpened: () => (reopenSettings = false) })}
  {#each hub.servers as a (a.origin)}
    {#if a.session}{@render serverView(a.origin, a.session, { friends: owner !== null ? friendsOn(a.origin) : undefined, pushSync: serverPush.syncFor(a), account: addedActions(a.origin), site: a.origin })}{/if}
  {/each}
  {#if shownAdded}
    {@const a = shownAdded}
    {@const s = a.session}
    {#if !(s?.connection?.state.status === "synced" && !s.error)}
      {#snippet added()}
        {@const host = new URL(a.origin).host}
        <div class="connecting">
          <p>{!s ? `Log in to ${host} on this device.` : s.error ? `${host}: ${s.error}` : `Connecting to ${host}…`}</p>
          <p class="actions">
            {#if !s && owner !== null}
              <button type="button" onclick={() => (addingFor = { owner: owner!, server: { origin: a.origin, name: a.name } })}>Log in</button>
            {:else if s?.error}
              <button type="button" onclick={() => void s.retry().catch(() => {})}>Try again</button>
            {/if}
            <button type="button" onclick={() => hub.remove(a.origin)}>Remove this server</button>
          </p>
        </div>
      {/snippet}
      {@render withRail(added)}
    {/if}
  {:else if !connection || session.error}
    {#snippet login()}
      <Login
        error={session.certificate ? null : (session.error ?? loginNote)}
        certificate={session.certificate}
        onTrust={trustAndConnect}
        onCancelTrust={() => leave(null, false)}
        {api}
        {account}
        desktop={desktop ? { server: desktopServer, kind: desktopKind, bridge, onServer: chooseServer, onChangeServer: changeServer } : null}
        onAccountChange={(acc) => {
          // A logout from the login screen (after a kick or refusal) ends the
          // home login too.
          authGeneration++;
          if (!acc) hub.endAll();
          account = acc;
          ownerKnown = true;
        }}
        onConnect={start}
        {fediSignup}
        onFediSignupDone={() => (connectHome ? window.close() : (fediSignup = false))}
        onHandoff={() => (connectPhase = "deliver")}
        keep={joinUrl ? `/?join=${encodeURIComponent(joinUrl)}` : authorizeQuery ? `/?authorize=${encodeURIComponent(authorizeQuery)}` : undefined}
      />
    {/snippet}
    {@render withRail(login)}
  {:else if connection.state.status !== "synced"}
    {#snippet connectingHome()}
      <div class="connecting">Connecting…</div>
    {/snippet}
    {@render withRail(connectingHome)}
  {/if}
{/if}

<div class="status-bar" aria-hidden="true"></div>

{#if fediNotice}<Toast text={fediNotice} />{/if}
{#key friendsNotice}{#if friendsNotice}<Toast text={friendsNotice} />{/if}{/key}
{#key hub.error}{#if hub.error}<Toast text={hub.error} />{/if}{/key}

{#if addingServer}
  <AddServerDialog
    home={homeOrigin()}
    homeCookie={!desktop}
    taken={hub.servers.map((a) => a.origin)}
    apiFor={dialogApi}
    server={addingFor?.server}
    address={addingFor?.address}
    onAdded={(origin, acc, token) => addingFor?.owner === owner && (addingFor.server ? hub.login(origin, acc.name, token) : hub.add(origin, acc.name, token))}
    onClose={() => {
      // An invite for a server that did not come: forgotten.
      const origin = addingFor?.server?.origin ?? addingFor?.address;
      if (origin && !hub.servers.some((s) => s.origin === origin && s.session)) links.used(origin);
      addingFor = null;
    }}
  />
{/if}

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
  /* The clients of servers not on screen stay mounted, out of sight but
     with their size (display: none would lose the lists' scroll position,
     and nothing could measure itself). */
  .view:not([hidden]) {
    display: contents;
  }
  .view[hidden] {
    display: block;
    position: fixed;
    inset: 0;
    z-index: -1;
    visibility: hidden;
    pointer-events: none;
  }
  /* A screen without the client, beside the rail. */
  .shell {
    display: grid;
    grid-template-columns: calc(72px + var(--safe-left)) 1fr;
    height: 100%;
  }
  .shell > :global(nav) {
    border-top: var(--safe-top) solid transparent;
    border-bottom: var(--safe-bottom) solid transparent;
    border-left: var(--safe-left) solid transparent;
  }
  .shell-main {
    min-width: 0;
    height: 100%;
    overflow: auto;
  }
  .connecting {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    height: 100%;
    padding: 16px;
    color: var(--text-muted);
    text-align: center;
  }
  .connecting p {
    margin: 0;
  }
  .connecting .actions {
    display: flex;
    gap: 8px;
  }
  .connecting .actions button {
    padding: 6px 12px;
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
  }
</style>
