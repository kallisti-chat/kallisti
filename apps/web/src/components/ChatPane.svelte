<script lang="ts">
  import UserName from "./UserName.svelte";
  import { autosize } from "../lib/autosize";
  import { afterImageError, asSpoiler, attachmentView, chatSrc, imageBox, lightboxOf } from "../lib/media";
  import { emojiImages } from "../lib/emojiImages.svelte";
  import Emoji from "./Emoji.svelte";
  import EmojiPicker from "./EmojiPicker.svelte";
  import StickerPicker from "./StickerPicker.svelte";
  import { stickerText, type Sticker } from "../lib/stickers";
  import MessageSheet, { type SheetItem } from "./MessageSheet.svelte";
  import ChannelSheet from "./ChannelSheet.svelte";
  import { headerParts } from "../lib/chatHeader";
  import { hold, longPress } from "../lib/longPress";
  import { createHoldTalk } from "../lib/holdTalk";
  import { htmlToLines, htmlToText } from "../lib/htmlText";
  import { collapses, MAX_HEIGHT_PX } from "../lib/collapse";
  import { previewText, toggleSpoiler } from "../lib/markdown";
  import { loadEmojiData, normalEmoji, quickRows, searchEmoji, withTone, type EmojiData } from "../lib/emojiPicker";
  import { emojiUses, recentEmoji, skinTone, usedEmoji } from "../lib/emojiPrefs.svelte";
  import { customEmojiFor, customEntries, jumboEmoji, knownEmoji, refsAsNames } from "../lib/customEmoji";
  import { composerEmoji, emojiActions, fromRefs, personalEntries, sameEmoji, toRefs } from "../lib/personalEmoji";
  import { blockedUsers, settings } from "../lib/settings.svelte";
  import AudioPlayer from "./AudioPlayer.svelte";
  import VideoPlayer from "./VideoPlayer.svelte";
  import { opensLightbox } from "../lib/lightbox";
  import { gridCells, splitFiles } from "../lib/fileGrid";
  import { COMMON_EMOJI, reactedBy, type Reaction } from "../lib/reactions";
  import type { LinkPreview } from "../lib/linkPreview";
  import { highlightMentions, pingsMe, replyPing, suggestNames, SUGGESTIONS } from "../lib/mentions";
  import { quoted, typedFilter, typedWord } from "../lib/suggest";
  import { createCombobox } from "../lib/combobox.svelte";
  import { matchPlaces } from "../lib/switcher";
  import type { SearchPlace } from "../lib/search";
  import Suggestions, { type Suggestion } from "./Suggestions.svelte";
  import Lightbox from "./Lightbox.svelte";
  import PollForm from "./PollForm.svelte";
  import MessageInfo from "./MessageInfo.svelte";
  import ReactionsSheet from "./ReactionsSheet.svelte";
  import { infoRows } from "../lib/messageInfo";
  import PollView from "./PollView.svelte";
  import type { PollBody } from "../lib/polls";
  import Toast from "./Toast.svelte";
  import { escapeHtml, type ChatMessage } from "@kallisti/protocol";
  import { formatSize, type Attachment, type DisplayMessage } from "../lib/textChannels";
  import { replyBoxText, type Thread } from "../lib/threads";
  import {
    addFiles,
    afterUpload,
    editOutcome,
    MAX_DESCRIPTION,
    canAttach,
    clipboardImage,
    emptyPaste,
    keepDraft,
    pastedFiles,
    refusedDraft,
    switchDraft,
    uploadTitle,
    type Drafts,
    type OutgoingUpload,
  } from "../lib/composer";
  import { createPendingFiles, type Pending, type PendingFiles } from "../lib/pendingFiles";
  import { linuxApp } from "../lib/desktop";
  import { formatSeconds, slowLeft } from "../lib/slowMode";
  import { chatItems, formatDay, formatTime, formatTimestamp, itemKey, messageKey, type MessageGroup } from "../lib/chat";
  import { awayFromBottom, newSince, nextScrollTop } from "../lib/scroll";
  import { jumpStep } from "../lib/jump";
  import { tick, untrack, type ComponentProps, type Snippet } from "svelte";
  import { isChannelLink, sanitize as safe } from "../lib/sanitize";
  import Avatar from "./Avatar.svelte";
  import EmojiMenu from "./EmojiMenu.svelte";
  import Icon from "./Icon.svelte";

  let {
    title,
    topic = "",
    kind,
    stored = false,
    messages,
    welcomeText,
    membersOpen,
    onToggleMembers,
    more = false,
    loadingOlder = false,
    onLoadOlder,
    error = null,
    onUpload,
    uploading = false,
    uploadStep,
    uploadMax,
    maxUploads = 1,
    returned = null,
    stickers = [],
    onSticker,
    onPoll,
    onVote,
    onEndPoll,
    onMenu,
    menuOpen = false,
    lost = false,
    onHoldTalk,
    talking = false,
    onSend,
    onClose,
    me = "",
    knownNames = [],
    onMention,
    people = [],
    channelNames = [],
    searchPlaces = [],
    onSearch,
    onPins,
    onOpenChannel,
    linkTo,
    onLink,
    stage,
    pinsOpen = false,
    mayPin = false,
    onPin,
    jumpTarget = null,
    onJumped,
    mayEdit,
    mayDelete,
    onEdit,
    onDelete,
    typing = "",
    onTyping,
    onReact,
    myId = null,
    onAddEmoji,
    onRemoveEmoji,
    previewOf,
    httpBase = "",
    cannotWrite = null,
    slow = null,
    loginToWrite = null,
    chatKey,
    drafts = { value: new Map() },
    pendingFiles = createPendingFiles(null, ""),
    compact = false,
    intro,
    tools,
    threadOf,
    onOpenThread,
    onCreateThread,
    onThreads,
    threadsOpen = false,
    phone = false,
    details,
  }: {
    title: string;
    // Shown after the title, like a Discord channel topic (plain text).
    topic?: string;
    // channel: a Mumble voice channel's chat (not stored), text: a stored
    // Kallisti text channel, dm: a private conversation.
    kind: "channel" | "text" | "dm";
    // A voice channel's chat that the server stores (with history).
    stored?: boolean;
    messages: readonly (ChatMessage | DisplayMessage)[];
    welcomeText: string;
    membersOpen: boolean;
    onToggleMembers: () => void;
    // Text channels: older messages can be loaded.
    more?: boolean;
    loadingOlder?: boolean;
    onLoadOlder?: () => void;
    // The server refused something (for example a message that is too long).
    error?: string | null;
    // Phones: open the channel list, and push-to-talk as a hold button.
    onMenu?: () => void;
    menuOpen?: boolean;
    // The connection is lost: only the menu (to the rail) and the title
    // take input (the rest would be lost).
    lost?: boolean;
    onHoldTalk?: (down: boolean) => void;
    // Really sending (also false after a window blur released the key).
    talking?: boolean;
    // Our name: messages that mention us stand out.
    me?: string;
    // Names that @mentions turn into pills; a click on one opens its menu.
    knownNames?: readonly string[];
    onMention?: (name: string, pill: HTMLElement, event: MouseEvent) => void;
    // Names "@" suggests (online users, recent authors).
    people?: readonly string[];
    // Text channel names, for "#" suggestions; chats for "in:" in the search.
    channelNames?: readonly string[];
    searchPlaces?: readonly SearchPlace[];
    // Searching stored messages; jumpTarget: a message to scroll to once
    // it is loaded (then onJumped).
    onSearch?: (text: string) => void;
    // Shown between the header and the messages (screen shares).
    stage?: Snippet;
    // A "#name" link in a message was clicked.
    onOpenChannel?: (id: number) => void;
    // A stored message's link ("Copy link"); onLink: a click on a link,
    // for links to messages here (see lib/messageLink), else null.
    linkTo?: (id: number) => string;
    onLink?: (href: string) => "opened" | "unavailable" | null;
    // The pinned messages panel (undefined: none here).
    onPins?: () => void;
    pinsOpen?: boolean;
    mayPin?: boolean;
    onPin?: (id: number, on: boolean) => void;
    jumpTarget?: number | null;
    onJumped?: () => void;
    // Stored messages: who may change which (the server checks again).
    mayEdit?: (m: DisplayMessage) => boolean;
    mayDelete?: (m: DisplayMessage) => boolean;
    onEdit?: (id: number, text: string, remove: string[]) => void; // remove: keys of files to take off
    onDelete?: (id: number) => void;
    // "bob is typing…" beside the message box; onTyping: we type.
    typing?: string;
    onTyping?: () => void;
    // Stored messages: add (on) or remove our reaction; myId: our Mumble
    // user id (null: not registered, no reactions).
    onReact?: (id: number, emoji: string, on: boolean) => void;
    myId?: number | null;
    // A custom emoji in a message or reaction: add it to ours (registered
    // users but guests), remove a personal one everywhere (moderators).
    onAddEmoji?: (emoji: string, name: string) => Promise<void>;
    onRemoveEmoji?: (emoji: string) => Promise<void>;
    // Link previews of stored messages; images are on the server (httpBase).
    previewOf?: (id: number) => LinkPreview | null;
    httpBase?: string;
    // Why we cannot write now (a time-out); the message box says it.
    cannotWrite?: string | null;
    // Slow mode here (see lib/slowMode): sending waits, the box counts down.
    slow?: { seconds: number; lastPost?: number; until?: number } | null;
    // A guest that may only read (see lib/guests): a note and the way to
    // an account instead of the message box.
    loginToWrite?: { note: string; action: string; onAction?: () => void } | null;
    // Tells the chats apart (see chatKeyOf); else the title.
    chatKey?: string;
    // Kept here while no pane shows their chat (see lib/composer).
    drafts?: Drafts;
    // Picked files not sent, by chat (see lib/pendingFiles).
    pendingFiles?: PendingFiles;
    // A thread's pane (see lib/threads): no member list button; intro: in
    // place of the channel's welcome; tools: more header buttons.
    compact?: boolean;
    intro?: Snippet;
    tools?: Snippet;
    // Threads started from messages (a box under them that opens them),
    // starting one from a message, and the channel's thread list.
    threadOf?: (message: number) => Thread | undefined;
    onOpenThread?: (thread: Thread) => void;
    onCreateThread?: (message: number, text: string) => void;
    onThreads?: () => void;
    threadsOpen?: boolean;
    // Phones (see lib/chatHeader): a simpler header; a tap on the name
    // opens the channel sheet (ChannelSheet) with the topic, pins,
    // threads and these: notification settings and the chat's link.
    phone?: boolean;
    details?: Pick<ComponentProps<typeof ChannelSheet>, "notify" | "onNotify" | "onCopyLink" | "onCopyInvite">;
    // Text channels: attach files (button, paste or drop), with a caption.
    // Settles once the server confirmed the message; fails with why (busy,
    // upload failed, refused, no answer, disconnected: the files stay).
    // nonce: the same for each retry of these files (see lib/pendingFiles).
    onUpload?: (files: OutgoingUpload[], caption: string, nonce: string, reply?: number, noPing?: boolean) => Promise<void>;
    uploading?: boolean;
    uploadStep?: string; // how far the upload is ("Uploading 2 of 5…")
    uploadMax?: number; // the server's limit per file (bytes)
    maxUploads?: number; // files per message
    // The server's stickers (urls absolute) and sending one as a message
    // of its own (stored chats; settles once the server stored it).
    stickers?: readonly Sticker[];
    onSticker?: (sticker: number, reply?: number, noPing?: boolean) => Promise<void>;
    // Polls (see lib/polls): a new one (settles once stored), our vote,
    // the author's early end.
    onPoll?: (poll: PollBody, nonce: string) => Promise<void>;
    onVote?: (message: number, answers: number[]) => void;
    onEndPoll?: (message: number) => void;
    // A send the server refused, back for the box (n: which refusal).
    returned?: { text: string; n: number } | null;
    // Missing: the conversation is read-only (the other user left).
    // reply: the stored message it answers (stored chats only).
    onSend?: (text: string, reply?: number, noPing?: boolean) => void;
    onClose?: () => void;
  } = $props();

  let draft = $state("");
  let list: HTMLElement | undefined = $state();
  // "Today at" must turn into "Yesterday at" after midnight.
  let now = $state(Date.now());
  const items = $derived(chatItems(messages, blockedUsers.has));
  const target = $derived(kind === "dm" ? `@${title}` : kind === "text" ? `#${title}` : title);

  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 60_000);
    return () => clearInterval(timer);
  });
  // Slow mode: a clock by the second while it is on.
  let clock = $state(Date.now());
  const slowWait = $derived(slow ? slowLeft(slow.seconds, slow.lastPost, slow.until, clock) : 0);
  const waitText = $derived(formatSeconds(Math.ceil(slowWait / 1000)));
  const slowOn = $derived(!!slow?.seconds); // not every new message restarts the clock
  $effect(() => {
    if (!slowOn) return;
    clock = Date.now();
    const timer = setInterval(() => (clock = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  // Scrolling after new content: measure before the DOM update, then follow
  // the bottom or keep the reading position (see nextScrollTop). Tracks the
  // count, not `messages` (a new array on every server update); `target`
  // tells a conversation switch, the first message's time tells older
  // history loading above.
  // Both effects track exactly the same values, and the snapshot is used
  // once: a stale snapshot would move the reader back.
  let before: { scrollHeight: number; scrollTop: number; clientHeight: number } | null = null;
  // Images load after the scroll and make the content taller. A reader at
  // the bottom stays there.
  let atBottom = true;
  // Up in the history: "Jump to present", with the messages that came since
  // (leftAt: the newest message's time when the reader left the bottom).
  let away = $state(false);
  let leftAt = $state<number | null>(null);
  // Sent while up there: the place stays, the bar says so.
  let sentAway = $state(false);
  const newCount = $derived(newSince(messages.map((m) => m.at), leftAt));
  // A scroll right after the list's height changed comes from that
  // change (Android's keyboard shrinks the page, the scroll event comes
  // before the ResizeObserver): a reader at the bottom stays there.
  let seenHeight = 0;
  const trackBottom = () => {
    if (!list) return;
    if (list.clientHeight !== seenHeight) {
      seenHeight = list.clientHeight;
      if (atBottom) return keepBottom();
    }
    atBottom = list.scrollHeight - list.scrollTop - list.clientHeight <= 80;
    away = awayFromBottom(list);
    if (!away) [leftAt, sentAway] = [null, false];
    else leftAt ??= messages.at(-1)?.at ?? 0;
  };
  const toPresent = () => {
    const motion = !matchMedia("(prefers-reduced-motion: reduce)").matches;
    list?.scrollTo({ top: list.scrollHeight, behavior: motion ? "smooth" : "auto" });
  };
  function keepBottom() {
    if (list && atBottom) list.scrollTop = list.scrollHeight;
  }
  // The list gets shorter or taller (the keyboard, the window): a reader at
  // the bottom stays there too.
  $effect(() => {
    if (!list) return;
    const observer = new ResizeObserver(keepBottom);
    observer.observe(list);
    return () => observer.disconnect();
  });
  let shown = { target: "", firstAt: 0 };
  $effect.pre(() => {
    void [messages.length, target];
    if (list) before = { scrollHeight: list.scrollHeight, scrollTop: list.scrollTop, clientHeight: list.clientHeight };
  });
  $effect(() => {
    void [messages.length, target];
    untrack(() => {
      if (!list) return;
      const firstAt = messages[0]?.at ?? 0;
      const switched = target !== shown.target;
      const prepended = !switched && firstAt < shown.firstAt;
      const top = nextScrollTop(before, list.scrollHeight, switched, prepended);
      if (top !== null) list.scrollTop = top;
      shown = { target, firstAt };
      before = null;
      trackBottom();
    });
  });

  let filePicker: HTMLInputElement | undefined = $state();
  // Counted: dragging over child elements fires enter/leave pairs.
  let dragDepth = $state(0);
  const dragging = $derived(dragDepth > 0);
  // Groups of messages from blocked users that were shown anyway.
  let unblocked = $state<ReadonlySet<string>>(new Set());
  const groupKey = (g: MessageGroup) => `${g.actorName} ${g.at}`;
  const unblock = (groups: MessageGroup[]) => (unblocked = new Set([...unblocked, ...groups.map(groupKey)]));
  // Spoiler attachments shown (by URL).
  let shownFiles = $state<ReadonlySet<string>>(new Set());
  // Chosen files wait above the message box until they are sent with
  // Enter (with the typed text as caption) or removed; kept per chat in
  // pendingFiles (over another chat, a reconnect, a reload).
  // Raw: kept files go to IndexedDB, which cannot store state proxies.
  let pending = $state.raw<Pending[]>([]);
  const pendingList = $derived(pending.map((p) => p.file));
  const WAIT_UPLOAD = "Wait for the upload to finish";
  // Pictures' previews (object URLs) by file; those of files gone are let go.
  let previews = $state(new Map<File, string>());
  const showPending = (next: readonly Pending[] | undefined) => {
    pending = [...(next ?? [])];
    const kept = new Map<File, string>();
    for (const { file } of pending) {
      const url = previews.get(file) ?? (file.type.startsWith("image/") ? URL.createObjectURL(file) : undefined);
      if (url) kept.set(file, url);
    }
    for (const [file, url] of previews) if (!kept.has(file)) URL.revokeObjectURL(url);
    previews = kept;
  };
  const setPending = (next: Pending[]) => {
    showPending(next);
    pendingFiles.set(chat, next, uploadMax);
  };
  const cancelPending = () => {
    pendingFiles.clear(chat);
    showPending([]);
  };
  const removePending = (i: number) => setPending(pending.filter((_, j) => j !== i));
  // The file whose description is open (see MAX_DESCRIPTION).
  let describing = $state<File | null>(null);
  // keep: also into pendingFiles (on change; each key only shows it, so
  // a Send right after typing has it).
  const describePending = (i: number, text: string, keep = true) => {
    const description = text.trim();
    const next = pending.map((p, j) => (j === i ? { ...p, description } : p));
    if (keep) setPending(next);
    else showPending(next);
  };
  const focusOnMount = (el: HTMLElement) => el.focus();
  const togglePendingSpoiler = (i: number) => setPending(pending.map((p, j) => (j === i ? { ...p, spoiler: !p.spoiler } : p)));
  const sendFiles = (files: ArrayLike<File> | null | undefined) => {
    const picked = Array.from(files ?? []);
    if (!picked.length) return;
    if (!canAttach(!!onUpload, uploading || !!sendStatus?.sending)) {
      if (onUpload) notify(WAIT_UPLOAD);
      return;
    }
    const added = addFiles(pending, picked, maxUploads, uploadMax);
    if (added.note) notify(added.note);
    if (!added.changed) return;
    setPending(added.files);
    messageInput?.focus(); // after the file picker or a drop: Enter sends
  };
  let returnedSeen = 0; // each refusal fills the box once
  $effect(() => {
    if (!returned || returned.n <= returnedSeen) return;
    returnedSeen = returned.n;
    const back = returned.text;
    untrack(() => (draft = refusedDraft(draft, fromRefs(back, custom.mine, custom.urls))));
  });
  // Another conversation: drop the reply and the edit (the ids are of the
  // old chat). The typed text is kept per chat (see switchDraft), also when
  // this pane closes; draftFor: the chat of `draft`. A waiting file stays
  // in pendingFiles and comes back with its chat.
  const chat = $derived(chatKey ?? target);
  // The waiting files' send (also one a pane before a reconnect started):
  // sending, or why it failed. A send that ends elsewhere takes them here too.
  let filesChanged = $state(0);
  $effect(() =>
    pendingFiles.watch(() => {
      filesChanged++;
      if (pending.length && !pendingFiles.holds(chat, pendingList)) showPending(pendingFiles.current(chat));
    }),
  );
  const sendStatus = $derived((void filesChanged, pending.length ? pendingFiles.status(chat, pendingList) : undefined));
  let draftFor = "";
  $effect(() => {
    const to = chat;
    untrack(() => {
      if (to === draftFor) return;
      ({ drafts: drafts.value, draft } = switchDraft(drafts.value, draftFor, to, draft));
      draftFor = to;
      showPending([]);
      void pendingFiles.get(to).then((back) => {
        if (back && !closed && chat === to && !pending.length) showPending(back);
      });
      replying = null;
      editing = null;
      expanded = new Set();
    });
  });
  // After this pane closes, an upload that ends finds its chat not open
  // (see afterUpload): a sent caption leaves the kept drafts.
  let closed = false;
  $effect(() => () => {
    closed = true;
    drafts.value = keepDraft(drafts.value, draftFor, draft);
    showPending([]);
  });
  const onPaste = (event: ClipboardEvent) => {
    if (!onUpload) return;
    const files = pastedFiles(event.clipboardData);
    if (files.length) {
      event.preventDefault();
      sendFiles(files);
    } else if (linuxApp() && emptyPaste(event.clipboardData)) {
      void clipboardImage(() => navigator.clipboard.read()).then((file) => file && sendFiles([file]));
    }
  };
  const onDrop = (event: DragEvent) => {
    event.preventDefault();
    dragDepth = 0;
    sendFiles(event.dataTransfer?.files);
  };

  // The button can disappear while held (wider screen, mode change, this
  // pane closed): then the press is released (see createHoldTalk).
  const holdTalk = createHoldTalk();
  $effect(() => {
    if (!onHoldTalk) holdTalk.release();
  });
  $effect(() => holdTalk.release);

  const submit = (event: SubmitEvent) => {
    event.preventDefault();
    const text = draft.trim();
    if (slowWait > 0 && (text || pending.length)) return void notify(`Slow mode: wait ${waitText}`);
    if (pending.length && onUpload) {
      // The files wait while others are sent (see canAttach).
      if (uploading || sendStatus?.sending) return void notify(WAIT_UPLOAD);
      // More than the server takes (kept from another server or a reload).
      if (pending.length > maxUploads) return void notify(`At most ${maxUploads} files per message: remove ${pending.length - maxUploads}.`);
      const files = pending;
      const sent = { chat, caption: text, files: files.map((p) => p.file), reply: replying?.id };
      sentAway ||= away;
      const [upload, caption, noPing] = [onUpload, withRefs(text), replying ? !replying.ping : undefined];
      const outgoing = files.map((p) => ({ file: asSpoiler(p.file, p.spoiler), description: p.description }));
      void pendingFiles.send(chat, sent.files, (nonce) => upload(outgoing, caption, nonce, sent.reply, noPing)).then((ok) => {
        const done = afterUpload(ok, sent, { chat: closed ? "" : chat, draft, files: pendingList, drafts: drafts.value });
        if (done.files) showPending([]);
        if (done.caption) draft = "";
        drafts.value = done.drafts;
        if (ok && replying?.id === sent.reply) replying = null;
      });
      return;
    }
    if (!text || !onSend) return;
    onSend(withRefs(text), replying?.id, replying ? !replying.ping : undefined);
    sentAway ||= away;
    replying = null;
    draft = "";
  };

  // The images shown large, from `start` (see Lightbox): a message's pictures.
  type Shown = NonNullable<DisplayMessage["uploads"]>[number];
  const viewerItem = (u: Shown) => ({ ...lightboxOf(u), ...(u.description && { description: u.description }) });
  let lightbox = $state<{ items: ReturnType<typeof viewerItem>[]; start: number } | null>(null);
  const showHidden = (u: Shown) => (shownFiles = new Set([...shownFiles, u.url]));
  const hideAgain = (u: Shown) => (shownFiles = new Set([...shownFiles].filter((url) => url !== u.url)));
  // Attachments that cannot play or show (see attachmentView): files.
  let unplayable = $state(new Set<string>());

  // Suggestions while typing "@name", "#channel" or ":emoji" (see
  // lib/suggest, lib/combobox).
  let messageInput: HTMLTextAreaElement | undefined = $state();
  const listId = $props.id();
  // Emoji data: loaded when ":" is first typed (as for the picker); after
  // a failure, again only for the next ":" word.
  let emojiData = $state.raw<EmojiData | null>(null);
  let emojiFailedAt = $state<number | null>(null);
  // Custom server emoji and ours (see Client). We type ":name:"; ours go
  // out as references (see lib/personalEmoji).
  const customEmoji = customEmojiFor();
  const custom = $derived(customEmoji());
  const withRefs = (text: string) => toRefs(text, custom.mine, custom.urls);
  const box = createCombobox({
    id: listId,
    text: () => draft,
    input: () => messageInput,
    find: typedWord,
    build: (typed): Suggestion[] => {
      if (typed.kind === "@") return suggestNames(people, typed.query, me).map((n) => ({ key: n, label: n, insert: `@${n}`, kind: "user" }));
      if (typed.kind === "#") return suggestNames(channelNames, typed.query, "").map((n) => ({ key: n, label: n, insert: `#${n}`, kind: "channel" }));
      // Custom emoji first, then ours; they go in as ":name:".
      const found = [
        ...searchEmoji(customEntries(custom.list), typed.query),
        ...searchEmoji(personalEntries(custom.mine), typed.query),
        ...searchEmoji(emojiData?.emoji ?? [], typed.query),
      ];
      return found.slice(0, SUGGESTIONS).map((e) => {
        const emoji = normalEmoji(withTone(e, skinTone.value));
        return { key: e.emoji, label: e.name, insert: composerEmoji(emoji, custom.mine, custom.urls), kind: "emoji", emoji };
      });
    },
    setText: (text, at) => setDraft(text, at),
    onPick: (s) => s.kind === "emoji" && s.emoji && usedEmoji(s.emoji),
  });
  $effect(() => {
    const typed = box.typed;
    if (typed?.kind !== ":" || emojiData || emojiFailedAt === typed.start) return;
    loadEmojiData().then(
      (d) => (emojiData = d),
      () => (emojiFailedAt = typed.start),
    );
  });
  const LIST_LABEL = { "@": "People", "#": "Channels", ":": "Emoji", from: "People", in: "Channels" } as const;
  // New text in the message box, with the caret at `at`.
  const setDraft = async (text: string, at: number) => {
    draft = text;
    await tick();
    messageInput?.focus();
    messageInput?.setSelectionRange(at, at);
    box.update();
  };

  // The "+" menu: a file or a poll (registered users, stored chats).
  const pollsHere = $derived(!!onPoll && myId !== null);
  let plusMenu = $state(false);
  let pollForm = $state(false);
  let plusButton: HTMLButtonElement | undefined = $state();
  let plusMenuEl: HTMLDivElement | undefined = $state();
  const closePlusMenu = () => {
    plusMenu = false;
    plusButton?.focus();
  };
  $effect(() => {
    if (!plusMenu) return;
    plusMenuEl?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    const away = (e: PointerEvent) => {
      if (!plusMenuEl?.contains(e.target as Node) && !plusButton?.contains(e.target as Node)) plusMenu = false;
    };
    document.addEventListener("pointerdown", away);
    return () => document.removeEventListener("pointerdown", away);
  });

  // Editing and deleting stored messages (stickers and polls stay as sent).
  const editable = (m: ChatMessage | DisplayMessage) => !("sticker" in m && m.sticker) && !("poll" in m && m.poll);
  // files: the message's; removed: the URLs of those to take off.
  let editing = $state<{ id: number; text: string; old: string; files: Shown[]; removed: string[]; canDelete: boolean } | null>(null);
  let confirming = $state<number | null>(null);
  let editInput: HTMLTextAreaElement | undefined = $state();
  let cancelButton: HTMLButtonElement | undefined = $state();
  const startEdit = async (m: ChatMessage | DisplayMessage) => {
    if (!("source" in m) || !m.source) return;
    const files = ("uploads" in m && m.uploads) || [];
    const canDelete = !!(mayDelete?.(m) && onDelete);
    editing = { id: m.source.id, text: fromRefs(m.source.text, custom.mine, custom.urls), old: m.source.text, files, removed: [], canDelete };
    await tick();
    editInput?.focus();
  };
  const saveEdit = () => {
    if (!editing) return;
    const text = withRefs(editing.text.trim());
    const { id, canDelete } = editing;
    const end = editOutcome({ old: editing.old, text, files: editing.files.map((f) => f.url), removed: editing.removed });
    editing = null;
    // Nothing left: as in Discord, it asks to delete the message.
    if (end.delete && canDelete) return void startConfirm(id);
    if (end.save) onEdit?.(id, text, end.remove);
    messageInput?.focus();
  };
  const toggleRemoved = (url: string) => {
    if (editing) editing.removed = editing.removed.includes(url) ? editing.removed.filter((u) => u !== url) : [...editing.removed, url];
  };
  // Replies and reactions: stored chats we may write in. picking: the
  // message whose emoji picker is open (focus moves into it and back).
  const canReply = $derived((stored || kind === "text") && !!onSend);
  const canReact = $derived(canReply && !!onReact && myId !== null);
  let picking = $state<number | null>(null);
  // In this pane (another server's hidden client may have the same ids).
  const inLine = (id: number | null, selector: string) => list?.querySelector<HTMLElement>(`[data-message="${id}"] ${selector}`);
  const openPicker = async (id: number) => {
    picking = id;
    await tick();
    inLine(id, ".actions .emoji")?.focus();
  };
  // The message box's emoji picker, at its button; the choice goes in at
  // the caret.
  let draftEmoji = $state<DOMRect | null>(null);
  const insertEmoji = (emoji: string) => {
    usedEmoji(emoji);
    draftEmoji = null;
    const from = messageInput?.selectionStart ?? draft.length;
    const to = messageInput?.selectionEnd ?? draft.length;
    const text = composerEmoji(emoji, custom.mine, custom.urls);
    void setDraft(draft.slice(0, from) + text + draft.slice(to), from + text.length);
  };
  // The sticker picker, at its button; a choice is sent at once (as a
  // reply when one is being written), the box keeps its text.
  let draftSticker = $state<DOMRect | null>(null);
  const stickerById = $derived(new Map(stickers.map((s) => [s.id, s])));
  const sendSticker = (s: Sticker) => {
    draftSticker = null;
    if (!onSticker) return;
    if (slowWait > 0) return void notify(`Slow mode: wait ${waitText}`);
    const reply = replying?.id;
    onSticker(s.id, reply, replying ? !replying.ping : undefined).then(
      () => {
        if (replying?.id === reply) replying = null;
      },
      (e: Error) => notify(e.message),
    );
    messageInput?.focus();
  };
  // The quick reactions in two rows (see quickRows): 8 most used, then
  // recent ones, 10 on wide screens, 9 on phones (under 8 and the "+");
  // "+" opens all emoji (allEmoji: for that message).
  const quickFor = (second: number) => quickRows(emojiUses.value, recentEmoji.value, COMMON_EMOJI, 8, second, (e) => knownEmoji(e, custom));
  const quick = $derived(quickFor(10));
  const quickPhone = $derived(quickFor(9));
  let allEmoji = $state.raw<{ id: number; anchor: DOMRect } | null>(null);
  // The current message (its reactions change while the picker is open).
  const allEmojiFor = $derived(allEmoji && messages.find((m) => idOf(m) === allEmoji!.id));
  // The menu of a custom emoji in a message or reaction (see EmojiMenu):
  // only when there is something to do with it.
  let emojiMenu = $state<{ x: number; y: number; emoji: string } | null>(null);
  // A custom emoji has a menu (add to my emoji, remove for everyone).
  const hasEmojiMenu = (emoji: string) => {
    const actions = emojiActions(emoji, custom, { add: !!onAddEmoji, remove: !!onRemoveEmoji });
    return !!actions && (actions.add !== null || actions.remove !== null);
  };
  const openEmojiMenu = (emoji: string, e: MouseEvent) => {
    if (!hasEmojiMenu(emoji)) return false;
    e.preventDefault();
    emojiMenu = { x: e.clientX, y: e.clientY, emoji };
    return true;
  };
  // Who reacted (ReactionsSheet), and a message's info (MessageInfo): by
  // id, so they follow updates and close when it is deleted.
  let reactionsFor = $state<{ id: number; start: string } | null>(null);
  let infoFor = $state<number | null>(null);
  const storedById = (id: number | null | undefined): DisplayMessage | undefined => {
    const m = id == null ? undefined : messages.find((x) => idOf(x) === id);
    return m && "source" in m && m.source ? (m as DisplayMessage) : undefined;
  };
  const reactionsMessage = $derived(storedById(reactionsFor?.id));
  const infoMessage = $derived(storedById(infoFor));
  const showInfo = (m: ChatMessage | DisplayMessage) => {
    if ("source" in m && m.source) infoFor = m.source.id;
  };
  // After Info from the phone sheet, the focus goes back to the message.
  const closeInfo = () => {
    const line = list?.querySelector<HTMLElement>(`li[data-message="${infoFor}"]`);
    infoFor = null;
    if (line && !line.contains(document.activeElement)) {
      line.tabIndex = -1;
      line.focus({ preventScroll: true });
    }
  };
  // A message deleted, or its last reaction taken back: the sheet goes.
  $effect(() => {
    if (reactionsFor && !reactionsMessage?.reactions?.length) reactionsFor = null;
  });
  const mineOf = (m: ChatMessage | DisplayMessage, emoji: string) =>
    !!("reactions" in m && m.reactions?.some((r) => sameEmoji(r.emoji, emoji) && reactedBy(r, myId)));
  const react = (m: ChatMessage | DisplayMessage, id: number, emoji: string) => {
    usedEmoji(emoji);
    onReact?.(id, emoji, !mineOf(m, emoji));
    void closePicker();
  };
  // A thread from a message that has none (stored chats we write in).
  const canThread = (id: number) => canReply && !!onCreateThread && !threadOf?.(id);
  // What we may do with a stored message (the server checks again).
  const allowed = (m: ChatMessage | DisplayMessage) => {
    if (!("source" in m) || !m.source || editing?.id === m.source.id) return null;
    return {
      id: m.source.id,
      canEdit: !!(mayEdit?.(m) && onEdit && editable(m)),
      canDelete: !!(mayDelete?.(m) && onDelete),
      canPin: !!(mayPin && onPin),
      pinned: "pinned" in m && !!m.pinned,
    };
  };

  // Phones: a long press opens the message's actions in a sheet. sheetFor: that
  // message (by id when stored, so it follows updates).
  let sheetFor = $state.raw<{ id?: number; m: ChatMessage | DisplayMessage } | null>(null);
  const sheetMessage = $derived(
    sheetFor && (sheetFor.id === undefined ? sheetFor.m : messages.find((m) => idOf(m) === sheetFor!.id)),
  );
  // Long messages show collapsed (see lib/collapse); expanded: their keys
  // while this chat is open. tall: Mumble HTML with images, by the measured
  // height. Collapsed text is only clipped: screen readers read all of it.
  let expanded = $state<ReadonlySet<string>>(new Set());
  let tall = $state<ReadonlySet<string>>(new Set());
  const isLong = (m: ChatMessage | DisplayMessage) =>
    "source" in m && m.source ? collapses(m.source.text) : collapses(htmlToLines(m.text)) || tall.has(messageKey(m));
  // Mumble HTML (no source): also by its height (images, blank lines).
  const measure = (el: HTMLElement, key: string | null) => {
    if (!key) return;
    const check = () => {
      if (tall.has(key) || el.scrollHeight <= MAX_HEIGHT_PX) return;
      tall = new Set([...tall, key]);
      void tick().then(keepBottom);
    };
    check();
    el.addEventListener("load", check, true); // images in the message
    return { destroy: () => el.removeEventListener("load", check, true) };
  };
  // Expanding keeps the place (the message grows below); folding a long
  // message brings its line back into view.
  const toggleLong = async (key: string, line: Element | null, open = !expanded.has(key)) => {
    expanded = open ? new Set([...expanded, key]) : new Set([...expanded].filter((k) => k !== key));
    await tick();
    if (!open) line?.scrollIntoView({ block: "nearest" });
    trackBottom();
  };
  // A short notice at the bottom (copied, a link that cannot open).
  let notice = $state({ n: 0, text: "" });
  const notify = (text: string) => (notice = { n: notice.n + 1, text });
  const copy = (text: string, done: string) =>
    (navigator.clipboard?.writeText(text) ?? Promise.reject()).then(
      () => notify(done),
      () => notify("Could not copy"),
    );
  const sheetItems = (m: ChatMessage | DisplayMessage): SheetItem[] => {
    const may = allowed(m);
    const done = (f: () => void) => () => ((sheetFor = null), f());
    const text = "source" in m && m.source ? m.source.text : htmlToText(m.text);
    return [
      ...(may && canReply ? [{ label: "Reply", icon: "reply" as const, run: done(() => startReply(m)) }] : []),
      ...(may && canThread(may.id) ? [{ label: "Create thread", icon: "thread" as const, run: done(() => onCreateThread?.(may.id, refsAsNames(text))) }] : []),
      ...(text ? [{ label: "Copy text", icon: "copy" as const, run: done(() => copy(refsAsNames(text), "Text copied")) }] : []),
      ...(may && linkTo ? [{ label: "Copy link", icon: "copy" as const, run: done(() => copy(linkTo(may.id), "Link copied")) }] : []),
      ...("source" in m && m.source ? [{ label: "Info", icon: "info" as const, run: done(() => showInfo(m)) }] : []),
      ...(may?.canPin
        ? [{ label: may.pinned ? "Unpin message" : "Pin message", icon: "pin" as const, run: done(() => onPin?.(may.id, !may.pinned)) }]
        : []),
      ...(may?.canEdit ? [{ label: "Edit message", icon: "edit" as const, run: done(() => void startEdit(m)) }] : []),
      ...(may?.canDelete
        ? [{ label: "Delete message", icon: "trash" as const, danger: true, confirm: "Delete this message?", run: done(() => onDelete?.(may.id)) }]
        : []),
    ];
  };
  // Not for a message without actions (an unstored image, read-only).
  const openSheet = (m: ChatMessage | DisplayMessage) => {
    const id = idOf(m);
    if (sheetItems(m).length || (canReact && id !== undefined)) sheetFor = { id, m };
  };
  const closePicker = async () => {
    const id = picking;
    picking = null;
    allEmoji = null;
    await tick();
    inLine(id, '[aria-label="Add a reaction"]')?.focus();
  };
  const startConfirm = async (id: number) => {
    confirming = id;
    await tick();
    cancelButton?.focus();
  };
  const endConfirm = () => {
    confirming = null;
    messageInput?.focus();
  };
  // Up in an empty message box edits our last message.
  const editLast = () => {
    const last = messages.findLast((m) => "source" in m && m.source && editable(m) && mayEdit?.(m));
    if (!last || !onEdit) return false;
    void startEdit(last);
    return true;
  };

  // Stored messages carry their id and the message they answer.
  const idOf = (m: ChatMessage | DisplayMessage) => ("source" in m ? m.source?.id : undefined);
  // Mentions as notifications see them: in the written text, not the
  // Markdown result (Mumble messages have no source: their HTML).
  const allOf = (m: ChatMessage | DisplayMessage) => (m as DisplayMessage).mentionAll; // @everyone, @here
  const pingText = (m: ChatMessage | DisplayMessage) => ("source" in m && m.source ? escapeHtml(m.source.text) : m.text);
  // Replies: the message we answer, and jumping to an original.
  // ping: the original's author gets a mention (the "@" switch).
  let replying = $state<{ id: number; author: string; ping: boolean } | null>(null);
  let flashed = $state<number | null>(null);
  const startReply = (m: ChatMessage | DisplayMessage) => {
    if (!("source" in m) || !m.source) return;
    replying = { id: m.source.id, author: m.actorName ?? "", ping: true };
    messageInput?.focus();
  };
  const jumpTo = (id: number) => {
    const line = list?.querySelector(`[data-message="${id}"]`);
    if (!line) return; // not loaded
    line.scrollIntoView({ block: "center" });
    trackBottom(); // now, before an image load could pull it back down
    flashed = id;
    setTimeout(() => (flashed = flashed === id ? null : flashed), 1500);
  };

  // Phones: the channel sheet, and the search field over the header.
  const hasDetails = $derived(phone && !!(topic || onPins || onThreads || (details?.notify && details.onNotify) || details?.onCopyLink));
  const parts = $derived(
    headerParts(phone, { topic: !!topic, search: !!onSearch, threads: !!onThreads, pins: !!onPins, details: hasDetails }),
  );
  let detailsOpen = $state(false);
  let searching = $state(false);
  // Not kept for another chat or after a resize.
  $effect(() => {
    void chatKey;
    void phone;
    detailsOpen = false;
    searching = false;
  });
  let searchOpener: HTMLButtonElement | undefined = $state();
  const openSearch = async () => {
    searching = true;
    await tick();
    searchInput?.focus();
  };
  const closeSearch = async () => {
    searching = false;
    await tick();
    searchOpener?.focus();
  };

  let searchText = $state("");
  // Suggestions after "from:" and "in:" (see lib/suggest).
  let searchInput: HTMLInputElement | undefined = $state();
  const searchBox = createCombobox({
    id: `${listId}-search`,
    text: () => searchText,
    input: () => searchInput,
    find: typedFilter,
    build: (typed): Suggestion[] => {
      if (typed.kind === "from")
        return suggestNames(people, typed.query, "").map((n) => ({ key: n, label: n, insert: `from:${quoted(n)}`, kind: "user" }));
      return matchPlaces(searchPlaces, typed.query)
        .slice(0, SUGGESTIONS)
        .map((p) => ({
          key: `${p.kind}${p.id}`,
          label: p.name,
          insert: `in:${p.kind === "text" ? `#${p.name}` : p.kind === "dm" ? `@${quoted(p.name)}` : quoted(p.name)}`,
          kind: p.kind === "text" ? "channel" : p.kind === "dm" ? "user" : "voice",
        }));
    },
    setText: async (text, at) => {
      searchText = text;
      await tick();
      searchInput?.focus();
      searchInput?.setSelectionRange(at, at);
    },
  });
  // Jump to a search result (jumpTarget) or a reply's original (seeking)
  // once it is loaded (see jumpStep).
  let seeking = $state<number | null>(null);
  const wanted = $derived(jumpTarget ?? seeking);
  let jumpPages = 0;
  $effect(() => {
    void wanted;
    jumpPages = 0;
  });
  $effect(() => {
    const id = wanted;
    if (id === null) return;
    const loaded = messages.some((m) => idOf(m) === id);
    const step = jumpStep({ loaded, more: more && !!onLoadOlder, loading: loadingOlder, pages: jumpPages });
    if (step === "load") {
      jumpPages++;
      onLoadOlder?.();
    } else if (step !== "wait") {
      seeking = null;
      onJumped?.();
      if (step !== "jump") return;
      // In hidden messages of a blocked user: shown first.
      const hidden = items.find((i) => i.kind === "blocked" && i.groups.some((g) => g.messages.some((m) => idOf(m) === id)));
      if (hidden?.kind === "blocked") unblock(hidden.groups);
      void tick().then(() => jumpTo(id));
    }
  });
</script>

<main
  class="chat"
  class:dragging
  class:hasTyping={!!typing}
  ondragenter={(e) => {
    if (onUpload && e.dataTransfer?.types.includes("Files")) dragDepth++;
  }}
  ondragover={(e) => {
    if (onUpload && e.dataTransfer?.types.includes("Files")) e.preventDefault();
  }}
  ondragleave={() => {
    if (dragDepth > 0) dragDepth--;
  }}
  ondrop={onUpload ? onDrop : undefined}
>
  {#snippet searchForm()}
    <form
      class="search"
      class:wide={searching}
      role="search"
      onsubmit={(e) => {
        e.preventDefault();
        if (searchText.trim()) onSearch?.(searchText.trim());
      }}
    >
      <input
        bind:this={searchInput}
        bind:value={searchText}
        placeholder="Search"
        aria-label="Search messages"
        title="Filters: from:name, in:#channel"
        {...searchBox.aria}
        oninput={searchBox.update}
        onclick={searchBox.update}
        onkeyup={searchBox.onkeyup}
        onkeydown={(e) => {
          searchBox.onkeydown(e);
          if (searching && e.key === "Escape" && !e.defaultPrevented) void closeSearch();
        }}
        onblur={searchBox.onblur}
      />
      {#if searchBox.items.length}
        <Suggestions
          id="{listId}-search"
          label={LIST_LABEL[searchBox.typed!.kind]}
          items={searchBox.items}
          active={searchBox.active}
          onPick={searchBox.pick}
          below
        />
      {/if}
    </form>
  {/snippet}
  <header>
    {#if parts.searchIcon && searching}
      <button class="back" onclick={closeSearch} title="Close search" aria-label="Close search"><Icon name="back" size={22} /></button>
      <div class="search-wrap" inert={lost}>{@render searchForm()}</div>
    {:else}
    {#if onMenu}
      <button class="menu" onclick={onMenu} aria-label="Channels" aria-expanded={menuOpen}>
        <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
          <path d="M4 6h16M4 12h16M4 18h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
      </button>
    {/if}
    {#if kind === "dm"}
      <span class="at" aria-hidden="true">@</span>
    {:else}
      <Icon name={compact ? "thread" : kind === "text" ? "hash" : "speaker"} size={22} />
    {/if}
    {#if hasDetails}
      <h1 class="ellipsis">
        <button class="title" onclick={() => (detailsOpen = true)} aria-haspopup="dialog" aria-expanded={detailsOpen} title="Channel details"
          ><span class="ellipsis">{title}</span><Icon name="chevron" size={16} /></button
        >
      </h1>
    {:else}
      <h1 class="ellipsis">{title}</h1>
    {/if}
    {#if parts.topic}
      <span class="divider-v" aria-hidden="true"></span>
      <span class="topic ellipsis" title={topic}>{topic}</span>
    {/if}
    <div class="tools" inert={lost}>
      {#if parts.searchBox}
        {@render searchForm()}
      {/if}
      {#if parts.searchIcon}
        <button bind:this={searchOpener} onclick={openSearch} title="Search" aria-label="Search messages"><Icon name="search" size={22} /></button>
      {/if}
      {#if parts.threads}
        <button class:on={threadsOpen} onclick={onThreads} aria-pressed={threadsOpen} title="Threads" aria-label="Threads">
          <Icon name="thread" size={20} />
        </button>
      {/if}
      {#if parts.pins}
        <button class:on={pinsOpen} onclick={onPins} aria-pressed={pinsOpen} title="Pinned messages" aria-label="Pinned messages">
          <Icon name="pin" size={20} />
        </button>
      {/if}
      {@render tools?.()}
      {#if kind !== "dm" && !compact}
        <button
          class:on={membersOpen}
          onclick={onToggleMembers}
          aria-pressed={membersOpen}
          title={membersOpen ? "Hide member list" : "Show member list"}
          aria-label="Member list"
          class="members-toggle"
        >
          <Icon name="members" size={22} />
        </button>
      {/if}
      {#if onClose}
        <button onclick={onClose} title="Close conversation" aria-label="Close conversation">
          <span class="x" aria-hidden="true">×</span>
        </button>
      {/if}
    </div>
    {/if}
  </header>
  {@render stage?.()}

  <!-- Clicks on "#name" links open the channel, clicks on spoilers show
       or hide them (see markdown). -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="scroller"
    inert={lost}
    bind:this={list}
    onscroll={trackBottom}
    onkeydown={(e) => {
      if ((e.key === "Enter" || e.key === " ") && toggleSpoiler(e.target as Element)) e.preventDefault();
    }}
    onclick={(e) => {
      // A click that ends a text selection does not hide a spoiler.
      if (!document.getSelection()?.toString() && toggleSpoiler(e.target as Element)) return e.preventDefault();
      const href = (e.target as Element).closest("a[href]")?.getAttribute("href");
      const opened = href && !e.ctrlKey && !e.metaKey && !e.shiftKey ? onLink?.(href) : null;
      if (opened) {
        e.preventDefault();
        if (opened === "unavailable") notify("You cannot open this message here");
        return;
      }
      const link = (e.target as Element).closest<HTMLElement>("a[data-channel]");
      if (!link || !isChannelLink(link)) return;
      e.preventDefault();
      onOpenChannel?.(Number(link.dataset.channel));
    }}
  >
    {#if more}
      <div class="older">
        <button onclick={onLoadOlder} disabled={loadingOlder}>
          {loadingOlder ? "Loading…" : "Load older messages"}
        </button>
      </div>
    {:else if intro}
      <div class="intro">{@render intro()}</div>
    {:else}
    <div class="intro">
      <div class="intro-icon" aria-hidden="true">
        {#if kind === "dm"}@{:else}<Icon name={kind === "text" ? "hash" : "speaker"} size={36} />{/if}
      </div>
      <h2>{kind === "dm" ? title : `Welcome to ${target}!`}</h2>
      <p>
        {#if kind === "dm"}
          This is the start of your conversation with {title}.{stored ? "" : " Private messages are not stored."}
        {:else if kind === "text"}
          This is the start of the {target} channel.
        {:else if stored}
          This is the start of the {title} voice channel's chat.
        {:else}
          This is the start of the {title} voice channel's chat. These messages are not stored: you
          only see what was sent while you were here.
        {/if}
      </p>
      {#if welcomeText}
        <div class="welcome">{@html safe(welcomeText)}</div>
      {/if}
    </div>
    {/if}

    <ol class="messages">
      {#each items as item (itemKey(item))}
        {#if item.kind === "day"}
          <li class="divider" role="separator"><span>{formatDay(item.at)}</span></li>
        {:else if item.kind === "group"}
          {@render lines(item.group)}
        {:else if item.groups.some((g) => unblocked.has(groupKey(g)))}
          {#each item.groups as group (messageKey(group.messages[0]!))}{@render lines(group)}{/each}
        {:else}
          {@const n = item.groups.reduce((a, g) => a + g.messages.length, 0)}
          {@const users = new Set(item.groups.map((g) => g.actorName)).size}
          <li class="blocked-line">
            {n === 1 ? "A message" : `${n} messages`} from {users === 1 ? "a blocked user" : "blocked users"}.
            <button type="button" onclick={() => unblock(item.groups)}>Show</button>
          </li>
        {/if}
      {/each}
    </ol>
  </div>

  {#if onHoldTalk}
    <button
      class="hold"
      inert={lost}
      class:held={talking}
      onpointerdown={(e) => {
        if (e.button !== 0) return;
        e.currentTarget.setPointerCapture(e.pointerId);
        holdTalk.press(onHoldTalk);
      }}
      onpointerup={holdTalk.release}
      onpointercancel={holdTalk.release}
      onlostpointercapture={holdTalk.release}
      oncontextmenu={(e) => e.preventDefault()}
    >
      {talking ? "Talking…" : "Hold to talk"}
    </button>
  {/if}
  <form class="send" inert={lost} onsubmit={submit}>
    {#if away}
      <div class="present">
        <span
          >{sentAway ? "Message sent" : newCount ? `${newCount} new ${newCount === 1 ? "message" : "messages"}` : "You are viewing older messages"}</span
        >
        <button type="button" onclick={toPresent}>{sentAway ? "Jump to your message" : "Jump to present"}</button>
      </div>
    {/if}
    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}
    {#if loginToWrite}
      <div class="read-only">
        <span>{loginToWrite.note}</span>
        {#if loginToWrite.onAction}
          <button type="button" onclick={loginToWrite.onAction}>{loginToWrite.action}</button>
        {/if}
      </div>
    {:else}
      {#if pending.length}
        <div class="pending">
          {#if sendStatus}
            <span class="pending-status ellipsis" class:failed={!sendStatus.sending} role="status"
              >{sendStatus.sending ? (uploadStep ?? "Sending…") : `Not sent: ${sendStatus.error}`}</span
            >
          {/if}
          <ul class="pending-files" aria-label="Files to send">
            {#each pending as p, i (p.file)}
              <li class="pending-file">
                {#if previews.has(p.file)}
                  <img src={previews.get(p.file)} alt="" onerror={(e) => e.currentTarget.remove()} />
                {/if}
                <span class="pending-name ellipsis" title={p.file.name}>{p.file.name}</span>
                <span class="file-size">{formatSize(p.file.size)}</span>
                <span class="pending-actions">
                  {#if maxUploads > 1}
                    <button type="button" class="pending-spoiler" aria-pressed={!!p.description} aria-expanded={describing === p.file}
                      disabled={sendStatus?.sending} aria-label="Description of {p.file.name}" title="Description (alt text)"
                      onclick={() => (describing = describing === p.file ? null : p.file)}>Alt</button
                    >
                  {/if}
                  <button type="button" class="pending-spoiler" aria-pressed={p.spoiler} disabled={sendStatus?.sending}
                    aria-label="Spoiler {p.file.name}" onclick={() => togglePendingSpoiler(i)}>Spoiler</button
                  >
                  <button type="button" class="pending-cancel" disabled={sendStatus?.sending} onclick={() => removePending(i)}
                    aria-label="Remove {p.file.name}">×</button
                  >
                </span>
              </li>
            {/each}
          </ul>
          {#each pending as p, i (p.file)}
            {#if describing === p.file}
              <label class="pending-description">
                <span>Description of {p.file.name} (alt text)</span>
                <!-- Kept on change, not on each key: the files go to IndexedDB with it. -->
                <textarea
                  rows="2"
                  maxlength={MAX_DESCRIPTION}
                  value={p.description ?? ""}
                  disabled={sendStatus?.sending}
                  oninput={(e) => describePending(i, e.currentTarget.value, false)}
                  onchange={(e) => describePending(i, e.currentTarget.value)}
                  onkeydown={(e) => {
                    if (e.key === "Escape" || (e.key === "Enter" && !e.shiftKey)) {
                      e.preventDefault();
                      describePending(i, e.currentTarget.value);
                      describing = null;
                      messageInput?.focus();
                    }
                  }}
                  use:focusOnMount
                ></textarea>
              </label>
            {/if}
          {/each}
        </div>
      {/if}
      {#if replying}
        <div class="replying">
          <span class="ellipsis">Replying to <strong>{replying.author}</strong></span>
          <button
            type="button"
            class="ping"
            class:off={!replying.ping}
            aria-pressed={replying.ping}
            title={replying.ping ? "The author gets a mention" : "The author gets no mention"}
            onclick={() => replying && (replying.ping = !replying.ping)}>@ {replying.ping ? "On" : "Off"}</button
          >
          <button type="button" aria-label="Cancel reply" onclick={() => (replying = null)}>×</button>
        </div>
      {/if}
      <div class="composer" class:disabled={!onSend || cannotWrite}>
        <button
          type="button"
          class="attach"
          class:busy={uploading}
          bind:this={plusButton}
          title={uploading ? "Uploading…" : onUpload ? uploadTitle(uploadMax, maxUploads) : "Uploads work in text channels and stored voice channel chats"}
          aria-label={pollsHere ? "Upload a file or create a poll" : "Upload a file"}
          aria-expanded={pollsHere ? plusMenu : undefined}
          disabled={!pollsHere && !canAttach(!!onUpload, uploading)}
          onclick={() => (pollsHere ? (plusMenu = !plusMenu) : filePicker?.click())}
        >
          {#if uploading}<span class="spinner"></span>{:else}<Icon name="plus" size={18} />{/if}
        </button>
        {#if plusMenu}
          <!-- A plain popover of two buttons; Escape or leaving it closes it. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="plus-menu"
            bind:this={plusMenuEl}
            onkeydown={(e) => {
              if (e.key === "Escape") (e.preventDefault(), e.stopPropagation(), closePlusMenu());
            }}
            onfocusout={(e) => {
              const to = e.relatedTarget as Node | null;
              if (to && !plusMenuEl?.contains(to) && to !== plusButton) plusMenu = false;
            }}
          >
            <button type="button" disabled={!canAttach(!!onUpload, uploading)} onclick={() => (closePlusMenu(), filePicker?.click())}>
              <Icon name="plus" size={16} /> Upload a file
            </button>
            <button type="button" onclick={() => (closePlusMenu(), (pollForm = true))}>📊 Create a poll</button>
          </div>
        {/if}
        <input
          type="file"
          multiple={maxUploads > 1}
          class="sr-only"
          tabindex="-1"
          bind:this={filePicker}
          onchange={(e) => {
            sendFiles(e.currentTarget.files);
            e.currentTarget.value = "";
          }}
        />
        <span class="sr-only" role="status">{uploading ? "Uploading…" : ""}</span>
        {#if box.items.length}
          <Suggestions id={listId} label={LIST_LABEL[box.typed!.kind]} items={box.items} active={box.active} onPick={box.pick} />
        {/if}
        <textarea
          rows="1"
          bind:this={messageInput}
          use:autosize={draft}
          enterkeyhint="send"
          onpaste={onPaste}
          oninput={(e) => {
            box.update();
            if (e.currentTarget.value.trim()) onTyping?.();
          }}
          onclick={box.update}
          onkeyup={box.onkeyup}
          onblur={box.onblur}
          onkeydown={(e) => {
            if (box.onkeydown(e)) return;
            // Enter picks a word in an input method (229: Safari, after
            // compositionend).
            if (e.isComposing || e.keyCode === 229) return;
            if (e.key === "ArrowUp" && !e.altKey && draft === "" && !pending.length && editLast()) {
              e.preventDefault();
              return;
            }
            // Enter sends; Shift+Enter makes a new line.
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              e.currentTarget.form?.requestSubmit();
              return;
            }
            if (e.key === "Escape" && replying) replying = null;
            else if (e.key === "Escape" && pending.length && !sendStatus?.sending) cancelPending();
          }}
          {...box.aria}
          bind:value={draft}
          placeholder={cannotWrite ?? (pending.length ? "Add a caption (optional), Enter to send" : onSend ? `Message ${target}` : `${title} left the server`)}
          disabled={!onSend || !!cannotWrite}
          aria-label="Message"
        ></textarea>
        {#if slowWait > 0}
          <span class="slow" role="timer" title="Slow mode: you can send again then">{waitText}</span>
        {/if}
        {#if onSend}
          <button
            type="button"
            class="emoji-button"
            aria-label="Add an emoji"
            title="Emoji"
            onclick={(e) => (draftEmoji = e.currentTarget.getBoundingClientRect())}><Emoji text="🙂" /></button
          >
          {#if onSticker && stickers.length}
            <button
              type="button"
              class="emoji-button sticker-button"
              aria-label="Send a sticker"
              title="Stickers"
              onclick={(e) => (draftSticker = e.currentTarget.getBoundingClientRect())}><Icon name="sticker" size={24} /></button
            >
          {/if}
          <!-- Touch screens: a send button (Enter is the only other way).
               The press keeps the keyboard open. -->
          {#if draft.trim() || pending.length}
            <button type="submit" class="send-button" aria-label="Send" title="Send" onpointerdown={(e) => e.preventDefault()}>
              <Icon name="send" size={20} />
            </button>
          {/if}
        {/if}
      </div>
    {/if}
    <p class="typing ellipsis" class:idle={!typing} aria-live="polite">{typing}</p>
  </form>
</main>

<!-- Menus, sheets and pickers act on the connection: none while it is lost. -->
<div class="overlays" inert={lost}>
{#if emojiMenu}
  <EmojiMenu {...emojiMenu} onAdd={onAddEmoji} onRemove={onRemoveEmoji} onClose={() => (emojiMenu = null)} />
{/if}
{#if sheetMessage}
  {@const m = sheetMessage}
  {@const id = idOf(m)}
  <MessageSheet
    quick={quickPhone}
    mine={(emoji) => mineOf(m, emoji)}
    onReact={canReact && id !== undefined ? (emoji) => (react(m, id, emoji), (sheetFor = null)) : undefined}
    items={sheetItems(m)}
    onClose={() => (sheetFor = null)}
  />
{/if}
{#if detailsOpen && hasDetails}
  <ChannelSheet
    {title}
    {kind}
    {topic}
    {onPins}
    {onThreads}
    onCopyLink={details?.onCopyLink}
    onCopyInvite={details?.onCopyInvite}
    notify={details?.notify}
    onNotify={details?.onNotify}
    onClose={() => (detailsOpen = false)}
  />
{/if}
{#if draftEmoji}
  <EmojiPicker anchor={draftEmoji} onPick={insertEmoji} onClose={() => ((draftEmoji = null), messageInput?.focus())} />
{/if}
{#if draftSticker}
  <StickerPicker anchor={draftSticker} {stickers} onPick={sendSticker} onClose={() => ((draftSticker = null), messageInput?.focus())} />
{/if}
{#if allEmoji && allEmojiFor}
  {@const { id, anchor } = allEmoji}
  {@const m = allEmojiFor}
  <EmojiPicker
    {anchor}
    label="Add a reaction"
    selected={(emoji) => mineOf(m, emoji)}
    onPick={(emoji) => react(m, id, emoji)}
    onClose={() => ((allEmoji = null), inLine(id, '[aria-label="More emoji"]')?.focus())}
  />
{/if}
{#if reactionsFor && reactionsMessage?.reactions?.length}
  <ReactionsSheet
    reactions={reactionsMessage.reactions}
    start={reactionsFor.start}
    emojiMenu={(emoji) =>
      hasEmojiMenu(emoji) ? () => ((reactionsFor = null), (emojiMenu = { x: innerWidth / 2, y: innerHeight / 2, emoji })) : null}
    onClose={() => (reactionsFor = null)}
  />
{/if}
{#if infoMessage}
  <MessageInfo
    author={infoMessage.actorName ?? "the server"}
    rows={infoRows({ at: infoMessage.at, edited: infoMessage.edited, pinned: infoMessage.pinned }, undefined, settings.value.timeFormat)}
    reactions={infoMessage.reactions ?? []}
    onClose={closeInfo}
  />
{/if}
{#if pollForm && onPoll}
  <PollForm onSubmit={onPoll} onClose={() => ((pollForm = false), messageInput?.focus())} />
{/if}
</div>
{#if notice.n}
  {#key notice.n}<Toast text={notice.text} />{/key}
{/if}
{#if lightbox}
  <Lightbox {...lightbox} onClose={() => (lightbox = null)} />
{/if}

{#snippet lines(group: MessageGroup)}
  {#each group.messages as m, i (messageKey(m))}
    <li class="line" class:first={i === 0} class:mentioned={pingsMe(pingText(m), group.actorName, replyPing(m as DisplayMessage), me, allOf(m))}
      class:flash={idOf(m) !== undefined && idOf(m) === flashed}
      class:sheet-open={sheetMessage === m}
      use:hold={() => openSheet(m)}
      data-message={idOf(m)}>
      {#if i === 0}
        <div class="gutter">
          {#if group.actorName}
            <Avatar name={group.actorName} size={40} registered={"verified" in m && m.verified === true} />
          {:else}
            <span class="system-avatar"></span>
          {/if}
        </div>
        <div class="body">
          <div class="meta">
            <span class="author">{#if group.actorName}<UserName name={group.actorName} />{:else}Server{/if}</span>
            {#if "verified" in m && m.verified}
              <span class="verified" title="Registered on the server">✓<span class="sr-only">registered</span></span>
            {/if}
            <time datetime={new Date(m.at).toISOString()}>{formatTimestamp(m.at, now, undefined, settings.value.timeFormat)}</time>
          </div>
          {@render body(m)}
        </div>
      {:else}
        <div class="gutter">
          <time class="hover-time" datetime={new Date(m.at).toISOString()}>{formatTime(m.at, undefined, settings.value.timeFormat)}</time>
        </div>
        <div class="body">
          {@render body(m)}
        </div>
      {/if}
      {@render actions(m)}
    </li>
  {/each}
{/snippet}

{#snippet body(m: ChatMessage | DisplayMessage)}
  {#if "reply" in m && m.reply}
    {@const quote = m.reply}
    <button type="button" class="quote" onclick={() => quote.author && (seeking = quote.id)}>
      {#if blockedUsers.has(quote.author)}<em>A message from a blocked user.</em>
      {:else if quote.author}<strong>{quote.author}</strong> <span class="ellipsis">{previewText(quote.text)}</span>
      {:else}<em>The original message was deleted.</em>{/if}
    </button>
  {/if}
  {#if "source" in m && m.source && editing?.id === m.source.id}
    <form class="edit" onsubmit={(e) => (e.preventDefault(), saveEdit())}>
      <textarea
        rows="1"
        bind:this={editInput}
        bind:value={editing.text}
        use:autosize={editing.text}
        aria-label="Edit message"
        onkeydown={(e) => {
          if (e.isComposing || e.keyCode === 229) return;
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            saveEdit();
          } else if (e.key === "Escape") {
            editing = null;
            messageInput?.focus();
          }
        }}
      ></textarea>
      <!-- Servers before several files cannot take files off. -->
      {#if editing.files.length && maxUploads > 1}
        <ul class="edit-files" aria-label="Files">
          {#each editing.files as f (f.url)}
            {@const gone = editing.removed.includes(f.url)}
            <li class:gone>
              <span class="ellipsis">{f.name}</span>
              <button type="button" onclick={() => toggleRemoved(f.url)} aria-label="{gone ? 'Keep' : 'Remove'} {f.name}">{gone ? "Undo" : "×"}</button>
            </li>
          {/each}
        </ul>
      {/if}
      <span class="hint">Escape to cancel, Enter to save, Shift+Enter for a new line</span>
    </form>
  {:else if "poll" in m && m.poll && "source" in m && m.source}
    {@const id = m.source.id}
    <PollView
      poll={m.poll}
      onVote={canReply && !cannotWrite && onVote && myId !== null ? (answers) => onVote(id, answers) : undefined}
      onEnd={canReply && !cannotWrite && onEndPoll && myId !== null && "authorId" in m && m.authorId === myId ? () => onEndPoll(id) : undefined}
    />
  {:else if "sticker" in m && m.sticker}
    <!-- A deleted sticker (not in the list) shows its name. -->
    {@const st = stickerById.get(m.sticker.id)}
    {#if st}
      <img class="sticker" src={st.url} alt={st.name} title={st.description ? `${st.name}: ${st.description}` : st.name} />
    {:else}
      <div class="text gone-sticker">{stickerText(m.sticker.name)}</div>
    {/if}
  {:else if m.text}
    {@const key = messageKey(m)}
    {@const long = isLong(m)}
    {@const open = expanded.has(key)}
    <!-- A click on a mention pill opens that user's menu: a mouse shortcut
         (keyboard users have the member list). -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="text"
      class:jumbo={jumboEmoji("source" in m && m.source ? m.source.text : m.text, custom)}
      class:collapsed={long && !open}
      id={long ? `${listId}-${key}` : undefined}
      use:measure={"source" in m && m.source ? null : key}
      onfocusin={(e) => long && !open && toggleLong(key, e.currentTarget.closest("li"), true)}
      onclick={(e) => {
        // Not in a hidden spoiler: that click shows it.
        const custom = (e.target as Element).closest?.("img.emoji.custom[data-emoji]");
        if (custom instanceof HTMLElement && !custom.closest(".spoiler:not(.shown)") && openEmojiMenu(custom.dataset.emoji!, e)) return e.stopPropagation();
        const pill = (e.target as Element).closest?.(".mention");
        const name = pill?.textContent?.slice(1) ?? "";
        if (pill instanceof HTMLElement && onMention && name !== "everyone" && name !== "here") onMention(name, pill, e);
      }}
    >
      {@html safe(highlightMentions(m.text, me, allOf(m), knownNames), emojiImages.ok, custom)}
      {#if "edited" in m && m.edited}<span class="edited" title={new Date(m.edited).toLocaleString()}>(edited)</span>{/if}
      <!-- Styled as the edited mark. -->
      {#if "pinned" in m && m.pinned}<span class="edited">(pinned)</span>{/if}
    </div>
    {#if long}
      <button
        type="button"
        class="more"
        aria-expanded={open}
        aria-controls="{listId}-{key}"
        aria-label={open ? undefined : "Show more of the message"}
        onclick={(e) => toggleLong(key, e.currentTarget.closest("li"))}>{open ? "Show less" : "Show more"}</button
      >
    {/if}
  {/if}
  {@render attachment(m)}
  {#if "source" in m && m.source && previewOf?.(m.source.id)}
    {@const p = previewOf(m.source.id)!}
    <a class="preview" href={p.url} target="_blank" rel="noopener noreferrer">
      <span class="preview-text">
        {#if p.site}<span class="preview-site ellipsis">{p.site}</span>{/if}
        <span class="preview-title">{p.title}</span>
        {#if p.description}<span class="preview-description">{p.description}</span>{/if}
      </span>
      <!-- The server may have let the image go (its cache): then none. -->
      {#if p.image}<img src={httpBase + p.image} alt="" loading="lazy" onerror={(e) => e.currentTarget.remove()} />{/if}
    </a>
  {/if}
  {#if "reactions" in m && m.reactions?.length && "source" in m && m.source}
    {@const id = m.source.id}
    <div class="reactions">
      {#each m.reactions as r (r.emoji)}
        {@const mine = reactedBy(r, myId)}
        <button
          type="button"
          class="reaction"
          class:mine
          aria-pressed={mine}
          aria-label="{refsAsNames(r.emoji)} {r.users.length}, from {r.users.join(', ')}"
          title={r.users.join(", ")}
          aria-disabled={!canReact}
          data-own-press
          use:longPress
          oncontextmenu={(e) => {
            // A long press (ours is not trusted; a touch screen's own): who
            // reacted, as in Discord, also for who cannot react. A
            // right-click: the emoji's menu.
            if (e.isTrusted && !matchMedia("(hover: none)").matches) return openEmojiMenu(r.emoji, e);
            e.preventDefault();
            reactionsFor = { id, start: r.emoji };
          }}
          onclick={() => canReact && onReact?.(id, r.emoji, !mine)}><span class="emoji"><Emoji text={r.emoji} /></span>{r.users.length}</button
        >
      {/each}
    </div>
  {/if}
  {#if "source" in m && m.source && threadOf?.(m.source.id)}
    {@const t = threadOf(m.source.id)!}
    <button type="button" class="thread-box" onclick={() => onOpenThread?.(t)}>
      <Icon name="thread" size={16} />
      <strong class="ellipsis">{t.name}</strong>
      <span class="ellipsis">{t.archived ? "Archived · " : ""}{replyBoxText(t, now)}</span>
    </button>
  {/if}
{/snippet}

{#snippet actions(m: ChatMessage | DisplayMessage)}
  {@const may = allowed(m)}
  {#if may}
    {@const { id, canEdit, canDelete, canPin, pinned } = may}
    <!-- Info is there for every stored message (read-only too). -->
    {#if canEdit || canDelete || canReply || canReact || canPin || linkTo || id}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="actions"
        class:confirming={confirming === id || picking === id}
        class:picking={picking === id}
        onkeydown={(e) => {
          if (e.key === "Escape" && confirming === id) endConfirm();
          if (e.key === "Escape" && picking === id) void closePicker();
        }}
      >
        {#if picking === id}
          {#each quick.first as emoji (emoji)}
            <button
              type="button"
              class="emoji"
              class:mine={mineOf(m, emoji)}
              aria-label="React with {refsAsNames(emoji)}"
              onclick={() => react(m, id, emoji)}><Emoji text={emoji} /></button
            >
          {/each}
          <button
            type="button"
            aria-label="More emoji"
            title="More emoji"
            onclick={(e) => (allEmoji = { id, anchor: e.currentTarget.getBoundingClientRect() })}>+</button
          >
          <button type="button" aria-label="Close" onclick={closePicker}>×</button>
          {#each quick.second as emoji (emoji)}
            <button
              type="button"
              class="emoji"
              class:mine={mineOf(m, emoji)}
              aria-label="React with {refsAsNames(emoji)}"
              onclick={() => react(m, id, emoji)}><Emoji text={emoji} /></button
            >
          {/each}
        {:else if confirming === id}
          <span>Delete this message?</span>
          <button type="button" class="danger" onclick={() => (onDelete?.(id), endConfirm())}>Delete</button>
          <button type="button" bind:this={cancelButton} onclick={endConfirm}>Cancel</button>
        {:else}
          {#if canReact}
            <button type="button" aria-label="Add a reaction" title="React" onclick={() => openPicker(id)}>React</button>
          {/if}
          {#if canReply}
            <button type="button" aria-label="Reply" title="Reply" onclick={() => startReply(m)}>Reply</button>
          {/if}
          {#if canThread(id)}
            <button type="button" aria-label="Create thread" title="Create thread" onclick={() => onCreateThread?.(id, "source" in m && m.source ? refsAsNames(m.source.text) : "")}
              >Thread</button
            >
          {/if}
          {#if canPin}
            <button type="button" aria-label={pinned ? "Unpin message" : "Pin message"} title={pinned ? "Unpin" : "Pin"} onclick={() => onPin?.(id, !pinned)}
              >{pinned ? "Unpin" : "Pin"}</button
            >
          {/if}
          {#if linkTo}
            <button type="button" aria-label="Copy link to message" title="Copy link" onclick={() => copy(linkTo(id), "Link copied")}>Link</button>
          {/if}
          <button type="button" aria-label="Message info" title="Sent, edited, reactions" onclick={() => showInfo(m)}>Info</button>
          {#if canEdit}
            <button type="button" aria-label="Edit message" title="Edit" onclick={() => startEdit(m)}>Edit</button>
          {/if}
          {#if canDelete}
            <button type="button" aria-label="Delete message" title="Delete" onclick={() => startConfirm(id)}>Delete</button>
          {/if}
        {/if}
      </div>
    {/if}
  {/if}
{/snippet}

<!-- The picture's box before it loads (see imageBox); the loaded picture's
     own ratio wins ("auto"). A large picture shows its preview, else the
     picture itself (also when the preview fails); one the browser cannot
     show at all is a file card. -->
<!-- fill: the picture fills its grid cell (the cell keeps the size). -->
{#snippet picture(u: Attachment, alt: string, fill = false)}
  {@const box = fill ? null : imageBox(u)}
  <img
    src={chatSrc(u)}
    {alt}
    loading="lazy"
    style:width={box && `${box.width}px`}
    style:aspect-ratio={box && `auto ${u.width} / ${u.height}`}
    onload={keepBottom}
    onerror={(e) => {
      const img = e.currentTarget as HTMLImageElement;
      const next = afterImageError(img.getAttribute("src"), u);
      if (next) img.src = next;
      else unplayable = new Set([...unplayable, u.url]);
    }}
  />
{/snippet}

<!-- Two or more pictures as a grid (see lib/fileGrid), the other files
     below it; the viewer goes through the grid's pictures. -->
{#snippet attachment(m: ChatMessage | DisplayMessage)}
  {@const files = ("uploads" in m && m.uploads) || []}
  {@const split = splitFiles(files, (u) => attachmentView(u, unplayable.has(u.url)))}
  {#if split.grid.length}
    {@const cells = gridCells(split.grid.length)}
    <div class="file-grid">
      {#each split.grid as upload, i (upload.url)}
        <div class="cell" class:wide={cells[i]!.cols === 6} class:tall={cells[i]!.rows > 1} style:grid-column="span {cells[i]!.cols}" style:grid-row="span {cells[i]!.rows}">
          {#if upload.spoiler && !shownFiles.has(upload.url)}
            <button type="button" class="spoiler-file" aria-label="Spoiler, show the attachment" onclick={() => showHidden(upload)}>
              {@render picture(upload, "", true)}
              <span>Spoiler</span>
            </button>
          {:else}
            {@render imageLink(upload, split.grid, i)}
            {#if upload.spoiler}
              <button type="button" class="rehide" onclick={() => hideAgain(upload)}>Hide</button>
            {/if}
          {/if}
        </div>
      {/each}
    </div>
  {/if}
  {#each split.rest as upload (upload.url)}
    {@const view = attachmentView(upload, unplayable.has(upload.url))}
    {#if upload.spoiler && !shownFiles.has(upload.url)}
      <button type="button" class="spoiler-file" aria-label="Spoiler, show the attachment" onclick={() => showHidden(upload)}>
        {#if view === "image"}{@render picture(upload, "")}{/if}
        <span>Spoiler</span>
      </button>
    {:else if upload.spoiler && (view === "image" || view === "video")}
      <!-- A shown spoiler can be hidden again. -->
      <div class="unspoiled">
        {@render uploadView(upload)}
        <button type="button" class="rehide" onclick={() => hideAgain(upload)}>Hide</button>
      </div>
    {:else}
      {@render uploadView(upload)}
    {/if}
  {/each}
{/snippet}

<!-- A picture that opens the viewer, at its place in `all`. -->
{#snippet imageLink(upload: Shown, all: readonly Shown[], at: number)}
  <a
    class="image"
    href={lightboxOf(upload).url}
    target="_blank"
    rel="noopener noreferrer"
    aria-label="View {upload.description || upload.name}"
    onclick={(e) => {
      if (!opensLightbox(e)) return;
      e.preventDefault();
      // Not the spoilers still hidden.
      const shown = all.filter((u, i) => i === at || !u.spoiler || shownFiles.has(u.url));
      lightbox = { items: shown.map(viewerItem), start: shown.indexOf(upload) };
    }}
  >
    {@render picture(upload, upload.description || upload.name, all.length > 1)}
  </a>
{/snippet}

{#snippet uploadView(upload: Shown)}
  {@const view = attachmentView(upload, unplayable.has(upload.url))}
  {#if view === "image"}
    {@render imageLink(upload, [upload], 0)}
  {:else if view !== "file"}
    {@const failed = () => (unplayable = new Set([...unplayable, upload.url]))}
    {#if view === "audio"}
      <AudioPlayer url={upload.url} name={upload.name} size={upload.size} length={upload.duration} onError={failed} />
    {:else}
      <VideoPlayer url={upload.url} poster={upload.preview} name={upload.name} size={upload.size} width={upload.width} height={upload.height} length={upload.duration} onLoad={keepBottom} onError={failed} />
    {/if}
  {:else}
    <a class="file" href={upload.url} rel="noopener noreferrer">
      <span class="file-name">{upload.name}</span>
      <span class="file-size">{formatSize(upload.size)}</span>
    </a>
  {/if}
{/snippet}

<style>
  .overlays,
  .search-wrap {
    display: contents;
  }
  .chat {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--bg-main);
  }
  .search {
    position: relative; /* for the suggestion list */
    display: flex;
    align-items: center;
  }
  .search input {
    width: 160px;
    height: 28px;
    min-height: 0;
    padding: 4px 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    font-size: 13px;
  }
  header {
    display: flex;
    flex-shrink: 0;
    align-items: center;
    gap: 8px;
    height: 48px;
    padding: 0 8px 0 16px;
    border-bottom: 1px solid var(--border);
    color: var(--text-faint);
  }
  /* The title and topic give way when the header gets tight, not the icons. */
  header > :global(svg),
  .at {
    flex-shrink: 0;
  }
  .at {
    font-size: 22px;
    font-weight: 600;
  }
  h1 {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
    color: var(--text);
  }
  .divider-v {
    width: 1px;
    height: 24px;
    margin: 0 4px;
    background: var(--border-subtle);
  }
  .topic {
    color: var(--text-muted);
    font-size: 14px;
  }
  .menu,
  .back {
    display: inline-flex;
    margin-right: 4px;
    padding: 10px;
    color: var(--text-muted);
  }
  .hold {
    margin: 0 16px 8px;
    padding: 14px;
    border-radius: 8px;
    background: var(--bg-input);
    color: var(--text);
    font-weight: 600;
    touch-action: none;
    user-select: none;
  }
  .hold.held {
    background: var(--accent-strong);
    color: white;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-left: auto;
  }
  .tools button {
    display: inline-flex;
    padding: 4px;
    border-radius: 6px;
    color: var(--text-muted);
  }
  .tools button:hover,
  .tools button.on {
    color: var(--text);
  }
  .x {
    width: 22px;
    font-size: 24px;
    line-height: 22px;
  }
  .scroller {
    flex: 1;
    overflow-y: auto;
    padding-bottom: 16px;
  }
  .intro {
    padding: 32px 16px 8px;
  }
  .intro-icon {
    display: grid;
    width: 68px;
    height: 68px;
    place-items: center;
    border-radius: 50%;
    background: var(--bg-active);
    color: var(--text);
    font-size: 36px;
    font-weight: 600;
  }
  h2 {
    margin: 8px 0;
    font-size: 30px;
    font-weight: 700;
  }
  .intro p {
    margin: 0;
    color: var(--text-muted);
  }
  .welcome {
    margin-top: 16px;
    padding: 12px 16px;
    border-left: 4px solid var(--accent);
    border-radius: 4px;
    background: var(--bg-sidebar);
    color: var(--text-muted);
  }
  .older {
    display: flex;
    justify-content: center;
    padding: 16px;
  }
  .older button {
    padding: 6px 14px;
    border-radius: 6px;
    background: var(--bg-input);
    color: var(--text-muted);
    font-size: 14px;
  }
  .older button:hover {
    color: var(--text);
  }
  .messages {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .divider {
    display: flex;
    align-items: center;
    margin: 24px 16px 8px;
    color: var(--text-faint);
    font-size: 12px;
    font-weight: 600;
  }
  .divider::before,
  .divider::after {
    flex: 1;
    height: 1px;
    background: var(--border-subtle);
    content: "";
  }
  .divider span {
    padding: 0 8px;
  }
  .line {
    display: flex;
    padding: 1px 48px 1px 0;
  }
  .line.first {
    margin-top: 16px;
  }
  .line:hover {
    background: rgb(0 0 0 / 0.08);
  }
  .gutter {
    display: flex;
    flex-shrink: 0;
    justify-content: center;
    width: 72px;
    padding-top: 2px;
  }
  .hover-time {
    visibility: hidden;
    font-size: 11px;
    line-height: 22px;
    color: var(--text-faint);
  }
  .line:hover .hover-time {
    visibility: visible;
  }
  .system-avatar {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: var(--bg-active);
  }
  .body {
    min-width: 0;
  }
  .meta {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .author {
    font-size: 16px;
    font-weight: 600;
    color: var(--text);
  }
  .verified {
    color: var(--accent);
    font-size: 13px;
    font-weight: 700;
  }
  .meta time {
    font-size: 12px;
    color: var(--text-faint);
  }
  .text {
    font-size: 16px;
    line-height: 1.375;
    overflow-wrap: anywhere;
  }
  .text :global(a) {
    color: var(--link);
  }
  /* A click opens its menu (see EmojiMenu). */
  .text :global(img.emoji.custom) {
    cursor: pointer;
  }
  /* Only emoji: large, as in Discord (48px). */
  .text.jumbo :global(img.emoji) {
    width: 3em;
    height: 3em;
    vertical-align: bottom;
  }
  .text :global(img:not(.emoji)) {
    max-width: 400px;
    border-radius: 8px;
  }
  /* Markdown (see lib/markdown). */
  .text :global(code) {
    padding: 1px 4px;
    border-radius: 4px;
    background: var(--bg-rail);
    font-family: ui-monospace, "SF Mono", Menlo, Consolas, monospace;
    font-size: 0.875em;
  }
  .text :global(pre) {
    margin: 4px 0;
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--bg-rail);
    white-space: pre-wrap;
  }
  .text :global(pre code) {
    padding: 0;
    background: none;
  }
  .text :global(blockquote) {
    margin: 2px 0;
    padding-left: 10px;
    border-left: 4px solid var(--border-subtle);
  }
  .text :global(.spoiler) {
    border-radius: 4px;
    background: var(--bg-rail);
  }
  .text :global(.spoiler:not(.shown)) {
    color: transparent;
    cursor: pointer;
    user-select: none;
  }
  .text :global(.spoiler:not(.shown) *) {
    color: transparent;
    visibility: hidden;
  }
  .text :global(.spoiler:not(.shown):hover) {
    background: var(--bg-active);
  }
  /* Long messages (see lib/collapse): the first lines, fading out. */
  .text.collapsed {
    max-height: calc(12 * 1.375em); /* 12 lines */
    overflow: hidden;
    mask-image: linear-gradient(to bottom, #000 calc(100% - 4em), transparent);
  }
  .more {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .more:hover {
    text-decoration: underline;
  }
  .blocked-line {
    padding: 4px 16px 4px 72px;
    color: var(--text-muted);
    font-size: 14px;
  }
  .blocked-line button {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    cursor: pointer;
  }
  .blocked-line button:hover {
    text-decoration: underline;
  }
  .spoiler-file {
    position: relative;
    display: grid;
    min-width: 160px;
    min-height: 80px;
    max-width: 400px;
    margin-top: 4px;
    padding: 0;
    place-items: center;
    border: none;
    border-radius: 8px;
    background: var(--bg-rail);
    overflow: hidden;
    color: inherit;
    cursor: pointer;
  }
  .spoiler-file img {
    grid-area: 1 / 1;
    max-width: 100%;
    max-height: 300px;
    object-fit: contain;
    filter: blur(32px);
  }
  .spoiler-file span {
    grid-area: 1 / 1;
    color: #fff;
    z-index: 1;
    padding: 4px 12px;
    border-radius: 16px;
    background: rgb(0 0 0 / 0.6);
    font-size: 14px;
    font-weight: 700;
    text-transform: uppercase;
  }
  .unspoiled {
    position: relative;
    width: fit-content;
  }
  .rehide {
    position: absolute;
    top: 12px;
    right: 8px;
    z-index: 1;
    padding: 2px 10px;
    border: none;
    border-radius: 12px;
    background: rgb(0 0 0 / 0.6);
    color: #fff;
    font: inherit;
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    cursor: pointer;
  }
  .rehide:hover {
    background: rgb(0 0 0 / 0.8);
  }
  .pending-spoiler {
    padding: 2px 8px;
    border: 1px solid var(--border-subtle);
    border-radius: 4px;
    background: none;
    color: var(--text-muted);
    font: inherit;
    font-size: 13px;
    cursor: pointer;
  }
  .pending-spoiler[aria-pressed="true"] {
    border-color: transparent;
    background: var(--accent-strong);
    color: #fff;
  }
  .present {
    position: absolute;
    right: 16px;
    bottom: calc(100% + 4px);
    left: 16px;
    z-index: 4;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 4px 4px 4px 12px;
    border-radius: 8px;
    background: var(--accent-strong);
    color: #fff;
    font-size: 14px;
  }
  .present button {
    padding: 4px 10px;
    border: none;
    border-radius: 6px;
    background: rgb(255 255 255 / 0.15);
    color: inherit;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .present button:hover {
    background: rgb(255 255 255 / 0.25);
  }
  .send {
    position: relative; /* for the "Jump to present" bar */
    flex-shrink: 0;
    padding: 0 16px;
  }
  /* On phones, reserve a row below the input so typing does not shift it. */
  .typing {
    height: 24px;
    margin: 0;
    font-size: 13px;
    line-height: 24px;
    color: var(--text-muted);
  }
  /* But with the keyboard open (lib/keyboard.ts) space counts more. */
  :global(html[data-keyboard]) .typing.idle {
    height: 4px;
  }
  .error {
    margin: 0 0 8px;
    color: var(--danger);
    font-size: 14px;
  }
  .plus-menu {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    z-index: 5;
    display: flex;
    flex-direction: column;
    min-width: 200px;
    padding: 6px;
    border-radius: 6px;
    background: var(--bg-rail);
    box-shadow: 0 4px 16px rgb(0 0 0 / 0.4);
  }
  .plus-menu button {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    border-radius: 4px;
    color: var(--text);
    text-align: left;
  }
  .plus-menu button:hover:not(:disabled),
  .plus-menu button:focus-visible {
    background: var(--bg-hover, rgb(255 255 255 / 0.06));
  }
  .plus-menu button:disabled {
    opacity: 0.5;
  }
  .composer {
    position: relative;
    display: flex;
    align-items: flex-end;
    gap: 4px;
    padding: 0 12px 0 4px;
    border-radius: 8px;
    background: var(--bg-input);
  }
  .chat.dragging .scroller {
    outline: 2px dashed var(--accent);
    outline-offset: -8px;
  }
  .line {
    position: relative;
  }
  .actions {
    position: absolute;
    top: -12px;
    right: 16px;
    display: flex;
    opacity: 0;
    align-items: center;
    gap: 2px;
    padding: 2px;
    border-radius: 6px;
    background: var(--bg-rail);
    box-shadow: 0 2px 8px rgb(0 0 0 / 0.3);
    font-size: 12px;
    z-index: 2;
  }
  /* Two rows of quick reactions: 8, "+" and "×", then 10. */
  .actions.picking {
    display: grid;
    grid-template-columns: repeat(10, auto);
  }
  .line:hover .actions,
  .line:focus-within .actions,
  .actions.confirming {
    opacity: 1;
  }
  /* Touch screens: a long press opens the actions (MessageSheet); no
     text selection, "Copy text" is there instead. */
  @media (hover: none) {
    .actions {
      display: none;
    }
    /* Except the question after an edit that leaves nothing (saveEdit). */
    .actions.confirming {
      display: flex;
    }
    .line {
      -webkit-touch-callout: none;
      -webkit-user-select: none;
      user-select: none;
    }
    .line textarea {
      -webkit-user-select: text;
      user-select: text;
    }
    .line.sheet-open {
      background: var(--bg-active);
    }
  }
  .actions span {
    padding: 0 6px;
  }
  .actions button {
    padding: 4px 8px;
    border-radius: 4px;
    color: var(--text);
  }
  .actions button:hover {
    background: var(--bg-active);
  }
  .actions .danger {
    color: var(--danger);
  }
  .actions .emoji {
    padding: 2px 4px;
    font-size: 18px;
  }
  .actions .emoji.mine {
    background: var(--bg-active);
  }
  .preview {
    display: flex;
    gap: 12px;
    max-width: 480px;
    margin-top: 6px;
    padding: 10px 12px;
    border-left: 4px solid var(--accent);
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    text-decoration: none;
  }
  .preview-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .preview-site {
    font-size: 12px;
    color: var(--text-muted);
  }
  .preview-title {
    font-weight: 600;
    color: var(--link);
  }
  .preview:hover .preview-title {
    text-decoration: underline;
  }
  .preview-description {
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    font-size: 14px;
    color: var(--text-muted);
  }
  .preview img {
    flex-shrink: 0;
    width: 80px;
    height: 80px;
    border-radius: 4px;
    object-fit: cover;
  }
  .thread-box {
    display: flex;
    align-items: center;
    gap: 8px;
    max-width: min(480px, 100%);
    margin-top: 4px;
    padding: 6px 10px;
    border-radius: 6px;
    background: var(--bg-sidebar);
    color: var(--text-muted);
    font-size: 13px;
    text-align: left;
  }
  .thread-box strong {
    color: var(--text);
    flex-shrink: 0;
    max-width: 50%;
  }
  .thread-box:hover {
    background: var(--bg-active);
    color: var(--text);
  }
  .reactions {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }
  .reaction {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 8px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: var(--bg-input);
    color: var(--text-muted);
    font-size: 14px;
    font-weight: 600;
  }
  .reaction:not([aria-disabled="true"]):hover {
    border-color: var(--border-subtle);
  }
  .reaction.mine {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--text);
  }
  .reaction .emoji {
    font-size: 16px;
  }
  .edited {
    margin-left: 4px;
    font-size: 11px;
    color: var(--text-muted);
  }
  .edit-files {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 6px 0 0;
    padding: 0;
    list-style: none;
  }
  .edit-files li {
    display: flex;
    align-items: center;
    gap: 6px;
    max-width: 240px;
    padding: 4px 4px 4px 10px;
    border-radius: 6px;
    background: var(--bg-sidebar);
    font-size: 13px;
  }
  .edit-files li.gone span {
    color: var(--text-muted);
    text-decoration: line-through;
  }
  .edit-files button {
    padding: 0 6px;
    color: var(--text-muted);
  }
  .edit-files button:hover {
    color: var(--text);
  }
  .edit {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .edit textarea {
    max-height: 40vh;
    resize: none;
    padding: 8px 10px;
    border: none;
    border-radius: 6px;
    background: var(--bg-input);
    color: var(--text);
    font: inherit;
  }
  .edit .hint {
    font-size: 11px;
    color: var(--text-muted);
  }
  .read-only {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--bg-input);
    color: var(--text-muted);
  }
  .read-only button {
    padding: 6px 12px;
    border-radius: 4px;
    background: var(--accent-strong);
    color: white;
    font-weight: 600;
  }
  .replying {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 12px;
    margin: 0 0 4px;
    border-radius: 8px 8px 0 0;
    background: var(--bg-rail);
    font-size: 13px;
    color: var(--text-muted);
  }
  .replying button {
    font-size: 18px;
    line-height: 1;
    color: var(--text-muted);
  }
  .replying .ping {
    margin-left: auto;
    font-size: 13px;
    font-weight: 700;
    color: var(--accent);
  }
  .replying .ping.off {
    color: var(--text-muted);
  }
  .quote {
    display: flex;
    gap: 6px;
    max-width: 100%;
    margin-bottom: 2px;
    padding: 0 0 0 8px;
    border-left: 2px solid var(--border-subtle);
    font-size: 13px;
    color: var(--text-muted);
    text-align: left;
    cursor: pointer;
  }
  .quote strong {
    color: var(--text);
    flex: none;
  }
  .line.flash {
    background: rgb(88 101 242 / 0.2);
    transition: background 0.3s;
  }
  .line.mentioned {
    background: rgb(240 178 50 / 0.1);
    box-shadow: inset 2px 0 0 rgb(240 178 50);
  }
  /* Mentions as pills (as in Discord): blue, mine yellow. */
  .text :global(.mention) {
    padding: 0 2px;
    border-radius: 3px;
    background: rgb(88 101 242 / 0.22);
    color: var(--link);
    font-weight: 600;
    cursor: pointer;
  }
  .text :global(.mention.me) {
    background: rgb(240 178 50 / 0.25);
    color: inherit;
  }
  .image {
    display: inline-block;
    margin-top: 4px;
  }
  /* 6 columns (see lib/fileGrid gridCells); cells keep their size while
     the pictures load. */
  .file-grid {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 4px;
    width: min(520px, 100%);
    margin-top: 4px;
    border-radius: 8px;
    overflow: hidden;
  }
  .cell {
    position: relative;
    aspect-ratio: 1;
    min-width: 0;
    background: var(--bg-rail);
  }
  .cell.wide {
    aspect-ratio: 2;
  }
  .cell.tall {
    aspect-ratio: auto;
  }
  .cell .image {
    display: block;
  }
  .cell .image,
  .cell .spoiler-file {
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    max-width: none;
    margin: 0;
    border-radius: 0;
  }
  .cell .image img,
  .cell .spoiler-file img {
    width: 100%;
    height: 100%;
    max-width: none;
    max-height: none;
    border-radius: 0;
    object-fit: cover;
  }
  /* The largest box: in step with imageBox (lib/media). contain: a
     picture whose ratio differs from the server's is not squashed. */
  .image img {
    display: block;
    max-width: min(400px, 100%);
    max-height: 300px;
    border-radius: 8px;
    object-fit: contain;
  }
  .file {
    display: inline-flex;
    flex-direction: column;
    gap: 2px;
    margin-top: 4px;
    padding: 10px 14px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-sidebar);
    color: var(--link);
    text-decoration: none;
  }
  .file-size {
    color: var(--text-faint);
    font-size: 12px;
  }
  .pending {
    margin-bottom: 8px;
    padding: 8px 12px;
    border-radius: 8px;
    background: var(--bg-sidebar);
  }
  /* A row of cards, as in Discord; it scrolls sideways. */
  .pending-files {
    display: flex;
    gap: 8px;
    margin: 0;
    padding: 0;
    overflow-x: auto;
    list-style: none;
  }
  .pending-file {
    position: relative;
    display: flex;
    flex: 0 0 auto;
    flex-direction: column;
    justify-content: flex-end;
    gap: 2px;
    width: 140px;
    min-height: 96px;
    padding: 6px;
    border-radius: 6px;
    background: var(--bg-main);
  }
  .pending-file img {
    width: 100%;
    height: 96px;
    border-radius: 4px;
    object-fit: cover;
  }
  .pending-actions {
    position: absolute;
    top: 4px;
    right: 4px;
    display: flex;
    gap: 4px;
  }
  .pending-actions button {
    background: var(--bg-sidebar);
  }
  .pending-actions button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .pending-description {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: 8px;
    font-size: 12px;
    color: var(--text-muted);
  }
  .pending-description textarea {
    padding: 6px 8px;
    border: 1px solid var(--border-subtle);
    border-radius: 4px;
    background: var(--bg-main);
    color: var(--text);
    font: inherit;
    font-size: 14px;
    resize: vertical;
  }
  .pending-status {
    display: block;
    margin-bottom: 6px;
    font-size: 12px;
    color: var(--text-muted);
  }
  .pending-status.failed {
    color: var(--danger);
  }
  .pending-cancel {
    padding: 0 6px;
    border-radius: 4px;
    font-size: 20px;
    line-height: 1;
    color: var(--text-muted);
  }
  .pending-cancel:hover {
    color: var(--text);
  }
  .composer:focus-within {
    outline: 2px solid var(--accent);
  }
  .composer.disabled {
    opacity: 0.6;
  }
  .slow {
    align-self: center;
    padding: 0 6px;
    font-size: 12px;
    white-space: nowrap;
    color: var(--text-muted);
  }
  .attach {
    display: grid;
    width: 24px;
    height: 24px;
    padding: 0; /* the browser's button padding would push the icon aside */
    margin: 10px 8px 10px 8px;
    place-items: center;
    border-radius: 50%;
    background: var(--text-muted);
    color: var(--bg-input);
  }
  /* Uploading: a ring that turns, on the button's place. */
  .attach.busy {
    background: none;
    opacity: 1;
  }
  .spinner {
    width: 20px;
    height: 20px;
    border: 3px solid color-mix(in srgb, var(--text-muted) 35%, transparent);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation-duration: 3s;
    }
  }
  .send-button {
    display: none;
    place-items: center;
    width: 36px;
    height: 36px;
    margin: 4px -8px 4px 0;
    padding: 0;
    border-radius: 50%;
    background: var(--accent);
    color: white;
  }
  @media (pointer: coarse) {
    .send-button {
      display: grid;
    }
  }
  .emoji-button {
    display: grid;
    padding: 0;
    margin: 9px 0;
    place-items: center;
    border: none;
    background: none;
    cursor: pointer;
    filter: grayscale(1) opacity(0.6);
  }
  .sticker {
    display: block;
    max-width: 160px;
    max-height: 160px;
    margin: 2px 0;
    object-fit: contain;
  }
  .gone-sticker {
    color: var(--text-muted);
    font-style: italic;
  }
  .emoji-button:hover,
  .emoji-button:focus-visible {
    filter: none;
  }
  .emoji-button :global(.emoji) {
    width: 24px;
    height: 24px;
  }
  .composer textarea {
    flex: 1;
    min-width: 0;
    max-height: 40vh;
    padding: 11px 0;
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    font-size: 16px;
    line-height: 22px;
    resize: none;
    outline: none;
  }
  input::placeholder,
  .composer textarea::placeholder {
    color: var(--text-faint);
  }
  @media (min-width: 701px) {
    /* Match the user panel's 48px height and 8px bottom inset. Typing floats
       above it; without a typist, history runs right up to the composer. */
    .send {
      padding-bottom: 8px;
    }
    .scroller {
      /* Scrollable end padding keeps the newest message clear of typing
         without reserving a blank strip when reading older messages. */
      padding-bottom: 32px;
    }
    .chat.hasTyping .scroller {
      mask-image: linear-gradient(to bottom, #000 calc(100% - 32px), transparent calc(100% - 24px));
    }
    .typing {
      position: absolute;
      right: 16px;
      bottom: 100%;
      left: 16px;
      pointer-events: none;
    }
    .chat.hasTyping .present {
      bottom: calc(100% + 28px);
    }
    .composer {
      padding-block: 2px;
    }
  }
  /* Phones: touch targets of at least 44 px; a narrower search field,
     wider while typing, with its suggestions as wide as the header. */
  @media (max-width: 700px) {
    header {
      position: relative;
      padding-left: 8px;
    }
    .tools button {
      padding: 11px;
    }
    .search input {
      width: 96px;
    }
    .search input:focus {
      width: 140px;
    }
    .search {
      position: static;
    }
    .search :global(.below) {
      top: calc(100% - 2px);
      right: 8px;
      left: 8px;
    }
    /* The search icon's field, over the whole header. */
    .search.wide {
      flex: 1;
      min-width: 0;
    }
    .search.wide input {
      width: 100%;
      height: 36px;
    }
    /* The name opens the channel sheet. */
    .title {
      display: flex;
      align-items: center;
      gap: 2px;
      max-width: 100%;
      padding: 8px 0;
      font-weight: inherit;
    }
    .title :global(svg) {
      flex-shrink: 0;
      color: var(--text-faint);
    }
  }
  /* The member list is hidden on narrow screens: so is its toggle. */
  @media (max-width: 900px) and (min-width: 701px) {
    .tools .members-toggle {
      display: none;
    }
  }
</style>
