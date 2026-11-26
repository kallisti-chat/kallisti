import { describe, expect, it, vi } from "vitest";
import {
  afterImageError,
  asSpoiler,
  attachmentView,
  chatSrc,
  chatVideo,
  downloadUrl,
  formatDuration,
  imageBox,
  lazyVideo,
  lightboxOf,
  mediaKind,
  mediaRatio,
  onlyOne,
  releaseMedia,
  shownLength,
  spoilerUpload,
  videoFullScreen,
  videoPreload,
} from "../src/lib/media";

describe("previews", () => {
  it("show a picture's preview in the chat, else the picture", () => {
    expect(chatSrc({ url: "/files/k/a.png", preview: "/files/k/a.png?preview" })).toBe("/files/k/a.png?preview");
    expect(chatSrc({ url: "/files/k/a.png" })).toBe("/files/k/a.png");
  });

  it("show a large GIF's video like a picture (a picture never gets the video)", () => {
    const gif = { url: "/files/k/a.gif", type: "image/gif", preview: "/files/k/a.gif?preview", previewType: "video/mp4" };
    expect(chatVideo(gif)).toBe("/files/k/a.gif?preview");
    expect(chatSrc(gif)).toBe("/files/k/a.gif");
    expect(chatVideo({ type: "image/png", preview: "/files/k/a.png?preview", previewType: "image/webp" })).toBeNull();
    // A video's preview is its poster, not a video to show.
    expect(chatVideo({ type: "video/mp4", preview: "/files/k/v.mp4?preview", previewType: "image/webp" })).toBeNull();
    expect(chatVideo({ type: "image/gif" })).toBeNull();
  });

  it("give a video a poster and load it only when played", () => {
    expect(videoPreload("/files/k/a.mp4?preview")).toBe("none");
    // Without one: its metadata and first frame.
    expect(videoPreload(undefined)).toBe("metadata");
  });
});

describe("attachments", () => {
  const avif = { type: "image/avif", name: "a.avif", url: "/files/k/a.avif" };

  it("show pictures, play audio and video, and offer the rest as files", () => {
    expect(attachmentView(avif, false)).toBe("image");
    expect(attachmentView({ type: "video/mp4", name: "a.mp4" }, false)).toBe("video");
    expect(attachmentView({ type: "application/octet-stream", name: "song.mp3" }, false)).toBe("audio");
    expect(attachmentView({ type: "application/zip", name: "a.zip" }, false)).toBe("file");
  });

  it("offer a picture or player the browser cannot show as a file", () => {
    expect(attachmentView(avif, true)).toBe("file");
    expect(attachmentView({ type: "video/webm", name: "a.webm" }, true)).toBe("file");
  });

  it("try the picture after a failed preview, then give up", () => {
    expect(afterImageError("/files/k/a.avif?preview", avif)).toBe("/files/k/a.avif");
    expect(afterImageError("/files/k/a.avif", avif)).toBeNull();
    expect(afterImageError(null, avif)).toBeNull();
  });

  // JPEG XL: few browsers show it; the server's preview is its picture.
  const jxl = { type: "image/jxl", name: "a.jxl", url: "/files/k/a.jxl" };
  const jxlWithPreview = { ...jxl, preview: "/files/k/a.jxl?preview" };

  it("show a JPEG XL picture by its preview only", () => {
    expect(attachmentView(jxlWithPreview, false)).toBe("image");
    expect(attachmentView(jxl, false)).toBe("file"); // none (yet)
    expect(attachmentView(jxlWithPreview, true)).toBe("file");
    expect(afterImageError(jxlWithPreview.preview, jxlWithPreview)).toBeNull();
  });

  it("open the preview in the lightbox, with the original as a download", () => {
    expect(lightboxOf(jxlWithPreview)).toEqual({ url: jxlWithPreview.preview, name: "a.jxl", download: "/files/k/a.jxl?download" });
    expect(lightboxOf({ ...avif, preview: "/files/k/a.avif?preview" })).toEqual({ url: avif.url, name: "a.avif" });
  });
});

