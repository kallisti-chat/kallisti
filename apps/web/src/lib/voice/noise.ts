import { t } from "../i18n.svelte";

// Noise suppression: the browser's own filter, RNNoise (strong, in our
// capture worklet) or DeepFilterNet3 (strongest, a worklet before it).
export type NoiseMode = "browser" | "strong" | "strongest";

// When one fails or the computer is too slow, the next lighter one.
export const fallbackMode = (mode: NoiseMode): NoiseMode => (mode === "strongest" ? "strong" : "browser");

export const noiseLabel = (mode: NoiseMode) =>
  mode === "strongest" ? "DeepFilterNet3" : mode === "strong" ? "RNNoise" : t("client.noiseBrowserFilter");
