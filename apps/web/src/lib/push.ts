// Web Push (see server/internal/push): while the app is closed, the server
// pushes mentions and direct messages to this browser, and a service
// worker (sw.ts, at /sw.js) shows them. It is registered only while push
// is on. A subscription belongs to one account on one server (owner).
import { isDesktop } from "./desktop";
import { t } from "./i18n.svelte";
import { localStore, safeGet, safeSet } from "./storage";

// "install": iOS has push only for apps on the home screen (16.4+).
export type PushSupport = "ok" | "install" | "none";

export const pushSupport = (e: {
  desktop: boolean;
  dev: boolean; // Vite serves no /sw.js
  serviceWorker: boolean;
  pushManager: boolean;
  notification: boolean;
  ios: boolean;
  standalone: boolean;
}): PushSupport => {
  if (e.desktop || e.dev) return "none";
  if (e.ios && !e.standalone) return "install";
  return e.serviceWorker && e.pushManager && e.notification ? "ok" : "none";
};

export const browserPushSupport = (): PushSupport =>
  pushSupport({
    desktop: isDesktop(),
    dev: import.meta.env.DEV,
    serviceWorker: "serviceWorker" in navigator,
    pushManager: "PushManager" in window,
    notification: "Notification" in window,
    // iPads say they are Macs, but have touch.
    ios: /iPad|iPhone|iPod/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1),
    standalone: matchMedia("(display-mode: standalone)").matches || (navigator as { standalone?: boolean }).standalone === true,
  });

// The server's VAPID key (base64url) as the Push API wants it.
export const applicationServerKey = (key: string): Uint8Array<ArrayBuffer> => {
  const b64 = key.replaceAll("-", "+").replaceAll("_", "/") + "=".repeat((4 - (key.length % 4)) % 4);
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
};

// A subscription made with another key (new server keys) must be made again.
export const keyMatches = (current: ArrayBuffer | null, key: string): boolean => {
  if (!current) return false;
  const [a, b] = [new Uint8Array(current), applicationServerKey(key)];
  return a.length === b.length && a.every((v, i) => v === b[i]);
};

export const pushOwner = (base: string, name: string) => `${base} ${name.toLowerCase()}`;

// What this browser wants no pushes for: chats it keeps quiet (and threads
// of quiet channels, but not the loud ones; see pushLists), and users it
// blocked.
export type PushPrefs = { quiet: number[]; loud: number[]; blocked: readonly string[] };

export const subscriptionBody = (sub: PushSubscriptionJSON, prefs: PushPrefs) => ({ endpoint: sub.endpoint, keys: sub.keys, ...prefs });

// Where pushes come from: the home server (its registration at "/", its
// cookie), or an added server (its own registration, see pushScope; its
// token).
type Target = { base: string; scope: string; token?: string };
const home = (base: string): Target => ({ base, scope: "/" });

// An added server's registration scope: the service worker's pushScope
// (not imported: the worker must stay one file, a shared module would be a
// chunk of its own).
// base64url: a scope's path must not hold an encoded "/" (%2F).
export const serverPushScope = (server: string, home: string) =>
  `${home}/push/${btoa(server).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "")}/`;

// Whose the home server's push is (an added server's follows it).
const OWNER_KEY = "pushOwner";

const request = (t: Target, init: RequestInit = {}): RequestInit => ({
  ...init,
  credentials: t.token ? "omit" : "include",
  headers: { ...(init.headers as Record<string, string>), ...(t.token && { Authorization: `Bearer ${t.token}` }) },
});

const send = async (t: Target, method: "POST" | "DELETE", body: unknown) => {
  const res = await fetch(`${t.base}/api/push/subscriptions`, request(t, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }));
  if (!res.ok) throw new Error(((await res.json().catch(() => ({}))) as { error?: string }).error ?? `request failed (${res.status})`);
};

const serverKey = async (t: Target): Promise<string> => {
  const res = await fetch(`${t.base}/api/push`, request(t));
  const key = res.ok ? ((await res.json().catch(() => ({}))) as { publicKey?: unknown }).publicKey : null;
  if (typeof key !== "string" || !key) throw new Error("this server has no push notifications");
  return key;
};

// The registration at exactly this scope (getRegistration gives the one
// that controls the address: "/" for an added server's without its own).
const registration = async (scope = "/") => {
  const reg = await navigator.serviceWorker?.getRegistration(scope);
  return reg?.scope === new URL(scope, location.origin).href ? reg : undefined;
};

// A registration with an active worker (pushManager needs one).
const active = (reg: ServiceWorkerRegistration) =>
  new Promise<ServiceWorkerRegistration>((resolve) => {
    const worker = reg.installing ?? reg.waiting;
    if (reg.active || !worker) return resolve(reg);
    worker.addEventListener("statechange", () => worker.state === "activated" && resolve(reg));
  });

