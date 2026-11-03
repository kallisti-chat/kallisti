// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import Login from "../src/components/Login.svelte";
import RegistrationSettings from "../src/components/RegistrationSettings.svelte";
import { createAccountsApi } from "../src/lib/accounts";

const mounted: ReturnType<typeof mount>[] = [];
afterEach(async () => {
  for (const component of mounted.splice(0)) await unmount(component);
  document.body.replaceChildren();
  history.replaceState(null, "", "/");
  localStorage.clear();
  sessionStorage.clear();
});

const input = (selector: string, value: string) => {
  const el = document.querySelector<HTMLInputElement>(selector)!;
  el.value = value;
  el.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
};

const login = (closed: boolean | undefined, token?: string, federated = false, extra: Record<string, unknown> = {}) => {
  if (token) history.replaceState(null, "", `/register#${token}`);
  const requests: { path: string; body: Record<string, unknown> }[] = [];
  const fetcher = (async (url: string, init: RequestInit) => {
    const body = init.body ? JSON.parse(String(init.body)) : {};
    requests.push({ path: url, body });
    let result: unknown = {};
    if (url === "/api/server") result = { password: false, name: "Friends", ...(closed !== undefined && { inviteOnly: closed }) };
    if (url === "/api/fedi") result = { enabled: "keep" in extra };
    if (url === "/api/fedi/start") result = { url: "https://home.example/oauth" };
    if (url === "/api/fedi/signup") result = init.body ? { name: "alice" } : { name: "alice", handle: "@alice@home.example", remote: "alice@home.example" };
    if (url === "/api/accounts") result = { name: "alice" };
    if (url === "/api/me") result = { name: "alice", temporary: false };
    return new Response(JSON.stringify(result));
  }) as typeof fetch;
  const onConnect = vi.fn();
  mounted.push(mount(Login, { target: document.body, props: {
    api: createAccountsApi("", fetcher), error: null, account: null,
    onAccountChange: vi.fn(), onConnect, fediSignup: federated, onFediSignupDone: vi.fn(), ...extra,
  } }));
  flushSync();
  return { requests, onConnect };
};

describe("invite-only user interface", () => {
  it("keeps the guest option on open and older servers", async () => {
    login(undefined);
    await vi.waitFor(() => expect(document.body.textContent).toContain("Friends"));
    expect(document.querySelector('[aria-label="How to join"]')?.textContent).toContain("Guest");
  });

  it("hides guest access and submits the account invitation from its link", async () => {
    const token = "b".repeat(32);
    const { requests, onConnect } = login(true, token);
    await vi.waitFor(() => expect(document.body.textContent).toContain("This server is invite-only"));
    expect(document.querySelector('[aria-label="How to join"]')?.textContent).not.toContain("Guest");
    input('input[autocomplete="username"]', "alice");
    input('input[autocomplete="new-password"]', "longpassword");
    document.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(onConnect).toHaveBeenCalled());
    expect(requests.find((r) => r.path === "/api/accounts")?.body).toEqual({ name: "alice", password: "longpassword", invite: token });
    expect(location.hash).toBe("");
  });

  it("keeps a waiting channel link beside the invitation over a fediverse login", async () => {
    const token = "e".repeat(32);
    const { requests } = login(true, token, false, { keep: "/?join=5" });
    await vi.waitFor(() => expect(document.querySelector('input[aria-describedby="fedi-hint"]')).not.toBeNull());
    input('input[aria-describedby="fedi-hint"]', "home.example");
    document.querySelector('input[aria-describedby="fedi-hint"]')!.closest("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(requests.some((r) => r.path === "/api/fedi/start")).toBe(true));
    await vi.waitFor(() => expect(sessionStorage.getItem("kallisti-account-invite")).toBe(token));
    expect(sessionStorage.getItem("kallisti-invite")).toBe("/?join=5");
  });

  it("carries the kept invitation into first-time federated signup", async () => {
    const token = "c".repeat(32);
    sessionStorage.setItem("kallisti-account-invite", token);
    const { requests, onConnect } = login(true, undefined, true);
    await vi.waitFor(() => expect(onConnect).toHaveBeenCalled());
    expect(requests.find((r) => r.path === "/api/fedi/signup" && "invite" in r.body)?.body).toEqual({ name: "", invite: token });
    expect(sessionStorage.getItem("kallisti-account-invite")).toBeFalsy(); // once
  });

  it("creates single-use invitations by default and shows the returned link", async () => {
    const onCreate = vi.fn(async () => "https://chat.example/register#" + "d".repeat(32));
    const onSet = vi.fn(async () => {});
    mounted.push(mount(RegistrationSettings, { target: document.body, props: {
      on: false, invites: [], onSet, onLoad: async () => {}, onCreate, onDelete: async () => {},
    } }));
    flushSync();
    await vi.waitFor(() => expect([...document.querySelectorAll("button")].find((b) => b.textContent === "Create invitation")?.disabled).toBe(false));
    [...document.querySelectorAll("button")].find((b) => b.textContent === "Create invitation")!.click();
    await vi.waitFor(() => expect(document.querySelector<HTMLInputElement>("input[readonly]")?.value).toContain("/register#"));
    expect(onCreate).toHaveBeenCalledWith(604800, 1);
    const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    checkbox.click();
    await vi.waitFor(() => expect(onSet).toHaveBeenCalledWith(true));
  });

  it("restores the saved policy and shows an error when disabling is refused", async () => {
    let refuse!: (reason: Error) => void;
    const onSet = vi.fn(() => new Promise<void>((_, reject) => { refuse = reject; }));
    mounted.push(mount(RegistrationSettings, { target: document.body, props: {
      on: true, invites: [], onSet, onLoad: async () => {}, onCreate: async () => "", onDelete: async () => {},
    } }));
    flushSync();
    const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    checkbox.click();
    flushSync();
    expect(onSet).toHaveBeenCalledWith(false);
    expect(checkbox.disabled).toBe(true);
    refuse(new Error("Admin permission was revoked"));
    await vi.waitFor(() => {
      expect(document.body.textContent).toContain("Admin permission was revoked");
      expect(checkbox.checked).toBe(true);
      expect(checkbox.disabled).toBe(false);
    });
  });
});
