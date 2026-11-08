// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import Login from "../src/components/Login.svelte";
import { createAccountsApi } from "../src/lib/accounts";

const mounted: ReturnType<typeof mount>[] = [];
afterEach(async () => {
  for (const component of mounted.splice(0)) await unmount(component);
  document.body.replaceChildren();
  localStorage.clear();
  sessionStorage.clear();
});

const show = (status: number, why: string) => {
  const onConnect = vi.fn();
  const onAccountChange = vi.fn();
  const fetcher = vi.fn(async (url: string, init: RequestInit) => {
    if (url === "/api/fedi/signup") {
      if (!init.body) return new Response(JSON.stringify({ name: "suggested", handle: "@remote@home.example", remote: "remote@home.example", host: "home.example" }));
      const { name } = JSON.parse(String(init.body));
      return new Response(JSON.stringify(name || status === 201 ? { name: name || "remote@home.example" } : { error: why }), { status: name ? 201 : status });
    }
    return new Response("{}");
  });
  mounted.push(mount(Login, { target: document.body, props: {
    api: createAccountsApi("", fetcher as typeof fetch), error: null, account: null,
    fediSignup: true, onAccountChange, onConnect,
  } }));
  flushSync();
  return { fetcher, onConnect, onAccountChange };
};

describe("federation signup name selection", () => {
  it.each(["this name is reserved", "not a name of an account from another server", "this name is already taken"])("keeps the remote proof and offers a local name for %s", async (why) => {
    const { fetcher, onConnect, onAccountChange } = show(409, why);
    await vi.waitFor(() => expect(document.querySelector('input[autocomplete="username"]')).not.toBeNull());
    expect(onConnect).not.toHaveBeenCalled();
    const input = document.querySelector<HTMLInputElement>('input[autocomplete="username"]')!;
    input.value = "local-name";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    flushSync();
    document.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(onConnect).toHaveBeenCalledOnce());
    expect(onAccountChange).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "local-name", fedi: { handle: "@remote@home.example", public: false, host: "home.example" } }));
    const signups = fetcher.mock.calls.filter(([url]) => url === "/api/fedi/signup");
    expect(signups.filter(([, init]) => !init.body)).toHaveLength(1);
    expect(signups.filter(([, init]) => init.body).map(([, init]) => JSON.parse(String(init.body)).name)).toEqual(["", "local-name"]);
    expect(fetcher.mock.calls.some(([url]) => url === "/api/fedi/start")).toBe(false);
  });

  it("finishes an ordinary automatic signup without a name step", async () => {
    const { onConnect } = show(201, "");
    await vi.waitFor(() => expect(onConnect).toHaveBeenCalledOnce());
    expect(document.querySelector('input[autocomplete="username"]')).toBeNull();
  });

  it("does not turn an unrelated bad request into name selection", async () => {
    const { onConnect } = show(400, "invalid request");
    await vi.waitFor(() => expect(document.querySelector('[role="alert"]')?.textContent).toBe("invalid request"));
    expect(document.querySelector('input[autocomplete="username"]')).toBeNull();
    expect(onConnect).not.toHaveBeenCalled();
  });
});
