import { t } from "../i18n.svelte";

// Browser features that voice needs (a worklet for capture, WebAssembly
// for Opus where WebCodecs lacks it: see codec.ts), by the names that are
// missing.
export const missingVoiceApis = (g: object = globalThis): string[] =>
  ["AudioWorkletNode", "WebAssembly"].filter((name) => typeof (g as Record<string, unknown>)[name] === "undefined");

export const voiceSupported = () => missingVoiceApis().length === 0;

export const unsupportedVoiceMessage = (missing: string[]): string | null =>
  missing.length
    ? t("client.voiceUnsupported", { apis: missing.join(", ") })
    : null;

// Choosing the output device: Chrome and Edge only.
export const outputDeviceSupported = () =>
  typeof AudioContext !== "undefined" && "setSinkId" in AudioContext.prototype;
