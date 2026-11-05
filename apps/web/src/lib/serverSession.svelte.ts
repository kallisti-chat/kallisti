import { untrack } from "svelte";
import type { ServerState } from "@kallisti/protocol";
import type { Credentials } from "./connection.svelte";
import type { Drafts } from "./composer";
import { certificateProblem, type CertificateProblem } from "./desktop";
import { createPendingFiles, indexedDbStore } from "./pendingFiles";
import { loadPlace, placeKey, savePlace, type Place } from "./place";
import { bridgeFailed } from "./desktop";
import { busyName, loggedOut, lossNotice, nextDelay, QUIET_MS, readyToShow, refusedForGood, shouldReconnect } from "./reconnect";
import { connectionError } from "./status";
import { leaving, loadLost, saveLost, type StreamMemory } from "./stream/rewatch";

// What a session needs of a connection (lib/connection's, or a test's).
export type SessionConnection = {
  readonly state: ServerState;
  readonly closed: boolean;
  readonly closeReason: string;
  readonly wasSynced: boolean;
  // The text side is known (text channels, or none: a plain Mumble server).
  readonly textSettled: boolean;
  readonly text: { readonly loaded: Readonly<Record<number, boolean>> };
  requestHistory: (channel: number) => void;
  // Closes it at once when the server does not answer in time.
  checkAlive: (timeoutMs: number) => void;
  close: () => void;
  setAudio: (ctx: AudioContext, keepMic?: boolean) => void;
};

// Where a connection goes; local: through the desktop app's own bridge
// to a Mumble server; site: the server's HTTP address when the URL is not
// on it (a Kallisti server through the bridge).
export type Target = { url: string; protocols?: string[]; local?: boolean; site?: string };

export type SessionDeps<C> = {
  // This server's site (the Kallisti server's origin): the key of what is
  // kept per server and user (read when used: the desktop app's server is
  // chosen later).
  site: () => string;
  urlFor: (credentials: Credentials) => Promise<Target>;
  connect: (target: Target, credentials: Credentials) => C;
  // Per tab: a refresh comes back, a new tab starts fresh (two tabs must
  // not fight over voice). Undefined: blocked storage.
  tabStorage: () => Storage | undefined;
};

const ATTEMPT_TIMEOUT_MS = 15_000;
// A new connection that is in but not ready to show (see readyToShow) by
// then replaces the old one anyway.
const READY_WAIT_MS = 5_000;
// Shown again: a connection that looks alive answers by then, or is lost
// (a suspended app's socket can be dead without a close).
const ALIVE_MS = 2_000;
const BUSY_NAME_TRIES = 5;

