import { describe, expect, it } from "vitest";
import { createAccountsApi } from "../src/lib/accounts";
import { bridgeFailed, kallistiSocket } from "../src/lib/desktop";

const fetchJson = (body: unknown) => (async () => new Response(JSON.stringify(body))) as unknown as typeof fetch;

describe("voice over UDP in the apps", () => {
  it("knows the server's Mumble port", async () => {
    const info = (body: unknown) => createAccountsApi("http://srv", fetchJson(body)).serverInfo();
    expect((await info({ password: false, udpPort: 64738 })).udpPort).toBe(64738);
    expect((await info({ password: false })).udpPort).toBeUndefined();
    expect((await info({ password: false, udpPort: 70000 })).udpPort).toBeUndefined();
    expect((await info({ password: false, udpPort: "64738" })).udpPort).toBeUndefined();
  });

  it("goes through the app's bridge, which sends voice over UDP", () => {
    const target = kallistiSocket("https://chat.example", "abc", { bridge: { port: 4567, secret: "s3cret" }, udpPort: 64738 });
    expect(target.protocols).toEqual(["kallisti", "kallisti.session.abc"]);
    expect("local" in target).toBe(false); // the server's close reasons, as without the bridge
    // Uploads, streams and pictures still go to the server itself.
    expect(target.site).toBe("https://chat.example");
    const url = new URL(target.url);
    expect(url.origin).toBe("ws://127.0.0.1:4567");
    expect(Object.fromEntries(url.searchParams)).toEqual({ secret: "s3cret", kallisti: "wss://chat.example/ws", udp: "64738" });
  });

  it("goes straight once the bridge could not reach the server", () => {
    // Another trust store or proxy than the web view's: the web view may.
    expect(bridgeFailed({ closeReason: "cannot reach wss://chat.example/ws: invalid peer certificate", failedToOpen: false })).toBe(true);
    expect(bridgeFailed({ closeReason: "logged out", failedToOpen: false })).toBe(false);
    expect(bridgeFailed({ closeReason: "", failedToOpen: false })).toBe(false);
    expect(bridgeFailed(undefined)).toBe(false);
  });

  it("goes straight when the socket to the bridge never opened", () => {
    // Android release builds refused ws://127.0.0.1 (cleartext): no reason.
    expect(bridgeFailed({ closeReason: "", failedToOpen: true })).toBe(true);
  });
});
