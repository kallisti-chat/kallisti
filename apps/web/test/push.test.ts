import { afterEach, describe, expect, it, vi } from "vitest";
import { applicationServerKey, keyMatches, pushOwner, pushSupport, serverPushScope, subscriptionBody, syncPush, turnOffPush } from "../src/lib/push";
import { clickTarget, noticeOf, windowFor } from "../src/lib/pushNotice";

const env = { desktop: false, dev: false, serviceWorker: true, pushManager: true, notification: true, ios: false, standalone: false };

describe("pushSupport", () => {
  it("is there in browsers with service workers and the Push API", () => {
    expect(pushSupport(env)).toBe("ok");
    expect(pushSupport({ ...env, ios: true, standalone: true })).toBe("ok"); // iOS 16.4+, on the home screen
  });

  it("asks iOS users to add the app to the home screen first", () => {
    expect(pushSupport({ ...env, ios: true, pushManager: false })).toBe("install");
    expect(pushSupport({ ...env, ios: true })).toBe("install");
  });

  it("is not there in the desktop app, in development, or without the APIs", () => {
    expect(pushSupport({ ...env, desktop: true })).toBe("none");
    expect(pushSupport({ ...env, dev: true })).toBe("none");
    expect(pushSupport({ ...env, pushManager: false })).toBe("none");
    expect(pushSupport({ ...env, serviceWorker: false })).toBe("none");
    expect(pushSupport({ ...env, notification: false })).toBe("none");
    expect(pushSupport({ ...env, ios: true, standalone: true, pushManager: false })).toBe("none"); // older iOS
  });
});

describe("keys", () => {
  it("decodes the server's base64url key", () => {
    expect([...applicationServerKey("AQL_-w")]).toEqual([1, 2, 255, 251]);
  });

  it("compares a subscription's key with the server's", () => {
    const key = applicationServerKey("AQL_-w");
    expect(keyMatches(key.buffer, "AQL_-w")).toBe(true);
    expect(keyMatches(key.buffer, "AQL_-A")).toBe(false);
    expect(keyMatches(null, "AQL_-w")).toBe(false);
  });
});

describe("subscriptions", () => {
  it("are sent with the quiet channels and blocked users", () => {
    const json = { endpoint: "https://push.example/x", keys: { p256dh: "P", auth: "A" }, expirationTime: null };
    expect(subscriptionBody(json, { quiet: [3, 4], loud: [5], all: [6], blocked: ["bob"] })).toEqual({
      endpoint: "https://push.example/x",
      keys: { p256dh: "P", auth: "A" },
      quiet: [3, 4],
      loud: [5],
      all: [6],
      blocked: ["bob"],
    });
  });

  it("belong to one account on one server", () => {
    expect(pushOwner("https://a", "Alice")).toBe(pushOwner("https://a", "alice"));
    expect(pushOwner("https://a", "alice")).not.toBe(pushOwner("https://b", "alice"));
    expect(pushOwner("https://a", "alice")).not.toBe(pushOwner("https://a", "bob"));
  });
});

const ORIGIN = "https://chat.example";

describe("noticeOf", () => {
  it("shows the server's short text and opens its link", () => {
    expect(noticeOf({ body: "Mention in #general from bob", url: "/channels/1/7", tag: "chat-1" }, ORIGIN)).toEqual({
      title: "Kallisti",
      options: { body: "Mention in #general from bob", tag: "chat-1", icon: "/icons/icon-192.png", badge: "/icons/icon-192.png", data: { url: `${ORIGIN}/channels/1/7` } },
    });
  });

  it("has the server's name and logo, an icon only from its logo", () => {
    const n = noticeOf({ body: "x", url: "/", title: "Our place", icon: "/api/logo/192.png?v=abc" }, ORIGIN);
    expect(n.title).toBe("Our place");
    expect(n.options.icon).toBe(`${ORIGIN}/api/logo/192.png?v=abc`);
    expect(n.options.badge).toBe("/icons/icon-192.png"); // a silhouette on Android
    for (const icon of ["https://evil.example/api/logo/192.png", "//evil.example/api/logo/192.png", "/api/logo/../files/x.png", "/files/x.png", 3]) {
      expect(noticeOf({ body: "x", icon }, ORIGIN).options.icon).toBe("/icons/icon-192.png");
    }
  });

  it("has a text for broken data", () => {
    const n = noticeOf("nope", ORIGIN);
    expect(n.options.body).toBe("New activity");
    expect(n.options.data.url).toBe(`${ORIGIN}/`);
  });
});

