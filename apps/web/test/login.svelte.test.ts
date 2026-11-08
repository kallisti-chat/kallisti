// @vitest-environment jsdom
import { flushSync, mount, unmount, type ComponentProps } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import Login from "../src/components/Login.svelte";
import { createAccountsApi, type Account } from "../src/lib/accounts";
import { solveInWorker, type Challenge, type Proof } from "../src/lib/captcha";

vi.mock("../src/lib/captcha", () => ({ solveInWorker: vi.fn() }));

type Kind = "kallisti" | "mumble";
const mounted: ReturnType<typeof mount>[] = [];
afterEach(async () => {
  for (const component of mounted.splice(0)) await unmount(component);
  document.body.replaceChildren();
  document.title = "";
  localStorage.clear();
  sessionStorage.clear();
  vi.mocked(solveInWorker).mockReset();
});

// Let the complete async submit continuation run before asserting no request.
const settle = async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
  flushSync();
};
const invitationSelector = 'input[placeholder="Paste your invitation link"]';
const enterInvitation = (value: string) => {
  const el = document.querySelector<HTMLInputElement>(invitationSelector)!;
  el.value = value;
  el.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
};

const field = (autocomplete: string) => document.querySelector<HTMLInputElement>(`input[autocomplete="${autocomplete}"]`)!;
const input = (autocomplete: string, value: string) => {
  const el = field(autocomplete);
  el.value = value;
  el.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
};
const click = (text: string) => {
  [...document.querySelectorAll("button")].find((button) => button.textContent?.trim() === text)!.click();
  flushSync();
};
const submit = () => {
  document.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  flushSync();
};
const fakeApi = (server: string, info = {}) => {
  const fetcher = vi.fn(async (url: string, init: RequestInit) => {
    const path = new URL(url).pathname;
    let body: unknown = {};
    if (path === "/api/server") body = { password: true, name: server, ...info };
    if (path === "/api/login") body = { name: "alice" };
    if (path === "/api/me") body = { name: "alice", temporary: false };
    if (path === "/api/guest") return new Response("{}", { status: 404 });
    return new Response(JSON.stringify(body));
  });
  return { api: createAccountsApi(`https://${server}`, fetcher as typeof fetch), fetcher };
};
const show = (kind: Kind, first = fakeApi("a.example")) => {
  let api = $state.raw(first.api);
  let account = $state.raw<Account | null>(null);
  let desktop = $state<ComponentProps<typeof Login>["desktop"]>({
    server: "a.example", kind, bridge: true,
    onChangeServer: () => { desktop = { ...desktop!, kind: null }; },
    onServer: async (server) => {
      api = next.api;
      desktop = { ...desktop!, server, kind: nextKind };
    },
  });
  let next = first;
  let nextKind = kind;
  const onConnect = vi.fn();
  const onAccountChange = vi.fn((value: Account | null) => { account = value; });
  mounted.push(mount(Login, { target: document.body, props: {
    get api() { return api; }, get desktop() { return desktop; }, get account() { return account; },
    error: null, onConnect, onAccountChange,
  } }));
  flushSync();
  return {
    onConnect, onAccountChange,
    replaceApi: (nextApi: typeof api) => { api = nextApi; flushSync(); },
    setServer: (server: string, kind: Kind) => { desktop = { ...desktop!, server, kind }; flushSync(); },
    async choose(server: string, kind: Kind, chosen = fakeApi(server)) {
      next = chosen;
      nextKind = kind;
      click("Change");
      input("url", server);
      submit();
      await vi.waitFor(() => expect(field("username")).not.toBeNull());
      return chosen;
    },
    // App may replace the desktop props after an account change. It is not a server change.
    refresh: () => { desktop = { ...desktop! }; flushSync(); },
  };
};

const enterSecrets = async (kind: Kind) => {
  if (kind === "kallisti") {
    await vi.waitFor(() => expect(document.body.textContent).toContain("a.example"));
    click("Guest");
    await vi.waitFor(() => expect(field("off")).not.toBeNull());
    input("off", "A-server-secret");
    click("Log in");
  }
  input("username", "alice");
  input("current-password", "A-account-secret");
};