describe("mediaKind", () => {
  it("plays audio and video by type, and older uploads by name", () => {
    expect(mediaKind({ type: "audio/mpeg", name: "x" })).toBe("audio");
    expect(mediaKind({ type: "video/mp4", name: "x" })).toBe("video");
    expect(mediaKind({ type: "application/octet-stream", name: "02 Song.MP3" })).toBe("audio");
    expect(mediaKind({ type: "application/octet-stream", name: "clip.webm" })).toBe("video");
    expect(mediaKind({ type: "application/octet-stream", name: "notes.txt" })).toBeNull();
    expect(mediaKind({ type: "image/png", name: "a.png" })).toBeNull();
  });
});

describe("formatDuration", () => {
  it("shows minutes and seconds (hours when long)", () => {
    expect(formatDuration(0)).toBe("0:00");
    expect(formatDuration(211.6)).toBe("3:31");
    expect(formatDuration(3725)).toBe("1:02:05");
    expect(formatDuration(NaN)).toBe("0:00");
  });
});

describe("shownLength", () => {
  it("is the player's length, else the server's", () => {
    expect(shownLength(12.5, 3000)).toBe(12.5);
    expect(shownLength(0, 3000)).toBe(3);
    expect(shownLength(Infinity, 3000)).toBe(3); // a recording without a length
    expect(shownLength(NaN)).toBe(0);
  });
});

describe("spoiler attachments", () => {
  it("are marked by name, as in Discord", () => {
    const file = new File(["x"], "cat.png", { type: "image/png" });
    expect(asSpoiler(file, false)).toBe(file);
    const spoiler = asSpoiler(file, true);
    expect([spoiler.name, spoiler.type]).toEqual(["SPOILER_cat.png", "image/png"]);
    expect(asSpoiler(spoiler, true).name).toBe("SPOILER_cat.png");
  });

  it("show without the prefix", () => {
    const u = { url: "/f/1", name: "SPOILER_cat.png", type: "image/png", size: 1 };
    expect(spoilerUpload(u)).toEqual({ ...u, name: "cat.png", spoiler: true });
    expect(spoilerUpload({ ...u, name: "cat.png" })).toEqual({ ...u, name: "cat.png" });
  });
});

describe("media boxes", () => {
  it("keep a picture's size, scaled down to the largest box", () => {
    expect(imageBox({ width: 40, height: 30 })).toEqual({ width: 40, height: 30 });
    expect(imageBox({ width: 1600, height: 900 })).toEqual({ width: 400, height: 225 });
    expect(imageBox({ width: 900, height: 1600 })).toEqual({ width: 169, height: 300 });
    expect(imageBox({ width: 10000, height: 1 })).toEqual({ width: 400, height: 1 });
    expect(imageBox({ width: 640, height: 480 }, { width: 160, height: 300 })).toEqual({ width: 160, height: 120 });
  });

  it("are unknown without a size", () => {
    expect(imageBox({})).toBeNull();
    expect(imageBox({ width: 0, height: 10 })).toBeNull();
    expect(mediaRatio({ width: 10 })).toBeUndefined();
  });

  it("give videos their aspect ratio", () => {
    expect(mediaRatio({ width: 1280, height: 720 })).toBe("1280 / 720");
  });
});

describe("downloadUrl", () => {
  it("asks the server for an attachment", () => {
    expect(downloadUrl("https://x.example/files/abc/clip.mp4")).toBe("https://x.example/files/abc/clip.mp4?download");
    expect(downloadUrl("/files/abc/a.mp4?x=1")).toBe("/files/abc/a.mp4?x=1&download");
  });
});

// A stand-in for an <audio>/<video> element (the tests run without a DOM).
const fakeMedia = () => ({ pause: vi.fn(), load: vi.fn(), removeAttribute: vi.fn() }) as unknown as HTMLMediaElement & Record<"pause" | "load" | "removeAttribute", ReturnType<typeof vi.fn>>;

