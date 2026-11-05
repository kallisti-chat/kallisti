// @vitest-environment jsdom
import { flushSync } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createServerSession } from "../src/lib/serverSession.svelte";
import { fakeConnection, type FakeConnection } from "./helpers/fakeConnection.svelte";

const credentials = { username: "alice" };

// urlFor answers (or fails) when the test says (the desktop app's bridge
// is slow).
const setup = (local = false, deferred = false) => {
  const made: FakeConnection[] = [];
  const pending: ((fail?: boolean) => void)[] = [];
  const session = createServerSession<FakeConnection>({
    site: () => "https://chat.example",
    urlFor: (c) =>
      deferred
        ? new Promise((resolve, reject) =>
            pending.push((fail) => (fail ? reject(new Error("no bridge")) : resolve({ url: `wss://chat.example/ws?${c.username}`, local }))),
          )
        : Promise.resolve({ url: `wss://chat.example/ws?${c.username}`, local }),
    connect: (target) => {
      const c = fakeConnection(target.url);
      made.push(c);
      return c;
    },
    tabStorage: () => undefined,
  });
  return { session, made, pending };
};
// Lets awaited promises and effects run.
const settle = async () => {
  await vi.advanceTimersByTimeAsync(0);
  flushSync();
};

describe("a server session", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("connects, and a later start wins over a slower earlier one", async () => {
    const { session, made, pending } = setup(false, true);
    void session.start(credentials);
    void session.start({ username: "bob" });
    pending[1]!(); // bob's address first
    await settle();
    pending[0]!(); // alice's late
    await settle();
    expect(made.map((c) => c.url)).toEqual(["wss://chat.example/ws?bob"]);
    expect(session.connection).toBe(made[0]);
    session.dispose();
  });

  it("leaving drops a start that still waits for its address", async () => {
    const { session, made, pending } = setup(false, true);
    void session.start(credentials);
    session.leave();
    pending[0]!();
    await settle();
    expect(made).toHaveLength(0);
    expect(session.connection).toBeNull();
    session.dispose();
  });

  it("leaving drops a reconnect attempt that still waits for its address", async () => {
    const { session, made, pending } = setup(false, true);
    void session.start(credentials);
    pending[0]!();
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000); // the attempt asks for its address
    session.leave();
    pending[1]!();
    await settle();
    expect(made).toHaveLength(1);
    session.dispose();
  });

  it("dispose ends everything: a failed lookup after it schedules nothing, starts do nothing", async () => {
    const { session, made, pending } = setup(false, true);
    void session.start(credentials);
    pending[0]!();
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000); // an attempt asks for its address
    session.dispose();
    pending[1]!(true);
    await settle();
    expect(vi.getTimerCount()).toBe(0); // nothing scheduled
    await vi.advanceTimersByTimeAsync(60_000);
    void session.start(credentials);
    session.wake();
    void session.tryNow();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(pending).toHaveLength(2);
    expect(made).toHaveLength(1);
  });

  it("dispose drops a start that still waits for its address", async () => {
    const { session, made, pending } = setup(false, true);
    void session.start(credentials);
    session.dispose();
    pending[0]!();
    await settle();
    expect(made).toHaveLength(0);
  });

  it("an old attempt still looking up its address does not hold up the next loss", async () => {
    const { session, made, pending } = setup(false, true);
    void session.start(credentials);
    pending[0]!();
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000); // attempt 1 waits for its address
    void session.start(credentials); // a new login meanwhile
    pending[2]!();
    await settle();
    made[1]!.sync();
    made[1]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000);
    expect(pending.length).toBe(4); // the new loss asked again
    pending[1]!(); // the old lookup ends late: nothing comes of it
    await settle();
    void session.tryNow(); // the new lookup still runs: no second one
    expect(pending.length).toBe(4);
    pending[3]!();
    await settle();
    expect(made.map((c) => c.url)).toHaveLength(3);
    session.dispose();
  });

  it("retry connects again and keeps drafts, files and the place", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.drop("kicked");
    session.drafts.value = new Map([["3", "half a message"]]);
    const files = session.pendingFiles;
    const place = { text: 3 } as unknown as Parameters<typeof session.keepPlace>[0];
    session.keepPlace(place);
    const streams = session.streams;
    await session.retry();
    await settle();
    expect(made).toHaveLength(2);
    expect(session.connection).toBe(made[1]);
    expect(session.drafts.value.get("3")).toBe("half a message");
    expect(session.pendingFiles).toBe(files);
    expect(session.place).toEqual(place);
    expect(session.streams).toBe(streams);
    session.dispose();
  });

  it("close ends everything and closes the connection", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    session.close();
    expect(made[0]!.closeRequests).toBe(1);
    void session.start(credentials);
    await settle();
    expect(made).toHaveLength(1);
  });

  it("dispose ends everything: no attempt later", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    session.dispose();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(made).toHaveLength(1);
  });

  it("gives up on an attempt that gets nowhere in 15 s", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await settle(); // the first attempt goes at once
    expect(made).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(14_999);
    expect(made[1]!.closeRequests).toBe(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(made[1]!.closeRequests).toBe(1);
    session.dispose();
  });

  it("comes back quietly after a loss: an attempt each second, then the new connection", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    expect(session.lost).toBe(true);
    expect(session.notice).toBe("none");
    await vi.advanceTimersByTimeAsync(1000);
    flushSync();
    expect(made).toHaveLength(2);
    made[1]!.sync();
    flushSync();
    expect(session.connection).toBe(made[1]);
    expect(session.lost).toBe(false);
    session.dispose();
  });

  it("tries straight once when the apps' bridge cannot reach the server, then shows the error", async () => {
    const made: FakeConnection[] = [];
    let tries = 0;
    const session = createServerSession<FakeConnection>({
      site: () => "https://chat.example",
      urlFor: async () => (tries++ === 0 ? { url: "ws://127.0.0.1:1/?kallisti", site: "https://chat.example" } : { url: "wss://chat.example/ws" }),
      connect: (target) => {
        const c = fakeConnection(target.url);
        made.push(c);
        return c;
      },
      tabStorage: () => undefined,
    });
    await session.start(credentials);
    await settle();
    made[0]!.drop("cannot reach wss://chat.example/ws: invalid peer certificate");
    flushSync();
    await settle();
    expect(made.map((c) => c.url)).toEqual(["ws://127.0.0.1:1/?kallisti", "wss://chat.example/ws"]);
    made[1]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(60_000);
    flushSync();
    expect(made).toHaveLength(2); // no more tries: the session never got in
    expect(session.lost).toBe(false);
    expect(session.error).not.toBeNull();
    session.dispose();
  });

  it("tries once at once when the connection is lost, then each second", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await settle();
    expect(made).toHaveLength(2);
    made[1]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(900);
    expect(made).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(200);
    expect(made).toHaveLength(3);
    session.dispose();
  });

  it("keeps the old view until the new connection has the open channel's messages", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    session.keepPlace({ session: 1, view: { kind: "text", id: 5 }, voice: null, thread: null, selfMute: false, selfDeaf: false });
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000);
    made[1]!.sync(false);
    flushSync();
    expect(session.connection).toBe(made[0]); // no text channels yet
    made[1]!.settleText();
    flushSync();
    expect(made[1]!.historyRequests).toEqual([5]);
    expect(session.connection).toBe(made[0]); // no messages yet
    made[1]!.load(5);
    flushSync();
    expect(session.connection).toBe(made[1]);
    expect(session.lost).toBe(false);
    session.dispose();
  });

  it("takes a new connection that is not ready after 5 s anyway", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    session.keepPlace({ session: 1, view: { kind: "text", id: 5 }, voice: null, thread: null, selfMute: false, selfDeaf: false });
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000);
    made[1]!.sync();
    flushSync();
    await vi.advanceTimersByTimeAsync(4900);
    flushSync();
    expect(session.connection).toBe(made[0]);
    await vi.advanceTimersByTimeAsync(200);
    flushSync();
    expect(session.connection).toBe(made[1]);
    session.dispose();
  });

  it("drops a new connection that closes while it waits to be ready", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    session.keepPlace({ session: 1, view: { kind: "text", id: 5 }, voice: null, thread: null, selfMute: false, selfDeaf: false });
    made[0]!.drop();
    flushSync();
    await settle();
    made[1]!.sync();
    flushSync();
    made[1]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000);
    expect(made).toHaveLength(3); // the next attempt, not after 5 s
    expect(session.connection).toBe(made[0]);
    session.dispose();
  });

  it("shown again, starts over: an attempt from before the freeze is dropped", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await settle();
    expect(made).toHaveLength(2); // hangs: frozen
    session.wake();
    await settle();
    expect(made[1]!.closeRequests).toBe(1);
    expect(made).toHaveLength(3);
    session.dispose();
  });

  it("does not count time in the background: shown again, the loss is quiet again", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(15_000); // hidden: attempts fail meanwhile
    for (const c of made.slice(1)) c.drop();
    flushSync();
    expect(session.notice).toBe("banner");
    session.wake();
    flushSync();
    expect(session.notice).toBe("none");
    session.dispose();
  });

  it("shown again, checks a connection that looks alive", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    flushSync();
    session.wake();
    expect(made[0]!.aliveChecks).toBe(1);
    made[0]!.drop();
    flushSync();
    session.wake(); // lost: an attempt instead
    expect(made[0]!.aliveChecks).toBe(1);
    session.dispose();
  });

  it("shows a small sign after 2 s and the banner after 10 s", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(2500);
    flushSync();
    expect(session.notice).toBe("sign");
    await vi.advanceTimersByTimeAsync(9000);
    flushSync();
    expect(session.notice).toBe("banner");
    session.dispose();
  });

  it("stops at a refusal by the server's rules and shows it", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    await vi.advanceTimersByTimeAsync(1000);
    flushSync();
    made[1]!.drop("join as a guest on the server's own site");
    flushSync();
    expect(session.connection).toBe(made[1]);
    expect(session.error).toBe("join as a guest on the server's own site");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(made).toHaveLength(2);
    session.dispose();
  });

  it("shows the desktop bridge's reason only for its own connections", async () => {
    for (const local of [true, false]) {
      const { session, made } = setup(local);
      await session.start(credentials);
      await settle();
      made[0]!.drop("cannot reach voice.example:64738: refused");
      flushSync();
      expect(session.error).toBe(local ? "cannot reach voice.example:64738: refused" : "Lost the connection to the server.");
      session.dispose();
    }
  });

  it("leaves: no connection and no more attempts", async () => {
    const { session, made } = setup();
    await session.start(credentials);
    await settle();
    made[0]!.sync();
    made[0]!.drop();
    flushSync();
    session.leave();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(session.connection).toBeNull();
    expect(made).toHaveLength(1);
    session.dispose();
  });
});
