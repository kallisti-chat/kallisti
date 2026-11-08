// @vitest-environment jsdom
import { flushSync } from "svelte";
import { describe, expect, it, vi } from "vitest";
import { createServerLinks } from "../src/lib/serverLinks.svelte";

const HOME = "https://home.example";
const B = "https://b.example";
const C = "https://c.example";
const TOKEN = "abcdefghij0123456789xyz";

const setup = () => {
  let owner = $state<string | null>(`${HOME} alice`);
  const servers = $state([{ origin: B, name: "al", session: {} }, { origin: C, name: "al", session: null as unknown }]);
  const worker = new EventTarget();
  const shown: string[] = [];
  const adds: unknown[] = [];
  const links = createServerLinks({
    worker,
    servers: () => servers,
    home: () => HOME,
    owner: () => owner,
    select: (id) => void shown.push(id),
    add: (target) => void adds.push(target),
  });
  flushSync();
  return { links, shown, adds, servers, worker, login: (o: string | null) => (owner = o) };
};

describe("links from elsewhere", () => {
  it("an invite: home, a server here, its login first, or added first", () => {
    const { links, shown, adds } = setup();
    expect(links.openInvite(`${HOME}/invite/${TOKEN}`)).toBe(true);
    expect(links.openInvite(`${B}/invite/${TOKEN}`)).toBe(true);
    expect(links.openInvite(`${C}/invite/${TOKEN}`)).toBe(true);
    expect(links.openInvite(`https://new.example/invite/${TOKEN}`)).toBe(true);
    expect(shown).toEqual(["", B, C]);
    expect(adds).toEqual([{ server: { origin: C, name: "al" } }, { address: "https://new.example" }]);
    expect([links.linkFor(""), links.linkFor(B)]).toEqual([`${HOME}/invite/${TOKEN}`, `${B}/invite/${TOKEN}`]);
    links.used(B);
    expect(links.linkFor(B)).toBeNull();
    expect(links.openInvite("https://x.example/not-an-invite")).toBe(false);
    links.dispose();
  });

  it("a guest adds no servers: the link opens as a page", () => {
    const { links, login } = setup();
    login(null);
    flushSync();
    expect(links.openInvite(`https://new.example/invite/${TOKEN}`)).toBe(false);
    links.dispose();
  });

  it("a message link (a push notice) opens on a server here, nothing else", () => {
    const { links, shown } = setup();
    expect(links.openMessageLink(`${B}/channels/1/2`)).toBe(true);
    expect(links.openMessageLink(`${B}/invite/${TOKEN}`)).toBe(false);
    expect(links.openMessageLink(`${C}/channels/1/2`)).toBe(false); // no session here
    expect(shown).toEqual([B]);
    expect(links.linkFor(B)).toBe(`${B}/channels/1/2`);
    // A voice channel link too (it asks there before joining).
    expect(links.openMessageLink(`${B}/voice/3`)).toBe(true);
    expect(links.linkFor(B)).toBe(`${B}/voice/3`);
    links.dispose();
  });

  it("another home login: none of the last one's links", () => {
    const { links, login } = setup();
    links.openInvite(`${B}/invite/${TOKEN}`);
    login(`${HOME} bob`);
    flushSync();
    expect(links.linkFor(B)).toBeNull();
    links.dispose();
  });
});