describe("clickTarget", () => {
  it("stays on this server", () => {
    expect(clickTarget("/channels/1/7", ORIGIN)).toBe(`${ORIGIN}/channels/1/7`);
    expect(clickTarget("https://evil.example/x", ORIGIN)).toBe(`${ORIGIN}/`);
    expect(clickTarget(42, ORIGIN)).toBe(`${ORIGIN}/`);
  });
});

describe("windowFor", () => {
  it("uses an open window of the app, the focused one first", () => {
    const a = { url: `${ORIGIN}/`, focused: false };
    const b = { url: `${ORIGIN}/channels/1/2`, focused: true };
    const other = { url: "https://other.example/", focused: true };
    expect(windowFor([a, b, other], ORIGIN)).toBe(b);
    expect(windowFor([other, a], ORIGIN)).toBe(a);
    expect(windowFor([other], ORIGIN)).toBeNull();
  });
});

// A browser with a service worker and the Push API. The server's key, or
// the push service's subscription, waits until released.
const fakeBrowser = (pauseAt: "key" | "subscribe") => {
  const KEY = "AQL_-w";
  const ENDPOINT = "https://fcm.googleapis.com/x";
  const storage = new Map<string, string>();
  const requests: string[] = [];
  let release = () => {};
  const gate = new Promise<void>((r) => (release = r));
  let registered = false;
  type Sub = { endpoint: string; options: { applicationServerKey: ArrayBuffer }; toJSON(): PushSubscriptionJSON; unsubscribe(): Promise<boolean> };
  let sub: Sub | null = null;
  const pushManager = {
    getSubscription: async () => sub,
    subscribe: async () => {
      if (pauseAt === "subscribe") await gate;
      return (sub = {
        endpoint: ENDPOINT,
        options: { applicationServerKey: applicationServerKey(KEY).buffer },
        toJSON: () => ({ endpoint: ENDPOINT, keys: {} }),
        unsubscribe: async () => ((sub = null), true),
      });
    },
  };
  const reg = { pushManager, unregister: async () => ((registered = false), true) };
  vi.stubGlobal("localStorage", { getItem: (k: string) => storage.get(k) ?? null, setItem: (k: string, v: string) => storage.set(k, v) });
  vi.stubGlobal("Notification", { permission: "granted" });
  vi.stubGlobal("navigator", {
    serviceWorker: {
      register: async () => ((registered = true), reg),
      ready: Promise.resolve(reg),
      getRegistration: async () => (registered ? reg : undefined),
    },
  });
  vi.stubGlobal("fetch", async (url: string, init?: RequestInit) => {
    requests.push(`${init?.method ?? "GET"} ${url}`);
    if (!url.endsWith("/api/push")) return new Response(null, { status: 204 });
    if (pauseAt === "key") await gate;
    return new Response(JSON.stringify({ publicKey: KEY }));
  });
  return { storage, requests, release: () => release(), state: () => ({ registered, subscribed: !!sub }) };
};

describe("push lifecycle", () => {
  afterEach(() => vi.unstubAllGlobals());

  it.each(["key", "subscribe"] as const)("stays off when turned off during a sync (waiting for the %s)", async (pauseAt) => {
    const b = fakeBrowser(pauseAt);
    const base = "https://chat.example";
    b.storage.set("pushOwner", pushOwner(base, "alice"));
    const sync = syncPush(base, "alice", { quiet: [], loud: [], all: [], blocked: [] });
    await new Promise((r) => setTimeout(r));
    const off = turnOffPush(base);
    b.release();
    await Promise.all([sync, off]);
    expect(b.state()).toEqual({ registered: false, subscribed: false });
    expect(b.storage.get("pushOwner")).toBe("");
    expect(b.requests).not.toContain(`POST ${base}/api/push/subscriptions`);
  });

  // Each tab has its own copy of the module, but they share the browser.
  const tab = async () => {
    vi.resetModules();
    return import("../src/lib/push");
  };

  it.each(["key", "subscribe"] as const)("stays off when another tab turns it off during a sync (waiting for the %s)", async (pauseAt) => {
    const b = fakeBrowser(pauseAt);
    const base = "https://chat.example";
    b.storage.set("pushOwner", pushOwner(base, "alice"));
    const [syncing, other] = [await tab(), await tab()];
    const sync = syncing.syncPush(base, "alice", { quiet: [], loud: [], all: [], blocked: [] });
    await new Promise((r) => setTimeout(r));
    await other.turnOffPush(base);
    b.release();
    await sync;
    expect(b.state()).toEqual({ registered: false, subscribed: false });
    expect(b.storage.get("pushOwner")).toBe("");
  });
});