// Subscribing runs one at a time. Turning off does not wait: it counts up
// offs, and subscribing asked for before that does not start, or undoes
// what it did (a sync from the start must not turn push on again). The
// subscription is the browser's, so offs in other tabs count too (shared
// in localStorage; this tab's count still works without storage).
let queue: Promise<unknown> = Promise.resolve();
const OFFS_KEY = "pushOffs";
let tabOffs = 0;
const offs = () => `${safeGet(localStore(), OFFS_KEY)} ${tabOffs}`;
const countOff = () => {
  tabOffs++;
  safeSet(localStore(), OFFS_KEY, String(Number(safeGet(localStore(), OFFS_KEY)) + 1));
};

// Subscribes (again, if the server's key changed) and tells the server,
// for the session now.
const subscribe = (t: Target, owner: string, prefs: PushPrefs) => {
  const asked = offs();
  const run = queue.then(async () => {
    if (asked !== offs()) return;
    const key = await serverKey(t);
    if (asked !== offs()) return;
    const reg = await active(await navigator.serviceWorker.register("/sw.js", { scope: t.scope }));
    let sub = await reg.pushManager.getSubscription();
    if (sub && !keyMatches(sub.options.applicationServerKey, key)) {
      await sub.unsubscribe();
      sub = null;
    }
    sub ??= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: applicationServerKey(key) });
    if (asked === offs()) await send(t, "POST", subscriptionBody(sub.toJSON(), prefs));
    if (asked !== offs()) return stopPush(t, reg, sub);
    if (!t.token) safeSet(localStore(), OWNER_KEY, owner);
  });
  queue = run.catch(() => {});
  return run;
};

export const pushIsOn = async (base: string, name: string) =>
  safeGet(localStore(), OWNER_KEY) === pushOwner(base, name) && !!(await (await registration())?.pushManager.getSubscription());

// Push turned on or off here (added servers follow; see App).
const changed = () => void globalThis.dispatchEvent?.(new Event("kallisti-push"));

// null: on; else why not.
export const turnOnPush = async (base: string, name: string, prefs: PushPrefs): Promise<string | null> => {
  if ((await Notification.requestPermission()) !== "granted") return t("client.pushNotAllowed");
  try {
    await subscribe(home(base), pushOwner(base, name), prefs);
    changed();
    return null;
  } catch (err) {
    return t("client.pushFailed", { error: err instanceof Error ? err.message : String(err) });
  }
};

// Also on logout. The server forgets it with the session anyway.
export const turnOffPush = (base: string) => {
  countOff();
  // The added servers' too: their clients may not be there (a server that
  // is down); each server drops its dead subscription at its next push.
  const run = Promise.all([stopPush(home(base)), dropServerPushes([]).catch(() => {})]).then(() => {}).finally(changed);
  queue = Promise.all([queue, run]).catch(() => {}); // later subscribing waits for it
  return run;
};

const stopPush = async (t: Target, reg?: ServiceWorkerRegistration, sub?: PushSubscription | null) => {
  if (!t.token) safeSet(localStore(), OWNER_KEY, "");
  reg ??= await registration(t.scope).catch(() => undefined);
  sub ??= await reg?.pushManager.getSubscription().catch(() => null);
  if (sub) await send(t, "DELETE", { endpoint: sub.endpoint }).catch(() => {});
  await sub?.unsubscribe().catch(() => {});
  await reg?.unregister().catch(() => {});
};

// On start and when the prefs change: push that is on for this account
// follows the session and the prefs (and comes back if the browser dropped
// the subscription); another account's goes.
export const syncPush = async (base: string, name: string, prefs: PushPrefs) => {
  const owner = safeGet(localStore(), OWNER_KEY);
  if (owner === pushOwner(base, name) && Notification.permission === "granted")
    await subscribe(home(base), owner, prefs).catch((err) => console.warn("push", err));
  else if (owner || (await registration().catch(() => undefined))) await turnOffPush(base);
};

// An added server (several servers in one client) pushes while the home
// server does (on: its push is on), with that server's token.
export const syncServerPush = async (server: { origin: string; token: string; name: string }, on: boolean, prefs: PushPrefs) => {
  const t: Target = { base: server.origin, scope: serverPushScope(server.origin, location.origin), token: server.token };
  // on, and still on in this browser (another tab may have turned it off).
  if (on && safeGet(localStore(), OWNER_KEY) && Notification.permission === "granted")
    await subscribe(t, pushOwner(server.origin, server.name), prefs).catch((err) => console.warn("push", err));
  else if (await registration(t.scope).catch(() => undefined)) await stopPush(t).catch(() => {});
};

// Registrations of servers no longer added (logged out there: the server
// dropped their subscriptions with the session) go.
export const dropServerPushes = async (keep: readonly string[]) => {
  const kept = keep.map((s) => serverPushScope(s, location.origin));
  for (const reg of (await navigator.serviceWorker?.getRegistrations?.().catch(() => [])) ?? []) {
    if (!reg.scope.startsWith(`${location.origin}/push/`) || kept.includes(reg.scope)) continue;
    await (await reg.pushManager.getSubscription().catch(() => null))?.unsubscribe().catch(() => {});
    await reg.unregister().catch(() => {});
  }
};
