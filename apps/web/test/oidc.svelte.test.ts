// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import Login from "../src/components/Login.svelte";
import OidcAccount from "../src/components/OidcAccount.svelte";
import { createAccountsApi, type AccountActions } from "../src/lib/accounts";
import { keepOidcInvite } from "../src/lib/oidc";
import { solveInWorker } from "../src/lib/captcha";

vi.mock("../src/lib/captcha", () => ({ solveInWorker: vi.fn(async () => ({ challenge: "challenge", nonce: "proof" })) }));
const mounted: ReturnType<typeof mount>[] = [];
afterEach(async () => {
  for (const component of mounted.splice(0)) await unmount(component);
  document.body.replaceChildren();
  localStorage.clear();
  sessionStorage.clear();
  vi.clearAllMocks();
});
const submit = () => {
  document.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  flushSync();
};

describe("OIDC login screen", () => {
  const show = (enabled: boolean, signup = false, desktop = false) => {
    const onConnect = vi.fn();
    const onAccountChange = vi.fn();
    const fetcher = vi.fn(async (url: string, init: RequestInit) => {
      if (url === "/api/oidc") return new Response(JSON.stringify({ enabled, name: "Pocket ID" }));
      if (url === "/api/oidc/signup") return new Response(JSON.stringify({ name: "alice", provider: "Pocket ID" }));
      if (url === "/api/challenge") return new Response(JSON.stringify({ challenge: "challenge", bits: 1 }));
      if (url === "/api/me") return new Response(JSON.stringify({ name: "alice", temporary: false, password: false, oidc: { provider: "Pocket ID" } }));
      return new Response("{}");
    });
    mounted.push(mount(Login, { target: document.body, props: {
      api: createAccountsApi("", fetcher as typeof fetch), account: null, error: null,
      oidcSignup: signup, onConnect, onAccountChange,
      ...(desktop && { desktop: { server: "chat.example", kind: "kallisti" as const, bridge: false, onServer: async () => {}, onChangeServer: () => {} } }),
    } }));
    flushSync();
    return { fetcher, onConnect, onAccountChange };
  };

  it("shows the provider only when configured", async () => {
    show(true);
    await vi.waitFor(() => expect(document.body.textContent).toContain("Continue with Pocket ID"));
  });

  it.each([false, true])("keeps the provider hidden when disabled or on desktop (%s)", async (desktop) => {
    const { fetcher } = show(desktop, false, desktop);
    await vi.waitFor(() => expect(fetcher.mock.calls.some(([url]) => url === "/api/server")).toBe(true));
    expect(document.body.textContent).not.toContain("Continue with Pocket ID");
    if (desktop) expect(fetcher.mock.calls.some(([url]) => url === "/api/oidc")).toBe(false);
  });

  it("retains account invitations across the provider redirect and requires explicit name confirmation", async () => {
    const invitation = "a".repeat(32);
    keepOidcInvite(invitation, "/?join=channel");
    expect(sessionStorage.getItem("kallisti-invite")).toBe("/?join=channel");
    const { fetcher, onConnect, onAccountChange } = show(true, true);
    await vi.waitFor(() => expect(document.querySelector<HTMLInputElement>('input[autocomplete="username"]')?.value).toBe("alice"));
    expect(onConnect).not.toHaveBeenCalled();
    expect(sessionStorage.getItem("kallisti-account-invite")).toBe("");
    submit();
    await vi.waitFor(() => expect(onConnect).toHaveBeenCalledOnce());
    expect(solveInWorker).toHaveBeenCalledOnce();
    const signup = fetcher.mock.calls.find(([url, init]) => url === "/api/oidc/signup" && init.body);
    expect(JSON.parse(String(signup?.[1].body))).toEqual({ name: "alice", invite: invitation, challenge: "challenge", nonce: "proof" });
    expect(onAccountChange).toHaveBeenCalledWith(expect.objectContaining({ name: "alice", password: false, oidc: { provider: "Pocket ID" } }));
  });
});

describe("OIDC settings", () => {
  const show = (linked: boolean, hasPassword: boolean, enabled = true) => {
    const link = vi.fn(async () => {});
    const unlink = vi.fn(async () => {});
    const account = { temporary: false, hasPassword, oidc: {
      linked: linked ? { provider: "Pocket ID", enabled } : null,
      provider: async () => ({ enabled, name: "Pocket ID" }), link, unlink, reauth: vi.fn(),
    } } as unknown as AccountActions;
    mounted.push(mount(OidcAccount, { target: document.body, props: { account } }));
    flushSync();
    return { link, unlink };
  };

  it.each([false, true])("requires password confirmation to change the link (%s)", async (linked) => {
    const { link, unlink } = show(linked, true);
    await vi.waitFor(() => expect(document.querySelector('input[type="password"]')).not.toBeNull());
    const field = document.querySelector<HTMLInputElement>('input[type="password"]')!;
    expect(field.required).toBe(true);
    field.value = "current";
    field.dispatchEvent(new Event("input", { bubbles: true }));
    flushSync();
    submit();
    await vi.waitFor(() => expect(linked ? unlink : link).toHaveBeenCalledWith("current"));
    expect(linked ? link : unlink).not.toHaveBeenCalled();
  });

  it("does not offer unlinking when no local password exists", async () => {
    show(true, false);
    await vi.waitFor(() => expect(document.body.textContent).toContain("Set a password before"));
    expect(document.querySelector("button")).toBeNull();
  });

  it("explains a removed provider while retaining password-confirmed unlink", async () => {
    const { unlink } = show(true, true, false);
    await vi.waitFor(() => expect(document.body.textContent).toContain("This login provider is unavailable"));
    const field = document.querySelector<HTMLInputElement>('input[type="password"]')!;
    field.value = "current";
    field.dispatchEvent(new Event("input", { bubbles: true }));
    flushSync();
    submit();
    await vi.waitFor(() => expect(unlink).toHaveBeenCalledWith("current"));
  });
});
