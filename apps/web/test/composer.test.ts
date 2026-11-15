import { describe, expect, it } from "vitest";
import {
  addFiles,
  editOutcome,
  afterUpload,
  canAttach,
  chatKeyOf,
  clipboardImage,
  emptyPaste,
  pastedFiles,
  refuseFileDrop,
  refusedDraft,
  switchDraft,
  keepDraft,
  tooLarge,
  uploadTitle,
} from "../src/lib/composer";

describe("canAttach", () => {
  it("takes a file only where uploads work and none is running", () => {
    expect(canAttach(true, false)).toBe(true);
    expect(canAttach(true, true)).toBe(false);
    expect(canAttach(false, false)).toBe(false);
  });
});

describe("afterUpload", () => {
  const file = new File(["x"], "a.png");
  const sent = { chat: "text 1", caption: "look", files: [file] };
  const none = new Map<string, string>();
  const now = { chat: "text 1", draft: "look ", files: [file], drafts: none };

  it("clears the file and caption after a success", () => {
    expect(afterUpload(true, sent, now)).toEqual({ files: true, caption: true, drafts: none });
  });

  it("keeps both after a failure, for a retry", () => {
    expect(afterUpload(false, sent, now)).toEqual({ files: false, caption: false, drafts: none });
  });

  it("keeps a caption that was changed during the upload", () => {
    expect(afterUpload(true, sent, { ...now, draft: "look at this" }).caption).toBe(false);
  });

  it("keeps the text of another chat", () => {
    const other = { ...now, chat: "text 2", draft: "look", files: [] };
    expect(afterUpload(true, sent, other)).toEqual({ files: false, caption: false, drafts: none });
  });

  it("forgets the sent caption kept for the chat it was sent in", () => {
    const other = { ...now, chat: "text 2", draft: "", files: [] };
    const kept = new Map([["text 1", "look"], ["text 3", "hi"]]);
    expect(afterUpload(true, sent, { ...other, drafts: kept }).drafts).toEqual(new Map([["text 3", "hi"]]));
    expect(afterUpload(false, sent, { ...other, drafts: kept }).drafts).toBe(kept);
    const changed = new Map([["text 1", "look again"]]);
    expect(afterUpload(true, sent, { ...other, drafts: changed }).drafts).toBe(changed);
  });

  it("keeps files changed during the upload", () => {
    expect(afterUpload(true, sent, { ...now, files: [file, new File(["y"], "b.png")] }).files).toBe(false);
  });
});

describe("addFiles", () => {
  const f = (name: string, size = 1) => new File(["x".repeat(size)], name);
  const pending = (file: File) => ({ file, spoiler: false });
  it("adds picked files after the waiting ones", () => {
    const a = pending(f("a"));
    const added = addFiles([a], [f("b"), f("c")], 10, undefined);
    expect(added.files.map((p) => p.file.name)).toEqual(["a", "b", "c"]);
    expect(added.files[0]).toBe(a);
    expect(added.note).toBeNull();
  });
  it("refuses files over the size limit one by one", () => {
    const added = addFiles([], [f("small", 1), f("big", 5), f("ok", 2)], 10, 3);
    expect(added.files.map((p) => p.file.name)).toEqual(["small", "ok"]);
    expect(added.note).toBe("big: the file is too large (at most 0 MB).");
  });
  it("takes at most the server's number of files", () => {
    const added = addFiles([pending(f("a"))], [f("b"), f("c")], 2, undefined);
    expect(added.files.map((p) => p.file.name)).toEqual(["a", "b"]);
    expect(added.note).toBe("At most 2 files per message.");
  });
  it("with one file per message, a new pick takes the place of the waiting file", () => {
    const added = addFiles([pending(f("a"))], [f("b"), f("c")], 1, undefined);
    expect(added.files.map((p) => p.file.name)).toEqual(["b"]);
    expect(added.note).toBeNull();
    expect(added.changed).toBe(true);
    const a = pending(f("a"));
    expect(addFiles([a], [f("big", 9)], 1, 3).changed).toBe(false);
  });
});