// The App owns ingress even when no Client exists. These fake Clients only
// model the link prop's readiness gate; no media or network APIs are needed.
describe("worker click ingress", () => {
  const click = (worker: EventTarget, data: unknown) => worker.dispatchEvent(new MessageEvent("message", { data }));
  const mount = (links: ReturnType<typeof createServerLinks>, id: string, opened: string[]) => {
    let ready = $state(false);
    const dispose = $effect.root(() => {
      $effect(() => {
        const url = links.linkFor(id);
        if (!ready || !url) return;
        opened.push(url);
        links.used(id);
      });
    });
    return { ready: () => (ready = true), dispose };
  };

  it.each(["", B])("keeps a click before Client mount and readiness, once for %s", (id) => {
    const { links, worker } = setup();
    const url = `${id || HOME}/channels/1/2`;
    click(worker, { type: "open", url });
    flushSync();
    expect(links.linkFor(id)).toBe(url);
    const opened: string[] = [];
    const other = mount(links, id === B ? "" : B, opened);
    const target = mount(links, id, opened);
    other.ready();
    flushSync();
    expect(opened).toEqual([]);
    target.ready();
    flushSync();
    flushSync();
    expect(opened).toEqual([url]);
    expect(links.linkFor(id)).toBeNull();
    other.dispose();
    target.dispose();
    links.dispose();
  });

  it("rejects malformed and foreign targets without replacing a valid click", () => {
    const { links, worker, shown } = setup();
    const url = `${HOME}/channels/1/2`;
    click(worker, { type: "open", url });
    for (const data of [null, {}, "open", { type: "other", url }, { type: "open", url: 1 },
      ...["garbage", "/channels/1/2", `${HOME}/bad`, `${C}/channels/1/2`, "https://foreign.example/channels/1/2"].map((url) => ({ type: "open", url }))]) {
      click(worker, data);
    }
    expect(shown).toEqual([""]);
    expect(links.linkFor("")).toBe(url);
    links.dispose();
  });

  it("retains only the latest validated message, bound to its server", () => {
    const { links, worker } = setup();
    click(worker, { type: "open", url: `${HOME}/channels/1/2` });
    click(worker, { type: "open", url: `${B}/channels/3/4` });
    expect(links.linkFor("")).toBeNull();
    expect(links.linkFor(B)).toBe(`${B}/channels/3/4`);
    links.dispose();
  });

  it("clears on owner change even before effects flush", () => {
    const { links, worker, login } = setup();
    click(worker, { type: "open", url: `${B}/channels/1/2` });
    expect(links.linkFor(B)).toBe(`${B}/channels/1/2`);
    login(`${HOME} bob`);
    expect(links.linkFor(B)).toBeNull();
    flushSync();
    expect(links.linkFor(B)).toBeNull();
    links.dispose();
  });

  it("removes its one listener and drops an unconsumed click on disposal", () => {
    const { links, worker, shown } = setup();
    const remove = vi.spyOn(worker, "removeEventListener");
    click(worker, { type: "open", url: `${HOME}/channels/1/2` });
    expect(links.linkFor("")).not.toBeNull();
    links.dispose();
    expect(remove).toHaveBeenCalledWith("message", expect.any(Function));
    expect(links.linkFor("")).toBeNull();
    click(worker, { type: "open", url: `${B}/channels/1/2` });
    expect(shown).toEqual([""]);
  });

  it("replaces a server's invite with a message or the reverse, without replay", () => {
    const { links, worker } = setup();
    const invite = `${B}/invite/${TOKEN}`;
    const url = `${B}/channels/1/2`;
    links.openInvite(invite);
    click(worker, { type: "open", url });
    expect(links.linkFor(B)).toBe(url);
    links.used(B);
    expect(links.linkFor(B)).toBeNull();
    click(worker, { type: "open", url });
    links.openInvite(invite);
    expect(links.linkFor(B)).toBe(invite);
    links.used(B);
    expect(links.linkFor(B)).toBeNull();
    links.dispose();
  });

  it("does not deliver to a removed server or retain a disposed listener", () => {
    const { links, worker, servers, shown } = setup();
    click(worker, { type: "open", url: `${B}/channels/1/2` });
    servers.splice(0, 1);
    expect(links.linkFor(B)).toBeNull();
    links.dispose();
    expect(links.linkFor("")).toBeNull();
    click(worker, { type: "open", url: `${HOME}/channels/3/4` });
    expect(shown).toEqual([B]);
    expect(links.linkFor("")).toBeNull();
  });
});
