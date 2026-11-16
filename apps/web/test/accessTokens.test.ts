import { describe, expect, it } from "vitest";
import { createAccountsApi, memoryStore } from "../src/lib/accounts";

describe("access tokens", () => {
  it("come back as accessToken, never as this app's login session", async () => {
    const session = memoryStore("login-session");
    const fetchFn = (async () => new Response(JSON.stringify({ accessToken: "kallisti_pat_x", id: 1, name: "agent", write: true, created: "" }), { status: 201 })) as typeof fetch;
    const made = await createAccountsApi("https://chat.example", fetchFn, session).makeAccessToken("agent", true);
    expect(made.accessToken).toBe("kallisti_pat_x");
    expect(session.get()).toBe("login-session");
  });
});
