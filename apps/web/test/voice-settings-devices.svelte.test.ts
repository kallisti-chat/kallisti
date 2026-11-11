// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import SettingsDialog from "../src/components/SettingsDialog.svelte";
import { DEFAULT_SETTINGS } from "../src/lib/settings";
import { t } from "../src/lib/i18n.svelte";

const mounted: ReturnType<typeof mount>[] = [];
const flush = async () => { flushSync(); await Promise.resolve(); await Promise.resolve(); flushSync(); };
const device = (kind: MediaDeviceKind, deviceId: string, label = deviceId): MediaDeviceInfo => ({
  kind, deviceId, label, groupId: label, toJSON() { return {}; },
});
const show = (list: MediaDeviceInfo[]) => {
  const events = new EventTarget();
  const media = {
    enumerateDevices: vi.fn(async () => list),
    addEventListener: events.addEventListener.bind(events),
    removeEventListener: events.removeEventListener.bind(events),
    getUserMedia: vi.fn(() => { throw new Error("No real microphone in tests"); }),
  };
  Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: media });
  const target = document.createElement("div"); document.body.append(target);
  const onChange = vi.fn();
  mounted.push(mount(SettingsDialog, { target, props: {
    settings: DEFAULT_SETTINGS, level: 0, start: "appearance", onChange, onClose() {},
  } }));
  return {
    target, onChange, media,
    open: (section: string) => target.querySelector<HTMLButtonElement>(`[data-category="${section}"]`)!.click(),
    change: (next: MediaDeviceInfo[]) => { list = next; events.dispatchEvent(new Event("devicechange")); },
    field: (label: string) => [...target.querySelectorAll("label")].find((el) => el.textContent?.includes(label))!.querySelector("select")!,
  };
};
const values = (field: HTMLSelectElement) => [...field.options].map((o) => o.value);
beforeEach(() => {
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("AudioContext", class { setSinkId() {} });
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
});
afterEach(async () => {
  for (const component of mounted.splice(0)) await unmount(component);
  document.body.replaceChildren();
  Reflect.deleteProperty(navigator, "mediaDevices");
  vi.unstubAllGlobals();
});

it.each(["audioinput", "audiooutput", "videoinput"] as const)("normalizes %s options before keyed rendering", async (kind) => {
  const f = show([
    device(kind, ""), device(kind, ""), device(kind, "default"), device(kind, "default"),
    device(kind, "named", "First label"), device(kind, "named", "Duplicate label"), device(kind, "second"),
  ]);
  await flush(); f.open("voice"); await flush();
  const label = kind === "audioinput" ? "settings.inputDevice" : kind === "audiooutput" ? "settings.outputDevice" : "settings.camera";
  const field = f.field(t(label));
  expect(values(field)).toEqual(["", "named", "second"]);
  expect(field.options[1]!.textContent).toBe("First label");
  expect(f.media.getUserMedia).not.toHaveBeenCalled();
});

it("keeps the same ID in different device kinds and preserves selection and default choices", async () => {
  const f = show([device("audioinput", "shared"), device("audiooutput", "shared"), device("videoinput", "shared")]);
  await flush(); f.open("voice"); await flush();
  for (const [label, key] of [["settings.inputDevice", "inputDeviceId"], ["settings.outputDevice", "outputDeviceId"], ["settings.camera", "cameraDeviceId"]] as const) {
    const field = f.field(t(label));
    expect(values(field)).toEqual(["", "shared"]);
    field.value = "shared"; field.dispatchEvent(new Event("change", { bubbles: true }));
    expect(f.onChange).toHaveBeenLastCalledWith({ [key]: "shared" });
    field.value = ""; field.dispatchEvent(new Event("change", { bubbles: true }));
    expect(f.onChange).toHaveBeenLastCalledWith({ [key]: null });
  }
});

it("reopens the voice category after devicechange introduces duplicate and redacted IDs", async () => {
  const f = show([device("audioinput", "first")]);
  await flush(); f.open("voice"); await flush();
  expect(values(f.field(t("settings.inputDevice")))).toEqual(["", "first"]);
  f.open("appearance"); await flush();
  f.change([device("audioinput", ""), device("audioinput", ""), device("audioinput", "next"), device("audioinput", "next")]);
  await flush(); f.open("voice"); await flush();
  expect(values(f.field(t("settings.inputDevice")))).toEqual(["", "next"]);
  f.open("appearance"); await flush(); f.open("voice"); await flush();
  expect(values(f.field(t("settings.inputDevice")))).toEqual(["", "next"]);
});

it("keeps the camera permission hint and default when camera IDs are hidden", async () => {
  const f = show([device("videoinput", ""), device("videoinput", "")]);
  await flush(); f.open("voice"); await flush();
  expect(values(f.field(t("settings.camera")))).toEqual([""]);
  expect(f.target.textContent).toContain(t("settings.cameraListHint"));
});