describe("media players", () => {
  it("play one at a time", () => {
    const a = fakeMedia();
    const b = fakeMedia();
    onlyOne(a);
    onlyOne(b);
    expect(a.pause).toHaveBeenCalledOnce();
    expect(b.pause).not.toHaveBeenCalled();
    releaseMedia(b);
  });

  it("stop and drop their source when they leave the screen, so a media key cannot resume them", () => {
    const a = fakeMedia();
    onlyOne(a);
    releaseMedia(a);
    expect(a.pause).toHaveBeenCalled();
    expect(a.removeAttribute).toHaveBeenCalledWith("src");
    expect(a.load).toHaveBeenCalledAfter(a.removeAttribute);
    // No longer the one that played: the next one does not touch it.
    a.pause.mockClear();
    const b = fakeMedia();
    onlyOne(b);
    expect(a.pause).not.toHaveBeenCalled();
    releaseMedia(b);
  });

  it("keep the one that plays when another leaves", () => {
    const a = fakeMedia();
    const b = fakeMedia();
    onlyOne(a);
    releaseMedia(b);
    onlyOne(b);
    expect(a.pause).toHaveBeenCalledOnce();
    releaseMedia(b);
  });
});

describe("lazyVideo", () => {
  // A stub observer the test drives.
  let seen: ((entries: { isIntersecting: boolean }[]) => void) | null = null;
  class FakeObserver {
    constructor(cb: (entries: { isIntersecting: boolean }[]) => void) {
      seen = cb;
    }
    observe() {}
    disconnect() {
      seen = null;
    }
  }
  const video = (play: () => Promise<void>) => ({ src: "", muted: false, play: vi.fn(play), pause: vi.fn(), removeAttribute: vi.fn(), load: vi.fn() });

  it("loads and plays only near the screen, pauses out of view", async () => {
    vi.stubGlobal("IntersectionObserver", FakeObserver);
    const el = video(async () => {});
    const stop = lazyVideo("/v.mp4", () => {})(el as unknown as HTMLVideoElement);
    expect(el.src).toBe("");
    seen!([{ isIntersecting: true }]);
    expect(el.src).toBe("/v.mp4");
    expect(el.muted).toBe(true);
    expect(el.play).toHaveBeenCalled();
    seen!([{ isIntersecting: false }]);
    expect(el.pause).toHaveBeenCalled();
    stop();
    expect(seen).toBeNull();
    vi.unstubAllGlobals();
  });

  it("a refused play (no autoplay allowed): the GIF instead; an interrupted one: nothing", async () => {
    vi.stubGlobal("IntersectionObserver", FakeObserver);
    const onFail = vi.fn();
    const refused = video(() => Promise.reject(Object.assign(new Error("no"), { name: "NotAllowedError" })));
    lazyVideo("/v.mp4", onFail)(refused as unknown as HTMLVideoElement);
    seen!([{ isIntersecting: true }]);
    await vi.waitFor(() => expect(onFail).toHaveBeenCalledTimes(1));
    const interrupted = video(() => Promise.reject(Object.assign(new Error("paused"), { name: "AbortError" })));
    lazyVideo("/v.mp4", onFail)(interrupted as unknown as HTMLVideoElement);
    seen!([{ isIntersecting: true }]);
    await new Promise((r) => setTimeout(r, 0));
    expect(onFail).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
  });
});

describe("videoFullScreen", () => {
  it("uses the standard full screen", () => {
    const requestFullscreen = vi.fn(() => Promise.resolve());
    videoFullScreen({ requestFullscreen } as unknown as HTMLVideoElement)?.();
    expect(requestFullscreen).toHaveBeenCalled();
  });
  it("uses the video full screen on iPhones (no standard one)", () => {
    const webkitEnterFullscreen = vi.fn();
    videoFullScreen({ webkitEnterFullscreen } as unknown as HTMLVideoElement)?.();
    expect(webkitEnterFullscreen).toHaveBeenCalled();
  });
  it("is null without either (no button)", () => {
    expect(videoFullScreen({} as HTMLVideoElement)).toBeNull();
  });
  it("uses the window's full screen in the Linux app (WebKitGTK keeps a webkitEnterFullscreen that cannot work)", () => {
    const enter = vi.fn();
    const webkitEnterFullscreen = vi.fn();
    videoFullScreen({ webkitEnterFullscreen } as unknown as HTMLVideoElement, () => enter)?.();
    expect(enter).toHaveBeenCalled();
    expect(webkitEnterFullscreen).not.toHaveBeenCalled();
  });
});
