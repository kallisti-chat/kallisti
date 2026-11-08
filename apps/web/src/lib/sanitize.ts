import DOMPurify from "dompurify";
import { addEmojiImages } from "./emoji";
import { addCustomEmojiImages, type EmojiImages } from "./customEmoji";

// Mumble messages are HTML from other users. On top of the default
// (XSS-safe) config: links open in a new tab, so clicking one does not drop
// the connection, and only inline images are allowed, so nobody can track
// readers with remote images. Mumble sends images as data: URLs anyway.
// A link to a text channel (from markdown: href "#" and a numeric
// data-channel) stays in the tab; Mumble HTML could carry the attribute too.
export const isChannelLink = (a: Element) => a.getAttribute("href") === "#" && /^\d+$/.test(a.getAttribute("data-channel") ?? "");

// Nothing remote may load: the page may load pictures and media from other
// servers (several servers in one client), so its CSP no longer stops it.
// Styles that could load a resource go whole. Dynamic substitutions can
// conceal a disallowed size until rendering, so they go too.
const UNSAFE_STYLE = /\\|@|(url|image|image-set|cross-fade|src|var|env|attr|if)\(/i;
// Text formatting only: messages must not position, resize, or stack content
// over the app. Check parsed property names, not a list of dangerous CSS.
const TEXT_STYLES = new Set([
  "color", "background-color", "font-family", "font-weight", "font-style", "text-decoration",
  // Browsers expand the shorthand; jsdom can retain it as one property.
  "text-decoration-line", "text-decoration-style", "text-decoration-color", "text-decoration-thickness",
]);
// HTML only (no SVG, MathML: their attributes load resources too), and no
// attribute naming url() at all (SVG mask, fill, filter ... in any form).
const CONFIG = { USE_PROFILES: { html: true }, FORBID_TAGS: ["style"] };
const NAMES_URL = /url\(/i;
// Attributes that load nothing by what they say: text, and link addresses
// (DOMPurify checks those, a link loads only when clicked).
const TEXT_ATTRS = new Set(["href", "title", "alt", "aria-label"]);
const REMOTE_ATTRS = ["srcset", "poster", "background", "xlink:href", "lowsrc", "dynsrc", "longdesc"];

DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node.tagName === "A" && !isChannelLink(node)) {
    node.setAttribute("target", "_blank");
    node.setAttribute("rel", "noopener noreferrer");
  }
  for (const name of REMOTE_ATTRS) node.removeAttribute(name);
  if (node.tagName !== "A") node.removeAttribute("href");
  if (!(node.tagName === "IMG" && node.getAttribute("src")?.startsWith("data:image/"))) node.removeAttribute("src");
  const style = node.getAttribute("style");
  if (style) {
    const parsed = (node as HTMLElement).style;
    const thickness = parsed.getPropertyValue("text-decoration-thickness");
    const customThickness = thickness && !/^(auto|from-font|initial)$/.test(thickness);
    if (UNSAFE_STYLE.test(style) || Array.from(parsed).some((name) => !TEXT_STYLES.has(name)) || customThickness || !parsed.length) node.removeAttribute("style");
    // Do not keep raw declarations the CSS parser ignored.
    else node.setAttribute("style", parsed.cssText);
  }
  for (const { name, value } of [...node.attributes]) if (!TEXT_ATTRS.has(name) && NAMES_URL.test(value)) node.removeAttribute(name);
});

// emoji: emoji become Twemoji images, added after sanitizing (into text
// only), so nothing from the message can make or change them. The same for
// custom emoji (":name:" and "<:name:id>" -> image URL).
export const sanitize = (html: string, emoji = false, custom?: EmojiImages) => {
  if (!emoji && !custom) return DOMPurify.sanitize(html, CONFIG);
  const fragment = DOMPurify.sanitize(html, { ...CONFIG, RETURN_DOM_FRAGMENT: true });
  if (custom) addCustomEmojiImages(fragment, custom);
  if (emoji) addEmojiImages(fragment);
  const div = document.createElement("div");
  div.append(fragment);
  return div.innerHTML;
};
