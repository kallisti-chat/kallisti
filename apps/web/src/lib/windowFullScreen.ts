// Full screen in the Linux app. WebKitGTK 2.50 aborts the app on HTML full
// screen, so the app turns it off (media.rs in the desktop app). Instead the
// window goes full screen and the element fills it from the top layer (a
// popover: no parent's overflow or transform clips it). Escape or a double
// click ends it.
type Win = { setFullscreen: (on: boolean) => Promise<void> };

export const windowFullScreen = (el: HTMLElement, win: () => Promise<Win>) => () => {
  const stop = new AbortController();
  const full = (on: boolean) => void win().then((w) => w.setFullscreen(on)).catch(() => {});
  const end = () => {
    stop.abort();
    el.hidePopover();
    el.classList.remove("window-fullscreen");
    el.removeAttribute("popover");
    full(false);
  };
  el.popover = "manual";
  el.classList.add("window-fullscreen");
  el.showPopover();
  full(true);
  document.addEventListener(
    "keydown",
    (e) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopPropagation();
      end();
    },
    { capture: true, signal: stop.signal },
  );
  el.addEventListener("dblclick", end, { signal: stop.signal });
};