// One server's connection and what goes with it: it comes back by itself
// after a loss (see lib/reconnect), and keeps the place, the watched
// streams, unsent texts and files over reconnects and reloads. dispose:
// nothing more happens (no start, attempt or effect); the connection stays
// open (leave closes it).
export const createServerSession = <C extends SessionConnection>(deps: SessionDeps<C>) => {
  // Raw: the objects themselves (their getters are reactive), so viaBridge
  // finds them.
  let connection = $state.raw<C | null>(null);
  let attempt = $state.raw<C | null>(null);
  let credentials = $state.raw<Credentials | null>(null);
  // Connections through the desktop app's own bridge: only its reasons
  // are shown as they are (see connectionError).
  const viaBridge = new WeakSet<C>();
  // Kallisti servers through the apps' bridge (their site is not the URL).
  const viaAppBridge = new WeakSet<C>();
  const connectTo = (target: Target, c: Credentials) => {
    const next = deps.connect(target, c);
    if (target.local) viaBridge.add(next);
    if (target.site) viaAppBridge.add(next);
    return next;
  };

  let place = $state<Place | null>(null);
  let streams = $state.raw<StreamMemory>({ watching: [], lost: [] });
  // Unsent texts, kept over reconnects (the client is new each time); a
  // login starts without.
  let drafts = $state.raw<Drafts>({ value: new Map() });
  // Files picked but not sent, by chat: also over reloads (see lib/pendingFiles).
  let pendingFiles = $state.raw(createPendingFiles(null, ""));
  const placeKeyOf = (c: Credentials) => placeKey(deps.site(), c.server ?? "", c.username);
  const streamsKeyOf = (c: Credentials) => `streams:${placeKeyOf(c)}`;

  // A lost connection comes back by itself (see shouldReconnect): until
  // then the old one stays on screen, with a note.
  const lost = $derived(
    !!connection &&
      shouldReconnect({
        status: connection.state.status,
        closed: connection.closed,
        closeReason: connection.closeReason,
        wasSynced: connection.wasSynced,
        straightNext: viaAppBridge.has(connection) && bridgeFailed(connection.closeReason),
      }),
  );
  // Why it failed, for the login screen (null: fine, or it comes back).
  const error = $derived(connection && !lost ? connectionError(connection.state, connection.closed, connection.closeReason, viaBridge.has(connection)) : null);
  // Desktop: the server's certificate is new or changed. The user decides.
  const certificate = $derived.by((): (CertificateProblem & { server: string }) | null => {
    const problem = connection?.closed ? certificateProblem(connection.closeReason) : null;
    return problem && credentials ? { ...problem, server: credentials.server ?? "" } : null;
  });
  // Since when it is lost, and a clock while it is (see lossNotice).
  let lostAt = $state<number | null>(null);
  let now = $state(Date.now());
  const notice = $derived(lostAt === null ? "none" : lossNotice(now - lostAt));

  // Reconnecting: one attempt at a time, with growing waits; at once when
  // the tab shows again or the network comes back. A generation number
  // stops attempts that started before a logout or a new login; each start
  // gets a number too (a slower earlier start must not win).
  let starts = 0;
  let attempts = 0;
  let busyTries = 0;
  let starting = false;
  let generation = 0;
  let disposed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  // Attempts in but not ready to show: asked for the open channel's
  // messages, waited for long enough.
  const askedHistory = new WeakSet<C>();
  const waiting = new WeakSet<C>();
  let overdue = $state.raw<C | null>(null);
  const stopReconnecting = () => {
    generation++;
    clearTimeout(timer);
    timer = undefined;
    attempt?.close();
    attempt = null;
    attempts = busyTries = 0;
    starting = false; // an older attempt's lookup must not hold up the next
  };
  const tryNow = async () => {
    clearTimeout(timer);
    timer = undefined;
    if (disposed || attempt || starting || !credentials || !lost) return;
    starting = true;
    const mine = generation;
    try {
      const target = await deps.urlFor(credentials);
      if (mine !== generation || !lost) return;
      const next = connectTo(target, credentials);
      attempt = next;
      // A connection that never gets anywhere: give up on it, try again.
      setTimeout(() => {
        if (attempt === next && next.state.status !== "synced") next.close();
      }, ATTEMPT_TIMEOUT_MS);
    } catch {
      if (mine === generation) schedule();
    } finally {
      if (mine === generation) starting = false;
    }
  };
  // The first at once (the server is usually there), then each second
  // while quiet; the growing waits count only after it.
  let tried = false;
  const schedule = () => {
    if (timer !== undefined) return;
    const lostFor = Date.now() - (lostAt ?? Date.now());
    timer = setTimeout(tryNow, tried ? nextDelay(lostFor, attempts) : 0);
    tried = true;
    if (lostFor >= QUIET_MS) attempts++;
  };

  const disposeEffects = $effect.root(() => {
    $effect(() => {
      if (!lost) return void (lostAt = null);
      untrack(() => {
        lostAt = now = Date.now();
        tried = false;
      });
      const tick = setInterval(() => (now = Date.now()), 500);
      return () => clearInterval(tick);
    });
    // Rejected or kicked: do not wait for the server to close the socket.
    $effect(() => {
      if (error) untrack(() => connection)?.close();
    });
    $effect(() => {
      if (lost && !attempt) untrack(schedule);
    });
    $effect(() => {
      const next = attempt;
      if (!next) return;
      if (next.state.status === "synced" && !next.closed) {
        const view = untrack(() => place)?.view ?? null;
        if (view?.kind === "text" && next.textSettled && !next.text.loaded[view.id] && !askedHistory.has(next)) {
          askedHistory.add(next);
          untrack(() => next.requestHistory(view.id));
        }
        if (!waiting.has(next)) {
          waiting.add(next);
          setTimeout(() => attempt === next && (overdue = next), READY_WAIT_MS);
        }
        if (!readyToShow({ status: "synced", textSettled: next.textSettled, loaded: next.text.loaded }, view) && overdue !== next) return;
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
          // certificate question: show it; also when it never got in (the
          // one straight try after the apps' bridge failed); else try again.
          const refused = next.state.status === "rejected" || next.state.status === "kicked";
          const final = refused || loggedOut(next.closeReason) || refusedForGood(next.closeReason) || certificateProblem(next.closeReason);
          if (final || !connection?.wasSynced) connection = next;
          else schedule();
        });
      }
    });
  });

  const begin = async (c: Credentials, fresh: boolean) => {
    if (disposed) return;
    credentials = c;
    const mine = ++starts;
    stopReconnecting();
    if (fresh) {
      // After a refresh: back where this tab was (see lib/place).
      place = loadPlace(deps.tabStorage(), placeKeyOf(c));
      drafts = { value: new Map() };
      streams = { watching: [], lost: loadLost(deps.tabStorage(), streamsKeyOf(c), Date.now()) };
      // Per server and user: another account here does not get the files.
      pendingFiles = createPendingFiles(indexedDbStore(), [deps.site(), c.server ?? "", c.username].join(" "));
    }
    connection?.close();
    const target = await deps.urlFor(c);
    if (mine !== starts) return;
    connection = connectTo(target, c);
  };

  const dispose = () => {
    disposed = true;
    starts++;
    stopReconnecting();
    disposeEffects();
  };

  // Untracked: callers save from their effects, which must not run again
  // when the credentials change.
  const keepPlace = (p: Place | null, left = false) => {
    place = p;
    const c = untrack(() => credentials);
    if (c) savePlace(deps.tabStorage(), placeKeyOf(c), p, left);
  };
  const keepStreams = () => {
    const [c, kept] = untrack(() => [credentials, streams] as const);
    if (c) saveLost(deps.tabStorage(), streamsKeyOf(c), leaving(kept.watching, kept.lost, Date.now()));
  };

  return {
    get connection() {
      return connection;
    },
    get credentials() {
      return credentials;
    },
    get lost() {
      return lost;
    },
    get notice() {
      return notice;
    },
    get error() {
      return error;
    },
    get certificate() {
      return certificate;
    },
    get place() {
      return place;
    },
    get streams() {
      return streams;
    },
    get drafts() {
      return drafts;
    },
    get pendingFiles() {
      return pendingFiles;
    },
    // Connects with these credentials (a login: fresh drafts and files);
    // a refusal of the address throws (for the login screen).
    start: (c: Credentials) => begin(c, true),
    // Connects again with the same credentials, keeping drafts, files,
    // place and streams ("Try again" after a refusal or a failed start).
    retry: async () => {
      const c = untrack(() => credentials);
      if (c) await begin(c, false);
    },
    // Ends it: no connection, nothing to come back to.
    leave: () => {
      starts++;
      stopReconnecting();
      connection?.close();
      connection = null;
      keepPlace(null);
      // Nothing to watch again after a logout.
      streams = { watching: [], lost: [] };
      if (credentials) saveLost(deps.tabStorage(), streamsKeyOf(credentials), []);
    },
    // Forgets the connection without closing it (the login screen after a
    // failed trust of a certificate).
    drop: () => {
      connection = null;
    },
    tryNow,
    // The tab shows again, or the network is back.
    // Time hidden does not count as lost (a suspended app's connection
    // closes in the background): quiet again, an attempt at once.
    wake: () => {
      if (document.visibilityState !== "visible") return;
      if (lost) {
        // An attempt from before a freeze may hang: start over (one that
        // is in waits to be ready, see readyToShow).
        if (attempt?.state.status !== "synced") stopReconnecting();
        lostAt = now = Date.now();
        void tryNow();
      } else if (connection?.wasSynced && !connection.closed) connection.checkAlive(ALIVE_MS);
    },
    keepPlace,
    keepStreams,
    // Into voice, or a new output device (see lib/voice/restart).
    setAudio: (ctx: AudioContext, keepMic: boolean) => [connection, attempt].forEach((c) => c?.setAudio(ctx, keepMic)),
    dispose,
    // For good, without forgetting the place (the app goes away).
    close: () => {
      dispose();
      connection?.close();
    },
  };
};
export type ServerSession<C extends SessionConnection> = ReturnType<typeof createServerSession<C>>;