describe("desktop login server boundary", () => {
  it.each<[Kind, Kind, string]>([
    ["kallisti", "kallisti", "b.example"],
    ["kallisti", "kallisti", "a.example"],
    ["kallisti", "mumble", "b.example"],
    ["mumble", "kallisti", "b.example"],
    ["mumble", "mumble", "b.example"],
    ["mumble", "mumble", "a.example"],
  ])("clears secrets from %s to %s at %s without a saved login", async (from, to, server) => {
    localStorage.setItem("username", "remembered");
    const screen = show(from);
    expect(field("username").value).toBe("remembered");
    await enterSecrets(from);
    const chosen = await screen.choose(server, to);
    expect(field("username").value).toBe("alice");
    expect(localStorage.getItem("username")).toBe("remembered");
    expect(field("current-password").value).toBe("");
    if (to === "kallisti") {
      // Dispatch directly to check the submitted value as well as the DOM.
      submit();
      await vi.waitFor(() => expect(screen.onConnect).toHaveBeenCalledWith({ username: "alice", password: "", account: true, server }));
      expect(chosen.fetcher.mock.calls.find(([url]) => url.endsWith("/api/login"))?.[1].body).toBe(JSON.stringify({ name: "alice", password: "" }));
    } else {
      submit();
      expect(screen.onConnect).toHaveBeenCalledWith({ username: "alice", password: "", account: false, server });
    }
  });

  it.each(["b.example", "a.example"])("clears the hidden guest password before a guest joins %s", async (server) => {
    const screen = show("kallisti");
    await enterSecrets("kallisti");
    await screen.choose(server, "kallisti");
    click("Guest");
    await vi.waitFor(() => expect(field("off")).not.toBeNull());
    expect(field("off").value).toBe("");
    submit();
    await vi.waitFor(() => expect(screen.onConnect).toHaveBeenCalledWith({ username: "alice", password: "", account: false, server }));
  });

  it("does not restore hidden secrets after a plain-Mumble round trip", async () => {
    const screen = show("kallisti");
    await enterSecrets("kallisti");
    await screen.choose("b.example", "mumble");
    input("current-password", "B-mumble-secret");
    await screen.choose("a.example", "kallisti");
    expect(field("current-password").value).toBe("");
    click("Guest");
    await vi.waitFor(() => expect(field("off")).not.toBeNull());
    expect(field("off").value).toBe("");
  });

  it("resets signup mode, invitation policy and errors before new server info arrives", async () => {
    const first = fakeApi("a.example", { inviteOnly: true, logo: "logo-a" });
    vi.spyOn(first.api, "create").mockRejectedValue(new Error("A refused signup"));
    const screen = show("kallisti", first);
    await vi.waitFor(() => expect(document.body.textContent).toContain("This server is invite-only"));
    click("Create account");
    input("username", "alice");
    input("new-password", "A-account-secret");
    enterInvitation(`https://a.example/register#${"a".repeat(32)}`);
    submit();
    await vi.waitFor(() => expect(document.body.textContent).toContain("A refused signup"));
    const second = fakeApi("b.example");
    vi.spyOn(second.api, "serverInfo").mockReturnValue(new Promise(() => {}));
    await screen.choose("b.example", "kallisti", second);
    expect(document.querySelector('[aria-pressed="true"]')?.textContent?.trim()).toBe("Log in");
    expect(document.body.textContent).not.toContain("This server is invite-only");
    expect(document.body.textContent).not.toContain("A refused signup");
    expect(document.querySelector("img.logo")).toBeNull();
    click("Create account");
    expect(document.querySelector('input[placeholder="Paste your invitation link"]')).toBeNull();
    click("Guest");
    expect(field("off")).toBeNull();
  });

  it("does not let a pending login restore the previous server's error or busy state", async () => {
    const first = fakeApi("a.example");
    const oldLogin = Promise.withResolvers<{ name: string; mumble: boolean }>();
    vi.spyOn(first.api, "login").mockReturnValue(oldLogin.promise);
    const screen = show("kallisti", first);
    await enterSecrets("kallisti");
    submit();
    const second = fakeApi("b.example");
    const newLogin = Promise.withResolvers<{ name: string; mumble: boolean }>();
    vi.spyOn(second.api, "login").mockReturnValue(newLogin.promise);
    await screen.choose("b.example", "kallisti", second);
    expect(document.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(false);
    input("current-password", "B-account-secret");
    submit();
    oldLogin.reject(new Error("A login failed"));
    await oldLogin.promise.catch(() => {});
    flushSync();
    expect(document.body.textContent).not.toContain("A login failed");
    expect(document.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(true);
    expect(screen.onConnect).not.toHaveBeenCalled();
    newLogin.resolve({ name: "alice", mumble: false });
    await vi.waitFor(() => expect(screen.onConnect).toHaveBeenCalledWith({ username: "alice", password: "", account: true, server: "b.example" }));
  });

  it("does not send B's invitation to A after A's delayed signup challenge", async () => {
    const first = fakeApi("a.example", { inviteOnly: true });
    const challenge = Promise.withResolvers<Challenge | null>();
    vi.spyOn(first.api, "challenge").mockReturnValue(challenge.promise);
    const create = vi.spyOn(first.api, "create");
    const screen = show("kallisti", first);
    await vi.waitFor(() => expect(document.body.textContent).toContain("This server is invite-only"));
    click("Create account");
    input("username", "alice");
    input("new-password", "A-account-secret");
    enterInvitation(`https://a.example/register#${"a".repeat(32)}`);
    submit();
    expect(first.api.challenge).toHaveBeenCalledOnce();
    await screen.choose("b.example", "kallisti", fakeApi("b.example", { inviteOnly: true }));
    await vi.waitFor(() => expect(document.body.textContent).toContain("This server is invite-only"));
    click("Create account");
    input("new-password", "B-account-secret");
    enterInvitation(`https://b.example/register#${"b".repeat(32)}`);
    challenge.resolve(null);
    await settle();
    expect(create).not.toHaveBeenCalled();
    expect(first.fetcher.mock.calls.some(([url]) => url.endsWith("/api/accounts"))).toBe(false);
    expect(document.querySelector<HTMLInputElement>(invitationSelector)!.value).toBe(`https://b.example/register#${"b".repeat(32)}`);
    expect(field("new-password").value).toBe("B-account-secret");
    expect(screen.onConnect).not.toHaveBeenCalled();
  });

  it("captures signup secrets before proof and validates the invitation before requesting it", async () => {
    const first = fakeApi("a.example", { inviteOnly: true });
    const challenge = Promise.withResolvers<Challenge | null>();
    vi.spyOn(first.api, "challenge").mockReturnValue(challenge.promise);
    const create = vi.spyOn(first.api, "create");
    const screen = show("kallisti", first);
    await vi.waitFor(() => expect(document.body.textContent).toContain("This server is invite-only"));
    click("Create account");
    input("username", "alice");
    input("new-password", "A-account-secret");
    enterInvitation(`https://b.example/register#${"b".repeat(32)}`);
    submit();
    expect(document.body.textContent).toContain("Enter an account invitation for this server.");
    expect(first.api.challenge).not.toHaveBeenCalled();
    enterInvitation(`https://a.example/register#${"a".repeat(32)}`);
    submit();
    input("new-password", "edited-password");
    enterInvitation(`https://a.example/register#${"c".repeat(32)}`);
    challenge.resolve(null);
    await vi.waitFor(() => expect(screen.onConnect).toHaveBeenCalled());
    expect(create).toHaveBeenCalledExactlyOnceWith("alice", "A-account-secret", undefined, "a".repeat(32));
  });

  it.each(["api-only", "same-api address", "same-api kind"])("cancels a guest request after proof on an identity change (%s)", async (boundary) => {
    const first = fakeApi("a.example");
    const challenge = Promise.withResolvers<Challenge | null>();
    vi.spyOn(first.api, "challenge").mockReturnValue(challenge.promise);
    const guest = vi.spyOn(first.api, "guest");
    const screen = show("kallisti", first);
    await enterSecrets("kallisti");
    click("Guest");
    submit();
    if (boundary === "api-only") screen.replaceApi(fakeApi("a.example").api);
    else screen.setServer(boundary === "same-api address" ? "b.example" : "a.example", "mumble");
    expect(field("current-password").value).toBe("");
    if (boundary === "api-only") {
      click("Guest");
      await vi.waitFor(() => expect(field("off")).not.toBeNull());
      expect(field("off").value).toBe("");
    }
    challenge.resolve(null);
    await settle();
    expect(guest).not.toHaveBeenCalled();
    expect(screen.onConnect).not.toHaveBeenCalled();
  });

  it("clears a plain-Mumble password when the address changes but the API stays the same", async () => {
    const first = fakeApi("a.example");
    const screen = show("mumble", first);
    await enterSecrets("mumble");
    screen.setServer("b.example", "mumble");
    expect(field("username").value).toBe("alice");
    expect(field("current-password").value).toBe("");
    submit();
    expect(screen.onConnect).toHaveBeenCalledWith({ username: "alice", password: "", account: false, server: "b.example" });
  });

  it("does not start checking on B when A's challenge arrives late", async () => {
    const first = fakeApi("a.example");
    const challenge = Promise.withResolvers<Challenge | null>();
    vi.spyOn(first.api, "challenge").mockReturnValue(challenge.promise);
    vi.mocked(solveInWorker).mockReturnValue(new Promise(() => {}));
    const screen = show("kallisti", first);
    await enterSecrets("kallisti");
    click("Guest");
    submit();
    await screen.choose("b.example", "kallisti");
    challenge.resolve({ challenge: "A", bits: 1 });
    await settle();
    expect(document.body.textContent).not.toContain("Checking your browser");
    expect(solveInWorker).not.toHaveBeenCalled();
  });

  it("does not clear B's checking indicator when A's proof finishes", async () => {
    const first = fakeApi("a.example");
    vi.spyOn(first.api, "challenge").mockResolvedValue({ challenge: "A", bits: 1 });
    const oldProof = Promise.withResolvers<Proof>();
    const newProof = Promise.withResolvers<Proof>();
    vi.mocked(solveInWorker).mockReturnValueOnce(oldProof.promise).mockReturnValueOnce(newProof.promise);
    const screen = show("kallisti", first);
    await enterSecrets("kallisti");
    click("Guest");
    submit();
    await vi.waitFor(() => expect(solveInWorker).toHaveBeenCalledTimes(1));
    const second = fakeApi("b.example");
    vi.spyOn(second.api, "challenge").mockResolvedValue({ challenge: "B", bits: 1 });
    await screen.choose("b.example", "kallisti", second);
    click("Guest");
    submit();
    await vi.waitFor(() => expect(solveInWorker).toHaveBeenCalledTimes(2));
    expect(document.body.textContent).toContain("Checking your browser");
    oldProof.resolve({ challenge: "A", nonce: "1" });
    await settle();
    expect(document.body.textContent).toContain("Checking your browser");
    expect(document.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(true);
    newProof.resolve({ challenge: "B", nonce: "2" });
    await vi.waitFor(() => expect(screen.onConnect).toHaveBeenCalledWith({ username: "alice", password: "", account: false, server: "b.example" }));
    expect(document.body.textContent).not.toContain("Checking your browser");
  });

  it.each([false, true])("keeps successful login handling (Mumble registration: %s)", async (mumble) => {
    const first = fakeApi("a.example");
    vi.spyOn(first.api, "login").mockResolvedValue({ name: "alice", mumble });
    const screen = show("kallisti", first);
    await enterSecrets("kallisti");
    screen.refresh();
    expect(field("current-password").value).toBe("A-account-secret");
    submit();
    await vi.waitFor(() => expect(screen.onConnect).toHaveBeenCalledWith({
      username: "alice", password: mumble ? "A-account-secret" : "A-server-secret", account: !mumble, server: "a.example",
    }));
    expect(localStorage.getItem("username")).toBe("alice");
    expect(screen.onAccountChange).toHaveBeenCalledTimes(mumble ? 0 : 1);
    if (mumble) expect(field("current-password").value).toBe("");
  });
});
