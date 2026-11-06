// A short label over a button while the pointer is on it or it has the
// keyboard's focus (as Discord's): {@attach tip(label)}. One label for the
// page; the button keeps its aria-label (the tip is not read twice).

type Box = { left: number; top: number; width: number; height: number };

const GAP = 8;
const EDGE = 4;

// Centred above the element, else below; inside the window.
export const tipPlace = (target: Box, tip: { width: number; height: number }, view: { width: number; height: number }) => {
  const below = target.top - GAP - tip.height < EDGE;
  const x = Math.min(Math.max(EDGE, target.left + target.width / 2 - tip.width / 2), view.width - EDGE - tip.width);
  const y = below ? target.top + target.height + GAP : target.top - GAP - tip.height;
  return { x, y, below };
};

let el: HTMLDivElement | null = null;
let owner: HTMLElement | null = null;

const show = (node: HTMLElement, label: string) => {
  el ??= Object.assign(document.createElement("div"), { className: "tooltip" });
  el.setAttribute("aria-hidden", "true");
  el.textContent = label;
  document.body.append(el);
  const place = tipPlace(node.getBoundingClientRect(), el.getBoundingClientRect(), { width: innerWidth, height: innerHeight });
  el.style.left = `${place.x}px`;
  el.style.top = `${place.y}px`;
  el.classList.toggle("below", place.below);
  // The arrow points at the element's middle.
  el.style.setProperty("--arrow", `${node.getBoundingClientRect().left + node.offsetWidth / 2 - place.x}px`);
  owner = node;
};

const hide = (node: HTMLElement) => {
  if (owner !== node) return;
  el?.remove();
  owner = null;
};

export const tip = (label: string) => (node: HTMLElement) => {
  const on = () => show(node, label);
  const off = () => hide(node);
  const onFocus = () => node.matches(":focus-visible") && on();
  node.addEventListener("pointerenter", on);
  node.addEventListener("pointerleave", off);
  node.addEventListener("focus", onFocus);
  node.addEventListener("blur", off);
  node.addEventListener("click", off);
  return () => {
    off();
    node.removeEventListener("pointerenter", on);
    node.removeEventListener("pointerleave", off);
    node.removeEventListener("focus", onFocus);
    node.removeEventListener("blur", off);
    node.removeEventListener("click", off);
  };
};
