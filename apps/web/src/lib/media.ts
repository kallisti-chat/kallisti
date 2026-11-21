// Inline audio and video attachments.

export type MediaKind = "audio" | "video";

// In step with the server's files/media.go.
const AUDIO = /\.(mp3|m4a|ogg|oga|opus|wav|flac)$/i;
const VIDEO = /\.(mp4|m4v|webm)$/i;

// By type; uploads from before the server kept media types are downloads,
// so their name decides (the server serves them with the right type).
export const mediaKind = (u: { type: string; name: string }): MediaKind | null => {
  if (u.type.startsWith("audio/")) return "audio";
  if (u.type.startsWith("video/")) return "video";
  if (u.type !== "application/octet-stream") return null;
  return AUDIO.test(u.name) ? "audio" : VIDEO.test(u.name) ? "video" : null;
};

// Pictures few browsers show (JPEG XL): the server serves them as
// downloads and shows them by its preview only (as files.previewedTypes).
const previewOnly = (u: { type: string }) => u.type === "image/jxl";

// How an attachment shows: a picture, a player, or a file card (also a
// picture or player the browser could not load: AVIF in an old WebView).
export const attachmentView = (u: { type: string; name: string; preview?: string }, failed: boolean): "image" | MediaKind | "file" =>
  failed || (previewOnly(u) && !u.preview) ? "file" : u.type.startsWith("image/") ? "image" : (mediaKind(u) ?? "file");

// The source to try after a picture failed to load from src: the picture
// after its preview; null: none, show the file card.
export const afterImageError = (src: string | null, u: { type: string; url: string }) =>
  src !== null && src !== u.url && !previewOnly(u) ? u.url : null;

// What the lightbox shows: the picture, or the preview of one shown only
// by it, with the original to download.
export const lightboxOf = (u: { type: string; name: string; url: string; preview?: string }) =>
  previewOnly(u) && u.preview ? { url: u.preview, name: u.name, download: downloadUrl(u.url) } : { url: u.url, name: u.name };

export const formatDuration = (seconds: number) => {
  const s = Number.isFinite(seconds) ? Math.floor(seconds) : 0;
  const mm = String(Math.floor(s / 60) % 60);
  const ss = String(s % 60).padStart(2, "0");
  return s >= 3600 ? `${Math.floor(s / 3600)}:${mm.padStart(2, "0")}:${ss}` : `${mm}:${ss}`;
};

// Seconds for display and seeking: recordings without a length (WebM from
// MediaRecorder) report Infinity.
export const finite = (seconds: number) => (Number.isFinite(seconds) ? seconds : 0);

type Sized = { width?: number; height?: number };

// The box a picture fills before it loads (no jump when it does): its size
// in CSS pixels, scaled down into max as the chat's CSS does. null: unknown.
export const imageBox = ({ width, height }: Sized, max = { width: 400, height: 300 }) => {
  if (!width || !height) return null;
  const scale = Math.min(1, max.width / width, max.height / height);
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) };
};

// The server's smaller picture (made with ffmpeg, if it has it): a large
// picture's in the chat (the original opens in the lightbox), a video's
// poster. A GIF's video is not a picture (see chatVideo).
export const chatSrc = (u: { url: string; preview?: string; previewType?: string }) =>
  u.preview !== undefined && !u.previewType?.startsWith("video/") ? u.preview : u.url;

// A large GIF's video (about a tenth of the GIF), shown like the picture:
// playing, looping, muted, without controls; null: none.
export const chatVideo = (u: { type?: string; preview?: string; previewType?: string }) =>
  u.preview !== undefined && u.type?.startsWith("image/") && u.previewType?.startsWith("video/") ? u.preview : null;

// A video with a poster loads nothing until played; without one its
// metadata and first frame.
export const videoPreload = (poster: string | undefined) => (poster ? "none" : "metadata");

// CSS aspect-ratio of a video (its box width is fixed).
export const mediaRatio = ({ width, height }: Sized) => (width && height ? `${width} / ${height}` : undefined);

// Seconds to show: the player's, else the server's (milliseconds, known
// before the metadata loads).
export const shownLength = (seconds: number, knownMs = 0) => finite(seconds) || knownMs / 1000;

// Full screen for a video, for our own button: a narrow (portrait) player
// leaves no room for the browser's (Firefox drops it, Chromium hides it in
// a menu). iPhones have only the video's own full screen. null: neither.
export const videoFullScreen = (video: HTMLVideoElement): (() => void) | null => {
  const v = video as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
  if (typeof v.requestFullscreen === "function") return () => void v.requestFullscreen().catch(() => {});
  if (typeof v.webkitEnterFullscreen === "function") return () => v.webkitEnterFullscreen?.();
  return null;
};

// One plays at a time: starting one pauses the one before.
let playing: HTMLMediaElement | null = null;
export const onlyOne = (el: HTMLMediaElement) => {
  if (playing && playing !== el) playing.pause();
  playing = el;
};

// A player that leaves the screen (closed, or the client re-mounted on a
// reconnect) stops and drops its source: else the browser keeps it as the
// media-session target, and a media key or a headset resumes it hours later.
export const releaseMedia = (el: HTMLMediaElement) => {
  el.pause();
  el.removeAttribute("src");
  el.load();
  if (playing === el) playing = null;
};

// As a Svelte attachment: {@attach releaseOnDestroy}.
export const releaseOnDestroy = (el: HTMLMediaElement) => () => releaseMedia(el);

// A GIF's video (see chatVideo), as a Svelte attachment: loaded and played
// only near the screen (as lazy pictures; a long chat has many), paused
// out of view. A refused play (autoplay not allowed: iOS Low Power Mode):
// onRefused (the GIF instead); an interrupted one (paused meanwhile) is
// nothing.
export const lazyVideo = (src: string, onRefused: () => void) => (el: HTMLVideoElement) => {
  const io = new IntersectionObserver(
    ([entry]) => {
      if (!entry?.isIntersecting) return el.pause();
      if (!el.src) el.src = src;
      el.muted = true; // before play, for iOS
      el.play().catch((err: unknown) => (err as { name?: string } | null)?.name === "NotAllowedError" && onRefused());
    },
    { rootMargin: "300px" },
  );
  io.observe(el);
  return () => {
    io.disconnect();
    releaseMedia(el);
  };
};

// Spoiler attachments are marked by name, as in Discord (no extra field on
// the server); they show without the prefix, hidden until clicked.
const SPOILER_PREFIX = "SPOILER_";

export const asSpoiler = (file: File, on: boolean): File =>
  on && !file.name.startsWith(SPOILER_PREFIX) ? new File([file], SPOILER_PREFIX + file.name, { type: file.type }) : file;

export const spoilerUpload = <T extends { name: string }>(u: T): T & { spoiler?: true } =>
  u.name.startsWith(SPOILER_PREFIX) ? { ...u, name: u.name.slice(SPOILER_PREFIX.length), spoiler: true } : u;

// A download button's link: the server sends the file as an attachment
// (media are served inline, so a plain link would play it).
export const downloadUrl = (url: string) => `${url}${url.includes("?") ? "&" : "?"}download`;
