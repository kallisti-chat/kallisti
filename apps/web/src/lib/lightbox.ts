import type { ChatItem, MessageGroup } from "./chat";
import { splitFiles, step } from "./fileGrid";

// Images open in the lightbox on a plain left click; middle and modified
// clicks (new tab, new window, download) stay with the browser.
export const opensLightbox = (e: Pick<MouseEvent, "button" | "ctrlKey" | "metaKey" | "shiftKey" | "altKey">) =>
  e.button === 0 && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.altKey;

// What the viewer of a chat goes through: its pictures and videos in the
// order the chat shows them (a message's grid of pictures first). Not the
// messages of blocked users still hidden (opened: their groups shown
// anyway), not the spoilers still hidden; view: see attachmentView.
export const chatMedia = <U>(items: readonly ChatItem[], opened: (g: MessageGroup) => boolean, view: (u: U) => string, hidden: (u: U) => boolean) =>
  items
    .flatMap((i) => (i.kind === "group" ? [i.group] : i.kind === "blocked" && i.groups.some(opened) ? i.groups : []))
    .flatMap((g) => g.messages)
    .flatMap((m) => {
      const { grid, rest } = splitFiles(("uploads" in m && (m.uploads as U[])) || [], view);
      return [...grid, ...rest].filter((u) => (view(u) === "image" || view(u) === "video") && !hidden(u));
    });

// The viewer's step (see fileGrid step); "older": a step back from the
// first item while the chat has older messages to load.
export const viewerStep = (index: number, by: number, count: number, more: boolean) =>
  by < 0 && index === 0 && more ? "older" : step(index, by, count);
