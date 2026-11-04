import { sameFiles, type Pending } from "./pendingFiles";
import { fileKey } from "./textChannels";
import type { View } from "./place";
import { i18n, t } from "./i18n.svelte";

// The message box of a chat (see ChatPane).

// One send of files at a time: no new file waits while one is sent.
export const canAttach = (uploads: boolean, uploading: boolean) => uploads && !uploading;

// Files in a paste: some engines give a pasted image only as an item, not
// in files.
type Pasted = { files: ArrayLike<File>; items: ArrayLike<{ kind: string; getAsFile(): File | null }> };
export const pastedFiles = (data: Pasted | null | undefined): File[] => {
  if (!data) return [];
  if (data.files.length) return Array.from(data.files);
  return Array.from(data.items).flatMap((i) => (i.kind === "file" ? (i.getAsFile() ?? []) : []));
};

// WebKitGTK (the Linux app) gives the page nothing on an image paste: no
// files, no items, no types. The clipboard API still reads the image then.
export const emptyPaste = (data: { types: ArrayLike<string> } | null | undefined) =>
  !Array.from(data?.types ?? []).some((t) => t === "Files" || t === "text/plain");

type Read = () => Promise<{ types: readonly string[]; getType(type: string): Promise<Blob> }[]>;
// The first image on the clipboard as a file, or null (also without
// clipboard access).
export const clipboardImage = async (read: Read): Promise<File | null> => {
  try {
    for (const item of await read()) {
      const type = item.types.find((t) => t.startsWith("image/"));
      if (type) return new File([await item.getType(type)], `pasted.${type.split(/[/+]/)[1]}`, { type });
    }
  } catch {
    // no clipboard API or no access
  }
  return null;
};

// A send the server refused comes back; it fills the box only if the user
// has not typed something new.
export const refusedDraft = (draft: string, text: string) => (draft.trim() ? draft : text);

// A file over the server's limit (none known: the server decides) is
// refused before it is sent.
const mb = (bytes: number) =>
  new Intl.NumberFormat(i18n.locale, { style: "unit", unit: "megabyte", maximumFractionDigits: 1 }).format(bytes / 1024 / 1024);
// The limit (as text) when the file is over it.
const over = (size: number, max: number | undefined) => (max && size > max ? mb(max) : null);
export const tooLarge = (size: number, max: number | undefined) => {
  const limit = over(size, max);
  return limit ? t("chat.fileTooLarge", { max: limit }) : null;
};
// A file to upload with a message (spoiler: in its name, see lib/media);
// its description (alt text) has at most MAX_DESCRIPTION characters, as
// on the server.
export type OutgoingUpload = { file: File; description?: string };
export const MAX_DESCRIPTION = 1024;

// Picked files join the waiting ones: those over the size limit are left
// out one by one, and only `max` files go in one message (note: why some
// were left out). With one file per message, a pick replaces the waiting
// file.
export const addFiles = (waiting: readonly Pending[], picked: readonly File[], max: number, sizeMax: number | undefined) => {
  const notes: string[] = [];
  const fits = picked.filter((file) => {
    const limit = over(file.size, sizeMax);
    if (limit) notes.push(t("chat.namedFileTooLarge", { name: file.name, max: limit }));
    return !limit;
  });
  const files = [...(max === 1 && fits.length ? [] : waiting), ...fits.map((file) => ({ file, spoiler: false }))];
  if (files.length > max && max > 1) notes.push(max === 1 ? t("chat.oneFilePerMessage") : t("chat.filesPerMessageMax", { count: max }));
  const kept = files.slice(0, max);
  return { files: kept, note: notes.join(" ") || null, changed: !sameFiles(kept.map((p) => p.file), waiting.map((p) => p.file)) };
};
// The upload button's tooltip, with the limit if known.
export const uploadTitle = (max: number | undefined, count = 1) => {
  if (!max) return t(count > 1 ? "chat.uploadFiles" : "chat.uploadFile");
  return t(count > 1 ? "chat.uploadFilesUpToEach" : "chat.uploadFileUpTo", { size: mb(max) });
};

// The open chat, for its draft: channels by id (voice channel names
// repeat in the tree, a rename keeps the draft), private chats by name.
export const chatKeyOf = (view: View, voiceChannel: number) =>
  view.kind === "text" ? `text ${view.id}` : view.kind === "dm" ? `dm ${view.name}` : `voice ${voiceChannel}`;

type Sent = { chat: string; caption: string; files: readonly File[] };
type Now = { chat: string; draft: string; files: readonly File[]; drafts: ReadonlyMap<string, string> };

// What goes after an upload finished: only a success takes the files and
// the caption (a failure leaves them for a retry), and only while they
// are still what was sent (the same chat, the caption not changed). A
// caption kept as the draft of the chat we left goes from `drafts`.
export const afterUpload = (ok: boolean, sent: Sent, now: Now) => {
  const here = now.chat === sent.chat;
  const kept = ok && !here && now.drafts.get(sent.chat)?.trim() === sent.caption;
  return {
    files: ok && sameFiles(now.files, sent.files),
    caption: ok && here && now.draft.trim() === sent.caption,
    drafts: kept ? new Map([...now.drafts].filter(([chat]) => chat !== sent.chat)) : now.drafts,
  };
};

// An edit's end: save (with the keys of files taken off), nothing (no
// change), or ask to delete a message left without text and files, as in
// Discord. files, removed: the files' URLs.
type Edit = { old: string; text: string; files: readonly string[]; removed: readonly string[] };
export const editOutcome = ({ old, text, files, removed }: Edit): { save: boolean; remove: string[]; delete?: true } => {
  const remove = files.filter((f) => removed.includes(f)).flatMap((f) => fileKey(f) ?? []);
  if (!text && files.length === remove.length) return { save: false, remove: [], delete: true };
  return { save: text !== old || remove.length > 0, remove };
};

// The drafts of the chats not open, kept above the panes (a pane closes
// with its chat: a thread over the channel on phones, a thread pane).
export type Drafts = { value: ReadonlyMap<string, string> };

// A chat's draft kept (none when empty), as when its pane closes.
export const keepDraft = (drafts: ReadonlyMap<string, string>, chat: string, draft: string): ReadonlyMap<string, string> => {
  const next = new Map(drafts);
  if (draft.trim()) next.set(chat, draft);
  else next.delete(chat);
  return next;
};

// Another chat: the draft of the one we leave is kept (drafts: of the
// chats not open, by chat key), the other one's comes back.
export const switchDraft = (drafts: ReadonlyMap<string, string>, from: string, to: string, draft: string) => {
  const next = new Map(keepDraft(drafts, from, draft));
  const back = next.get(to) ?? "";
  next.delete(to);
  return { drafts: next, draft: back };
};