describe("push from added servers", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("subscribes at the server's own scope with its token, follows the home's switch, and goes with the server", async () => {
    const HOME = "https://chat.example";
    const B = "https://b.example";
    const KEY = "AQL_-w";
    type Reg = { scope: string; active: object; pushManager: { getSubscription(): Promise<unknown>; subscribe(): Promise<unknown> }; unregister(): Promise<boolean> };
    const regs = new Map<string, Reg>();
    const requests: string[] = [];
    const storage = new Map<string, string>();
    const makeReg = (scope: string): Reg => {
      let sub: { endpoint: string; options: { applicationServerKey: ArrayBuffer }; toJSON(): PushSubscriptionJSON; unsubscribe(): Promise<boolean> } | null = null;
      return {
        scope,
        active: {},
        pushManager: {
          getSubscription: async () => sub,
          subscribe: async () =>
            (sub = {
              endpoint: `https://fcm.googleapis.com/${encodeURIComponent(scope)}`,
              options: { applicationServerKey: applicationServerKey(KEY).buffer },
              toJSON: () => ({ endpoint: "e", keys: {} }),
              unsubscribe: async () => ((sub = null), true),
            }),
        },
        unregister: async () => regs.delete(scope),
      };
    };
    vi.stubGlobal("location", { origin: HOME });
    vi.stubGlobal("localStorage", { getItem: (k: string) => storage.get(k) ?? null, setItem: (k: string, v: string) => storage.set(k, v) });
    vi.stubGlobal("Notification", { permission: "granted" });
    vi.stubGlobal("navigator", {
      serviceWorker: {
        register: async (_script: string, o: { scope: string }) => {
          const scope = new URL(o.scope, HOME).href;
          if (!regs.has(scope)) regs.set(scope, makeReg(scope));
          return regs.get(scope)!;
        },
        getRegistration: async (s: string) => regs.get(new URL(s, HOME).href) ?? regs.get(`${HOME}/`),
        getRegistrations: async () => [...regs.values()],
      },
    });
    vi.stubGlobal("fetch", async (url: string, init?: RequestInit) => {
      const auth = (init?.headers as Record<string, string> | undefined)?.Authorization ?? "-";
      requests.push(`${init?.method ?? "GET"} ${url} ${auth} ${init?.credentials}`);
      return url.endsWith("/api/push") ? new Response(JSON.stringify({ publicKey: KEY })) : new Response(null, { status: 204 });
    });
    const { dropServerPushes, syncServerPush, turnOffPush } = await import("../src/lib/push");
    const prefs = { quiet: [], loud: [], all: [], blocked: [] };
    storage.set("pushOwner", pushOwner(HOME, "lain")); // the home server's push is on
    const scope = serverPushScope(B, HOME);
    await syncServerPush({ origin: B, token: "tb", name: "lain" }, true, prefs);
    expect([...regs.keys()]).toEqual([scope]);
    expect(requests).toContain(`POST ${B}/api/push/subscriptions Bearer tb omit`);
    expect(await regs.get(scope)!.pushManager.getSubscription()).not.toBeNull();
    // The home's push off: this one goes too.
    await syncServerPush({ origin: B, token: "tb", name: "lain" }, false, prefs);
    expect(requests).toContain(`DELETE ${B}/api/push/subscriptions Bearer tb omit`);
    expect(regs.has(scope)).toBe(false);
    // Off in another tab (its key gone): nothing new here.
    storage.set("pushOwner", "");
    await syncServerPush({ origin: B, token: "tb", name: "lain" }, true, prefs);
    expect(regs.size).toBe(0);
    storage.set("pushOwner", pushOwner(HOME, "lain"));
    // On again; then the server is removed.
    await syncServerPush({ origin: B, token: "tb", name: "lain" }, true, prefs);
    await dropServerPushes([]);
    expect(regs.size).toBe(0);
    // Push off at home ends the added servers' too (their clients may not
    // be there: a server that is down).
    await syncServerPush({ origin: B, token: "tb", name: "lain" }, true, prefs);
    await turnOffPush(HOME);
    expect(regs.has(scope)).toBe(false);
  });
});
