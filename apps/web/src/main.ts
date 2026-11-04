import { mount } from "svelte";
import App from "./App.svelte";
import { androidApp, currentWindow, isDesktop, openInBrowser, setVoiceService } from "./lib/desktop";
import { openLinksOutside } from "./lib/externalLinks";
import { watchKeyboard } from "./lib/keyboard";
import { watchWindowHidden } from "./lib/windowHidden";
import { i18n } from "./lib/i18n.svelte";
import "./app.css";

await i18n.init();
const app = mount(App, { target: document.getElementById("app")! });

// After mount: Svelte's own document listener (for elements outside the
// app's root) must see clicks first.
if (isDesktop()) openLinksOutside((url) => openInBrowser(url).catch((err) => console.error("link not opened", err)));

// Animated content stops painting while the window is hidden (app.css).
void (isDesktop() ? currentWindow() : Promise.resolve(null))
  .catch(() => null)
  .then((win) => watchWindowHidden((hidden) => document.documentElement.toggleAttribute("data-window-hidden", hidden), { doc: document, win }));

// The Android app: a new page (a reload) starts with no voice service; the
// voice it joins starts it again (see Client.svelte).
if (androidApp()) void setVoiceService(false, null);

if (window.visualViewport)
  watchKeyboard((open) => document.documentElement.toggleAttribute("data-keyboard", open), {
    viewport: window.visualViewport,
    layoutHeight: () => document.documentElement.clientHeight,
  });

// iOS lets the installed app pinch-zoom despite touch-action (app.css):
// its own gesture events stop it there.
const installed = matchMedia("(display-mode: standalone)").matches || (navigator as { standalone?: boolean }).standalone === true;
if (installed) document.addEventListener("gesturestart", (e) => e.preventDefault());

export default app;
