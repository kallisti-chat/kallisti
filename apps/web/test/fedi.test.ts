import { describe, expect, it } from "vitest";
import { fetchFediHandle, fediResult, reauthInstance, withoutFediResult } from "../src/lib/fedi";
import { createAccountsApi, needsReauth } from "../src/lib/accounts";

describe("fediResult", () => {
  it("reads the server's answer after a fediverse login", () => {
    expect(fediResult("")).toBeNull();
    expect(fediResult("?x=1")).toBeNull();
    expect(fediResult("?fedi=register")).toEqual({ kind: "register" });
    expect(fediResult("?fedi=linked")).toEqual({ kind: "linked", text: "Fediverse account linked: you can log in with it now." });
    expect(fediResult("?fedi=error&reason=taken")).toEqual({
      kind: "error",
      text: "This fediverse account is linked to another account already.",
    });
  });

  it("reads a fresh login for the first password", () => {
    expect(fediResult("?fedi=reauth")).toEqual({ kind: "reauth", text: "Confirmed: set your password now." });
    expect(fediResult("?fedi=error&reason=other")?.kind).toBe("error");
  });

  it("never shows text from the address", () => {
    const r = fediResult("?fedi=error&reason=%3Cb%3Eyou%20were%20hacked");
    expect(r).toEqual({ kind: "error", text: "The fediverse login did not work. Please try again." });
    expect(fediResult("?fedi=whatever")).toBeNull();
  });

  it("removes only its own parameters from the address", () => {
    expect(withoutFediResult("/?fedi=error&reason=denied")).toBe("/");
    expect(withoutFediResult("/?a=1&fedi=linked#x")).toBe("/?a=1#x");
    expect(withoutFediResult("/app?invite=abc")).toBe("/app?invite=abc");
  });
});

type Call = { url: string; init: RequestInit };
const fakeFetch = (status: number, body: unknown, calls: Call[]) =>
  (async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    return new Response(status === 204 ? null : JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  }) as unknown as typeof fetch;

describe("fediverse account API", () => {
  it("knows whether the server has fediverse logins", async () => {
    expect(await createAccountsApi("http://srv", fakeFetch(200, { enabled: true }, [])).fediEnabled()).toBe(true);
    expect(await createAccountsApi("http://srv", fakeFetch(404, {}, [])).fediEnabled()).toBe(false);
  });

  it("knows whether the server takes new accounts", async () => {
    expect(await createAccountsApi("http://srv", fakeFetch(200, { password: false, registration: false }, [])).registration()).toBe(false);
    expect(await createAccountsApi("http://srv", fakeFetch(200, { password: false, registration: true }, [])).registration()).toBe(true);
    // An older server says nothing: open, as before.
    expect(await createAccountsApi("http://srv", fakeFetch(200, { password: false }, [])).registration()).toBe(true);
    expect(fediResult("?fedi=error&reason=closed")).toEqual({ kind: "error", text: "This server does not take new accounts." });
  });

  it("starts a login or a link", async () => {
    const calls: Call[] = [];
    const api = createAccountsApi("http://srv", fakeFetch(200, { url: "https://social.example/oauth/authorize?x" }, calls));
    expect(await api.fediStart("social.example")).toBe("https://social.example/oauth/authorize?x");
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ instance: "social.example" });
    await api.fediStart("social.example", "pw");
    expect(calls[1]?.url).toBe("http://srv/api/fedi/start");
    expect(JSON.parse(String(calls[1]?.init.body))).toEqual({ instance: "social.example", link: true, password: "pw" });
  });

  it("reads the sign-up and creates the account", async () => {
    const api = createAccountsApi("http://srv", fakeFetch(200, { name: "alice", handle: "@alice@social.example" }, []));
    expect(await api.fediSignup()).toEqual({ name: "alice", handle: "@alice@social.example" });
    expect(await createAccountsApi("http://srv", fakeFetch(404, { error: "expired" }, [])).fediSignup()).toBeNull();
    const calls: Call[] = [];
    const create = createAccountsApi("http://srv", fakeFetch(201, { name: "alice" }, calls));
    expect(await create.fediCreate("alice", { challenge: "c", nonce: "n" })).toBe("alice");
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ name: "alice", challenge: "c", nonce: "n" });
  });

  it("reads the account's fediverse link and password state", async () => {
    const api = createAccountsApi(
      "http://srv",
      fakeFetch(200, { name: "alice", temporary: false, password: false, fedi: { handle: "@alice@social.example", public: true } }, []),
    );
    expect(await api.me()).toEqual({
      name: "alice",
      temporary: false,
      password: false,
      fedi: { handle: "@alice@social.example", public: true },
    });
  });

  it("finds a user's public handle", async () => {
    const calls: Call[] = [];
    expect(await fetchFediHandle("http://srv", "a b", fakeFetch(200, { handle: "@a@b.example" }, calls))).toBe("@a@b.example");
    expect(calls[0]?.url).toBe("http://srv/api/fedi/handle?name=a%20b");
    expect(await fetchFediHandle("http://srv", "x", fakeFetch(500, {}, []))).toBe("");
    expect(await fetchFediHandle("http://srv", "x", fakeFetch(404, "not json", []))).toBe("");
  });
});

describe("reauthInstance", () => {
  it("uses the server from the linked handle, or the one typed for it", () => {
    expect(reauthInstance("@alice@social.example", null)).toBe("social.example");
    expect(reauthInstance("@alice@social.example", "other.example")).toBe("social.example");
    expect(reauthInstance("@alice@social.example", "https://Social.example/")).toBe("https://Social.example/");
    expect(reauthInstance("@alice@127.0.0.1:9000", "http://127.0.0.1:9000")).toBe("http://127.0.0.1:9000");
  });
});

describe("first password", () => {
  it("tells a refusal for a missing fresh login apart", async () => {
    const api = createAccountsApi("http://srv", fakeFetch(403, { error: "confirm first", code: "reauth" }, []));
    const err = await api.changePassword("", "newpassword").catch((e: unknown) => e);
    expect(needsReauth(err)).toBe(true);
    const other = createAccountsApi("http://srv", fakeFetch(401, { error: "wrong" }, []));
    expect(needsReauth(await other.changePassword("a", "b").catch((e: unknown) => e))).toBe(false);
  });

  it("starts a fresh login", async () => {
    const calls: Call[] = [];
    const api = createAccountsApi("http://srv", fakeFetch(200, { url: "https://social.example/oauth/authorize" }, calls));
    expect(await api.fediReauth("social.example")).toBe("https://social.example/oauth/authorize");
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ instance: "social.example", purpose: "reauth" });
  });
});
