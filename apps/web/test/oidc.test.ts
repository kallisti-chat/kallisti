import { describe, expect, it } from "vitest";
import { createAccountsApi, needsReauth } from "../src/lib/accounts";
import { oidcResult, withoutOidcResult } from "../src/lib/oidc";

const response = (body: unknown, status = 200) => (async () => new Response(JSON.stringify(body), { status })) as typeof fetch;

describe("OIDC callback", () => {
  it("recognizes signup, login and reauthentication without trusting arbitrary messages", () => {
    expect(oidcResult("?oidc=register")).toEqual({ kind: "register" });
    expect(oidcResult("?oidc=login")).toEqual({ kind: "login" });
    expect(oidcResult("?oidc=reauth")).toEqual({ kind: "reauth", text: "Confirmed: set your password now." });
    expect(oidcResult("?oidc=error&reason=%3Cscript%3E")).toEqual({ kind: "error", text: "The identity provider login did not work. Please try again." });
    expect(oidcResult("?oidc=error&reason=__proto__")?.kind).toBe("error");
    expect(oidcResult("?oidc=unknown")).toBeNull();
    expect(withoutOidcResult("/?join=abc&oidc=linked&reason=whatever#invite")).toBe("/?join=abc#invite");
  });
});

describe("OIDC account API", () => {
  it("defaults to disabled for old and unavailable servers", async () => {
    expect((await createAccountsApi("", response({}, 404)).oidcProvider()).enabled).toBe(false);
    expect((await createAccountsApi("", response({ enabled: "true" })).oidcProvider()).enabled).toBe(false);
    expect(await createAccountsApi("", response({ enabled: true, name: "Pocket ID" })).oidcProvider()).toEqual({ enabled: true, name: "Pocket ID" });
  });

  it("preserves the linked identity and password state", async () => {
    expect(await createAccountsApi("", response({ name: "alice", temporary: false, password: false, oidc: { provider: "Pocket ID" } })).me())
      .toMatchObject({ name: "alice", password: false, oidc: { provider: "Pocket ID" } });
    const api = createAccountsApi("", response({ error: "confirm first", code: "reauth" }, 403));
    expect(needsReauth(await api.changePassword("", "newpassword").catch((e: unknown) => e))).toBe(true);
  });

  it("separates login, explicit linking, reauth, signup proof, and password-confirmed unlink", async () => {
    const calls: { url: string; method: string | undefined; body: unknown }[] = [];
    const api = createAccountsApi("", (async (url: string, init: RequestInit) => {
      calls.push({ url, method: init.method, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify({ url: "https://id.example/authorize", name: "alice" }));
    }) as typeof fetch);
    await api.oidcStart();
    await api.oidcStart("link", "current");
    await api.oidcStart("reauth");
    await api.oidcCreate("alice", { challenge: "proof", nonce: "answer" }, "invitation");
    await api.oidcUnlink("current");
    expect(calls).toEqual([
      { url: "/api/oidc/start", method: "POST", body: { mode: "login" } },
      { url: "/api/oidc/start", method: "POST", body: { mode: "link", password: "current" } },
      { url: "/api/oidc/start", method: "POST", body: { mode: "reauth" } },
      { url: "/api/oidc/signup", method: "POST", body: { name: "alice", challenge: "proof", nonce: "answer", invite: "invitation" } },
      { url: "/api/oidc/link", method: "DELETE", body: { password: "current" } },
    ]);
  });
});
