import { t } from "../i18n.svelte";

// Noise suppression: the browser's own filter, RNNoise (strong, in our
// capture worklet) or DeepFilterNet3 (strongest, a worklet before it).
export type NoiseMode = "off" | "browser" | "strong" | "strongest";

export const fallbackMode = (mode: NoiseMode): NoiseMode => (mode === "strongest" ? "strong" : mode === "off" ? "off" : "browser");

export const noiseLabel = (mode: NoiseMode) =>
  mode === "strongest" ? "DeepFilterNet3" : mode === "strong" ? "RNNoise" : mode === "off" ? t("client.noiseOff") : t("client.noiseBrowserFilter");
