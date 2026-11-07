// @vitest-environment jsdom
import { initialState, joinChannel, type ServerState } from "@kallisti/protocol";
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Client from "../src/components/Client.svelte";
import type { Connection } from "../src/lib/connection.svelte";
import { initialTextState } from "../src/lib/textChannels";
import { createContextDemand } from "../src/lib/voice/contextDemand";
import { t } from "../src/lib/i18n.svelte";

const flush = async () => { flushSync(); for (let i = 0; i < 12; i++) await Promise.resolve(); };
const mounted: ReturnType<typeof mount>[] = [];
const ctx = () => {
  const audio = {
    state: "running",
    suspend: vi.fn(async () => { audio.state = "suspended"; }),
    resume: vi.fn(async () => { audio.state = "running"; }),
    addEventListener: vi.fn(), removeEventListener: vi.fn(),
  };
  return audio;
};
const server = (channel = 0, streams: Connection["streams"] = null) => {
  let state = $state.raw<ServerState>({
    ...initialState, status: "synced", session: 1, rootPermissions: 0,
    channels: {
      0: { id: 0, name: "Root", parent: null, description: "", descriptionHash: null, position: 0, temporary: false, links: [] },
      5: { id: 5, name: "General", parent: 0, description: "", descriptionHash: null, position: 0, temporary: false, links: [] },
    } as ServerState["channels"],
    users: { 1: { session: 1, name: "me", channelId: channel, userId: 1, comment: "", selfMute: true, selfDeaf: true } } as unknown as ServerState["users"],
  });
  const noop = vi.fn();
  const connection = {
    get state() { return state; }, text: initialTextState, httpBase: "https://example.test", account: false,
    capture: null, playback: null, audio: null, streams, closed: false,
    info: {}, send: vi.fn(), onTextEvent: () => () => {}, fetchComment: noop, fetchTextures: noop, fetchDescriptions: noop,
  };
  return {
    connection: connection as unknown as Connection,
    move: (channelId: number) => { state = { ...state, users: { 1: { ...state.users[1]!, channelId } } }; },
  };
};
const show = (s: ReturnType<typeof server>, demand: ReturnType<typeof createContextDemand>, extra = {}) => {
  const target = document.createElement("div");
  document.body.append(target);
  const component = mount(Client, { target, props: { connection: s.connection, audioDemand: demand, ...extra } });
  mounted.push(component);
  flushSync();
  return { target, component };
};
beforeEach(() => {
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} });
  Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: {
    enumerateDevices: vi.fn(async () => []), addEventListener() {}, removeEventListener() {},
    getUserMedia: vi.fn(() => { throw new Error("No real microphone in tests"); }),
  } });
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(async () => {
  for (const c of mounted.splice(0)) await unmount(c);
  document.body.replaceChildren();
  vi.useRealTimers();
  Reflect.deleteProperty(navigator, "mediaDevices");
  vi.unstubAllGlobals();
});

describe("Client shared context demand", () => {
  it("does not let a mounted idle Client stop another Client's voice", async () => {
    const demand = createContextDemand(), audio = ctx();
    demand.setContext(audio);
    const active = server(5), idle = server();
    show(active, demand);
    show(idle, demand);
    await flush();
    expect(audio.suspend).not.toHaveBeenCalled();
    active.move(0);
    await flush();
    expect(audio.suspend).toHaveBeenCalledOnce();
  });

  it("resumes in the settings click, before effects, and releases on close", async () => {
    const demand = createContextDemand(), audio = ctx();
    demand.setContext(audio);
    const { target } = show(server(), demand);
    await flush();
    target.querySelector<HTMLButtonElement>(`button[title="${t("common.settings")}"]`)!.click();
    expect(audio.resume).toHaveBeenCalledOnce(); // no flush yet
    await flush();
    expect(audio.suspend).toHaveBeenCalledOnce(); // initial idle only
    target.querySelector<HTMLDialogElement>("dialog")!.dispatchEvent(new Event("close"));
    await flush();
    expect(audio.suspend).toHaveBeenCalledTimes(2);
  });

  it("holds a pending join before restart and confirmation; a refused join expires", async () => {
    vi.useFakeTimers();
    const demand = createContextDemand(), audio = ctx(), next = ctx();
    next.state = "suspended";
    demand.setContext(audio);
    const s = server();
    const restart = vi.fn(() => { demand.setContext(next); expect(next.resume).toHaveBeenCalledOnce(); });
    const { target } = show(s, demand, { onRestartAudio: restart });
    await flush();
    const channel = [...target.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === "General")!;
    channel.click();
    expect(audio.resume).toHaveBeenCalledOnce();
    expect(restart).toHaveBeenCalledOnce();
    expect(s.connection.send).toHaveBeenCalledWith(joinChannel(1, 5));
    await flush();
    expect(next.suspend).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(5000);
    await flush();
    expect(next.suspend).toHaveBeenCalledOnce();
  });

  it("passes pending join demand to confirmed voice without a suspension", async () => {
    const demand = createContextDemand(), audio = ctx(), s = server();
    demand.setContext(audio);
    const { target } = show(s, demand);
    await flush();
    [...target.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === "General")!.click();
    await flush();
    s.move(5);
    await flush();
    expect(audio.suspend).toHaveBeenCalledOnce(); // initial idle only
    s.move(0);
    await flush();
    expect(audio.suspend).toHaveBeenCalledTimes(2);
  });

  it("holds rejoining demand until confirmation, even before voiceOn", async () => {
    const demand = createContextDemand(), audio = ctx(), s = server();
    demand.setContext(audio);
    show(s, demand, { resume: { session: 1, voice: 5, view: { kind: "voice" } } });
    await flush();
    expect(s.connection.send).toHaveBeenCalledWith(joinChannel(1, 5));
    expect(audio.suspend).not.toHaveBeenCalled();
    s.move(5);
    await flush();
    expect(audio.suspend).not.toHaveBeenCalled();
  });

  it("coalesces an actual Client unmount and replacement mount", async () => {
    const demand = createContextDemand(), audio = ctx();
    demand.setContext(audio);
    const a = show(server(5), demand);
    await flush();
    mounted.splice(mounted.indexOf(a.component), 1);
    const removed = unmount(a.component);
    const replacement = show(server(5), demand);
    await removed;
    await flush();
    expect(audio.suspend).not.toHaveBeenCalled();
    mounted.splice(mounted.indexOf(replacement.component), 1);
    await unmount(replacement.component);
    await flush();
    expect(audio.suspend).toHaveBeenCalledOnce();
  });

  it("keeps a watched stream's audio clock at zero volume while deafened", async () => {
    const demand = createContextDemand(), audio = ctx();
    let volume = $state<number | undefined>(0);
    const noop = vi.fn();
    const streams = {
      watching: [7], volumeOf: () => volume, sharing: { screen: false, camera: false },
      preview: { screen: null, camera: null }, gone: false,
      setPreviews: noop, setCameraDevice: noop, setQuality: noop, setDeaf: noop,
      unwatch: noop, watch: noop, attach: noop, statsOf: () => null,
    } as unknown as NonNullable<Connection["streams"]>;
    demand.setContext(audio);
    show(server(0, streams), demand); // no voice; selfDeaf is true
    await flush();
    expect(streams.setDeaf).toHaveBeenCalledWith(true);
    expect(audio.suspend).not.toHaveBeenCalled();
    volume = undefined;
    await flush();
    expect(audio.suspend).toHaveBeenCalledOnce();
    volume = 0;
    await flush();
    expect(audio.resume).toHaveBeenCalledOnce();
  });

});