describe("chatKeyOf", () => {
  it("tells chats apart by id, private chats by name", () => {
    expect(chatKeyOf({ kind: "text", id: 3 }, 0)).toBe("text 3");
    expect(chatKeyOf({ kind: "voice" }, 5)).not.toBe(chatKeyOf({ kind: "voice" }, 6));
    expect(chatKeyOf({ kind: "dm", session: 2, name: "bob" }, 5)).toBe(chatKeyOf({ kind: "dm", session: null, name: "bob" }, 0));
    expect(chatKeyOf({ kind: "dm", session: 2, name: "5" }, 5)).not.toBe(chatKeyOf({ kind: "voice" }, 5));
  });
});

describe("switchDraft", () => {
  it("keeps the draft of the chat we leave and brings back the other one", () => {
    const left = switchDraft(new Map(), "#general", "#random", "half a thought");
    expect(left).toEqual({ drafts: new Map([["#general", "half a thought"]]), draft: "" });
    const back = switchDraft(left.drafts, "#random", "#general", "");
    expect(back).toEqual({ drafts: new Map(), draft: "half a thought" });
  });

  it("keeps no empty drafts", () => {
    expect(switchDraft(new Map([["#general", "old"]]), "#general", "#random", "  ").drafts).toEqual(new Map());
  });

  it("does not change the map it got", () => {
    const drafts = new Map([["#random", "hi"]]);
    switchDraft(drafts, "#general", "#random", "x");
    expect(drafts).toEqual(new Map([["#random", "hi"]]));
  });

  // The panes come and go (a thread over the channel on phones, a closed
  // thread pane): the drafts are kept above them.
  it("keeps the draft of a pane that closes for the pane that opens next", () => {
    const opened = switchDraft(new Map(), "", "text 1", ""); // a new pane
    const closed = keepDraft(opened.drafts, "text 1", "half a thought");
    expect(switchDraft(closed, "", "text 1", "").draft).toBe("half a thought");
    expect(keepDraft(closed, "text 1", " ")).toEqual(new Map());
  });
});

describe("tooLarge", () => {
  it("names the limit for a file over it", () => {
    expect(tooLarge(10 * 1024 * 1024 + 1, 10 * 1024 * 1024)).toBe("The file is too large (at most 10 MB).");
    expect(tooLarge(10 * 1024 * 1024, 10 * 1024 * 1024)).toBeNull();
  });
  it("lets the server decide without a known limit", () => {
    expect(tooLarge(1e12, undefined)).toBeNull();
  });
});

describe("refusedDraft", () => {
  it("brings a refused text back into an empty box only", () => {
    expect(refusedDraft("", "buy spam")).toBe("buy spam");
    expect(refusedDraft("  ", "buy spam")).toBe("buy spam");
    expect(refusedDraft("new text", "buy spam")).toBe("new text");
  });
});

describe("uploadTitle", () => {
  it("names the server's limit when known", () => {
    expect(uploadTitle(10 * 1024 * 1024)).toBe("Upload a file (up to 10 MB)");
    expect(uploadTitle(undefined)).toBe("Upload a file");
    expect(uploadTitle(10 * 1024 * 1024, 10)).toBe("Upload files (up to 10 MB each)");
    expect(uploadTitle(undefined, 10)).toBe("Upload files");
  });
});

describe("pastedFiles", () => {
  const file = { name: "a.png" } as File;
  const item = (kind: string, f: File | null) => ({ kind, getAsFile: () => f });
  it("takes the files, else the file items (WebKitGTK)", () => {
    expect(pastedFiles({ files: [file], items: [] })).toEqual([file]);
    expect(pastedFiles({ files: [], items: [item("string", null), item("file", file)] })).toEqual([file]);
    expect(pastedFiles({ files: [], items: [item("file", null)] })).toEqual([]);
    expect(pastedFiles(null)).toEqual([]);
  });
});

