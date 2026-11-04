import { isDesktop } from "./desktop";
import { t } from "./i18n.svelte";

// The phone apps scan login codes (see lib/qrLogin) with their camera
// (tauri-plugin-barcode-scanner); browsers use the phone's camera app,
// which opens the code's link.
export const canScan = () => isDesktop() && /Android|iPhone|iPad/.test(navigator.userAgent);

// The scanned text; null: cancelled.
export const scanCode = async (): Promise<string | null> => {
  const { scan, checkPermissions, requestPermissions, Format } = await import("@tauri-apps/plugin-barcode-scanner");
  let allowed = await checkPermissions();
  if (allowed !== "granted") allowed = await requestPermissions();
  if (allowed !== "granted") throw new Error(t("login.cameraDenied"));
  // A scan ends with a code, or rejects when the user leaves it.
  return scan({ windowed: false, formats: [Format.QRCode] }).then(
    (r) => r.content,
    () => null,
  );
};
