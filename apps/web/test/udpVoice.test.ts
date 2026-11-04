import { describe, expect, it } from "vitest";
import { createAccountsApi } from "../src/lib/accounts";
import { kallistiSocket } from "../src/lib/desktop";

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
    expect(target.local).toBe(true);
    const url = new URL(target.url);
    expect(url.origin).toBe("ws://127.0.0.1:4567");
    expect(Object.fromEntries(url.searchParams)).toEqual({ secret: "s3cret", kallisti: "wss://chat.example/ws", udp: "64738" });
  });
});