describe("emptyPaste", () => {
  it("is a paste without files or text (WebKitGTK with an image)", () => {
    expect(emptyPaste({ types: [] })).toBe(true);
    expect(emptyPaste({ types: ["text/html"] })).toBe(true);
    expect(emptyPaste({ types: ["text/plain"] })).toBe(false);
    expect(emptyPaste({ types: ["Files"] })).toBe(false);
    expect(emptyPaste(null)).toBe(true);
  });
});

describe("clipboardImage", () => {
  const blob = new Blob(["png"], { type: "image/png" });
  const item = (types: string[]) => ({ types, getType: async (t: string) => (t === "image/png" ? blob : new Blob([t])) });
  it("takes the first image as a file", async () => {
    const file = await clipboardImage(async () => [item(["text/plain"]), item(["text/html", "image/png"])]);
    expect([file?.name, file?.type, await file?.text()]).toEqual(["pasted.png", "image/png", "png"]);
    expect((await clipboardImage(async () => [item(["image/svg+xml"])]))?.name).toBe("pasted.svg");
    expect((await clipboardImage(async () => [item(["image/gif"]), item(["image/png"])]))?.name).toBe("pasted.gif");
  });
  it("is null without an image or a clipboard", async () => {
    expect(await clipboardImage(async () => [item(["text/plain"])])).toBeNull();
    expect(await clipboardImage(async () => Promise.reject(new Error("denied")))).toBeNull();
    expect(await clipboardImage(() => Promise.resolve([{ types: ["image/png"], getType: () => Promise.reject(new Error()) }]))).toBeNull();
  });
});

describe("editOutcome", () => {
  const files = ["http://srv/files/a/a.png", "http://srv/files/b/b.png"];
  it("saves a changed text or removed files (their keys)", () => {
    expect(editOutcome({ old: "x", text: "y", files, removed: [] })).toEqual({ save: true, remove: [] });
    expect(editOutcome({ old: "x", text: "x", files, removed: [files[1]!] })).toEqual({ save: true, remove: ["b"] });
    expect(editOutcome({ old: "", text: "", files, removed: [files[0]!] })).toEqual({ save: true, remove: ["a"] });
  });
  it("does nothing without a change", () => {
    expect(editOutcome({ old: "x", text: "x", files, removed: [] })).toEqual({ save: false, remove: [] });
  });
  it("asks to delete a message left with no text and no file, as in Discord", () => {
    expect(editOutcome({ old: "x", text: "", files: [], removed: [] })).toEqual({ save: false, remove: [], delete: true });
    expect(editOutcome({ old: "", text: "", files, removed: files })).toEqual({ save: false, remove: [], delete: true });
  });
});

describe("refuseFileDrop", () => {
  const drag = (over: { types?: string[]; defaultPrevented?: boolean; target?: object }) => {
    const e = {
      defaultPrevented: over.defaultPrevented ?? false,
      target: (over.target ?? {}) as EventTarget,
      dataTransfer: { types: over.types ?? ["Files"], dropEffect: "copy" },
      preventDefault: () => void (e.defaultPrevented = true),
    };
    refuseFileDrop(e);
    return { prevented: e.defaultPrevented, effect: e.dataTransfer.dropEffect };
  };

  it("refuses a file outside a chat, so the browser does not open it", () => {
    expect(drag({})).toEqual({ prevented: true, effect: "none" });
  });

  it("leaves a drop the chat took, file inputs and dragged text alone", () => {
    expect(drag({ defaultPrevented: true }).effect).toBe("copy");
    expect(drag({ target: { type: "file" } })).toEqual({ prevented: false, effect: "copy" });
    expect(drag({ types: ["text/plain"] })).toEqual({ prevented: false, effect: "copy" });
  });
});
