import { createStreamStats, type StreamStats } from "./stats";
import { voiceDecoder, type VoiceDecoder } from "../voice/codec";
import { shouldFallBack } from "../voice/fallback";
import { createPlayout, lastDue, type Playout } from "./playout";
import { t } from "../i18n.svelte";
import { cannotPlay, decodeFrame, decodeWatch, encodeFrame, pickCodec, previewSize } from "./protocol";
import { DEFAULT_LIMITS, DEFAULT_QUALITY, KINDS, cameraConstraints, encoding, nextCamera, parseLimits, sources, type Kind, type Quality } from "./quality";
import Ticker from "./ticker.worker?worker";

// Screen sharing and cameras over the stream connection (/api/stream, see
// the server's stream package): each is encoded with WebCodecs and sent as
// frames; watched streams are decoded into canvases. One per voice
// connection.

const KEYFRAME_EVERY_MS = 8_000; // viewers also ask for keyframes
const MAX_BUFFERED = 1_500_000; // bytes waiting in the socket: skip frames
const MAX_DECODE_QUEUE = 30; // frames waiting in a decoder: skip to a keyframe
const MAX_DECODE_ERRORS = 5; // decoder errors in a row without a picture: give up
// Thumbnails for people not watching: small JPEGs, now and then.
const PREVIEW_EVERY_MS = 15_000;
const PREVIEW_FIRST_MS = 2_000;
const MAX_PREVIEW = 60_000; // characters of the data URL (the server takes 64 KiB)

// Stream sound: Opus, 48 kHz mono, 20 ms frames (what Mumble clients get
// too).
const SOUND: AudioEncoderConfig = { codec: "opus", sampleRate: 48_000, numberOfChannels: 1, bitrate: 64_000 };
// Watched pictures wait encoded for their time (see playout.ts) and go to
// the decoder this long before it.
const DECODE_AHEAD = 0.1;
const MAX_PENDING = 240; // encoded pictures waiting: start over at a keyframe
const MAX_QUEUED_PICTURES = 30; // decoded ones

type Viewer = {
  decoder: VideoDecoder | null;
  codec: string;
  needKey: boolean;
  canvas: HTMLCanvasElement | null;
  // With sound: its decoder and volume.
  sound: VoiceDecoder | null;
  gain: GainNode | null;
  // Picture and sound on one clock: the audio context's with sound, else
  // the page's.
  playout: Playout;
  // Encoded pictures and their play times (null: late, decoded for the
  // ones after it but not shown), the play times of those in the decoder,
  // and decoded ones waiting.
  pending: { chunk: EncodedVideoChunk; when: number | null }[];
  decoding: Map<number, number | null>;
  queue: { frame: VideoFrame; when: number }[];
  failures: ReturnType<typeof decodeWatch>;
  stats: StreamStats; // see lib/stream/stats
};
// One of our own streams (a screen share or the camera) while it runs.
type Outgoing = {
  media: MediaStream | null;
  encoder: VideoEncoder | null;
  soundEncoder: AudioEncoder | null;
  stopFrames: (() => void) | null;
  stopSound: (() => void) | null;
  forceKey: boolean;
  started: ((err: string | null) => void) | null;
  starting: boolean;
  // Counts stops: a start that sees it change while it waits (the picker
  // is open, the connection closed) gives up.
  stops: number;
  // Another capture of the same kind takes over (a camera switch).
  replace: ((media: MediaStream) => void) | null;
};
type Text = { type: string; session?: number; sessions?: number[]; codec?: string; audio?: string; message?: string; camera?: boolean; limits?: unknown };

const both = <T>(v: () => T): Record<Kind, T> => ({ screen: v(), camera: v() });
// The camera's commands and frames say so; the screen's are as before.
const own = (kind: Kind) => (kind === "camera" ? { camera: true } : {});

// Watching needs decoders; what a browser can share, see shareSources.
export const streamingSupported = () => typeof VideoDecoder !== "undefined" && typeof VideoFrame !== "undefined";

export const shareSources = (phone: boolean) =>
  sources({
    encode: typeof VideoEncoder !== "undefined",
    display: !!navigator.mediaDevices?.getDisplayMedia,
    camera: !!navigator.mediaDevices?.getUserMedia,
    phone,
  });

// The chosen camera, or the default one when it is gone (as microphones).
const openCamera = async (deviceId: string | null, q: Quality): Promise<MediaStream> => {
  try {
    return await navigator.mediaDevices.getUserMedia({ video: cameraConstraints(deviceId, q), audio: false });
  } catch (e) {
    if (!shouldFallBack(e, deviceId)) throw e;
    return navigator.mediaDevices.getUserMedia({ video: cameraConstraints(null, q), audio: false });
  }
};

// audio: the app's audio context (stream sound plays there; none: no sound).
export const createStreamClient = (url: string, token: () => Promise<string>, audio: AudioContext | null) => {
  let ws: WebSocket | null = null;
  let opening: Promise<WebSocket> | null = null;
  let closed = false;
  // Sharing, per kind.
  const out = both<Outgoing>(() => ({
    media: null,
    encoder: null,
    soundEncoder: null,
    stopFrames: null,
    stopSound: null,
    forceKey: true,
    started: null,
    starting: false,
    stops: 0,
    replace: null,
  }));
  let sharing = $state(both(() => false));
  let preview = $state(both<MediaStream | null>(() => null));
  let quality = $state<Record<Kind, Quality>>(DEFAULT_QUALITY);
  let limits = DEFAULT_LIMITS;
  let cameraDevice: string | null = null;
  let error = $state<string | null>(null);
  // Who watches our streams (sessions), and whether others see thumbnails.
  let viewers = $state(both<number[]>(() => []));
  let previews = true;
  // Deafened: stream sound is silent too.
  let deaf = false;
  // Watching: by stream id (see protocol.streamId).
  const watched = new Map<number, Viewer>();
  let watching = $state<number[]>([]);
  // The streaming connection closed (it drops what was watched): what was
  // watched before still counts (see lib/stream/rewatch). A new one opens
  // with the next watch.
  let gone = $state(false);
  // Watched streams with sound, and their volume (0 to 1).
  let sounds = $state<Record<number, number>>({});

  // The connection: open once the server took the token ("ready", with
  // its limits).
  const open = (): Promise<WebSocket> => {
    if (ws?.readyState === WebSocket.OPEN) return Promise.resolve(ws);
    opening ??= (async () => {
      const t = await token();
      const sock = new WebSocket(url);
      sock.binaryType = "arraybuffer";
      await new Promise<void>((resolve, reject) => {
        sock.onopen = () => sock.send(JSON.stringify({ type: "auth", token: t }));
        sock.onmessage = (e) => {
          const m = typeof e.data === "string" ? (JSON.parse(e.data) as Text) : null;
          if (m?.type !== "ready") return;
          limits = parseLimits(m.limits);
          resolve();
        };
        sock.onclose = () => reject(new Error("cannot connect for streaming"));
      });
      if (closed) {
        sock.close();
        throw new Error("closed");
      }
      sock.onmessage = (e) => (typeof e.data === "string" ? onText(JSON.parse(e.data)) : onFrame(e.data as ArrayBuffer));
      sock.onclose = () => {
        gone = true; // before the streams go
        if (ws === sock) ws = null;
        for (const kind of KINDS) {
          out[kind].started?.("the streaming connection closed");
          stopSharing(kind, false);
        }
        for (const s of [...watched.keys()]) unwatch(s, false);
      };
      ws = sock;
      gone = false;
      return sock;
    })().finally(() => (opening = null));
    return opening;
  };

  const send = (v: object) => ws?.readyState === WebSocket.OPEN && ws.send(JSON.stringify(v));

  const onText = (m: Text) => {
    const kind: Kind = m.camera ? "camera" : "screen"; // of our own stream
    const o = out[kind];
    switch (m.type) {
      case "started":
        o.started?.(null);
        break;
      case "keyframe":
        o.forceKey = true;
        break;
      case "stopped": // not a late answer to our stop while a new start waits
        if (sharing[kind]) stopSharing(kind, false);
        break;
      case "viewers":
        viewers = { ...viewers, [kind]: m.sessions ?? [] };
        break;
      case "error":
        // About a stream we wanted to watch, or about our own.
        if (m.session !== undefined) unwatch(m.session, false);
        else if (o.started) o.started(m.message ?? "error");
        if (!o.started || m.session !== undefined) error = m.message ?? "error";
        break;
      case "stream":
        if (m.session !== undefined && m.codec) void configureIfSupported(m.session, m.codec, m.audio === "opus");
        break;
      case "ended":
        if (m.session !== undefined) unwatch(m.session, false);
        break;
    }
  };

  // Sharing ------------------------------------------------------------

  const startSharing = async (kind: Kind = "screen") => {
    const o = out[kind];
    if (sharing[kind] || o.starting || closed) return; // a second click while the picker is open
    o.starting = true;
    error = null;
    const stopsAtStart = o.stops;
    const stale = new Error("stopped");
    const live = <T>(v: T): T => {
      if (o.stops !== stopsAtStart) throw stale;
      return v;
    };
    let asked = false; // the server was asked to start
    try {
      const sock = live(await open());
      // Screens: sound where the browser offers it (Chromium: a tab, or the
      // system on Windows). Cameras: none (the microphone is voice). A
      // stale capture is stopped in the catch.
      o.media =
        kind === "camera"
          ? await openCamera(cameraDevice, quality.camera)
          : await navigator.mediaDevices.getDisplayMedia({ video: { frameRate: quality.screen.fps }, audio: true });
      live(null);
      const track = o.media.getVideoTracks()[0]!;
      track.onended = () => stopSharing(kind, true); // the browser's own "Stop sharing", an unplugged camera
      const soundTrack = o.media.getAudioTracks()[0];
      const withSound = !!soundTrack && live(await soundSupported());
      if (soundTrack && !withSound) soundTrack.stop();
      const settings = track.getSettings();
      const together = () => sharing[kind === "screen" ? "camera" : "screen"];
      let enc = encoding({ width: settings.width || 1280, height: settings.height || 720 }, quality[kind], kind, limits, together());
      const codec = live(await pickCodec(async (c) => (await VideoEncoder.isConfigSupported(encoderConfig(c, enc))).supported === true));
      if (!codec) throw new Error("this browser cannot encode video for streaming");
      const configure = () => {
        if (o.encoder?.state !== "closed") o.encoder?.close();
        o.encoder = new VideoEncoder({
          output: (chunk) => {
            const data = new Uint8Array(chunk.byteLength);
            chunk.copyTo(data);
            if (sock.readyState === WebSocket.OPEN) sock.send(encodeFrame(chunk.type === "key", chunk.timestamp, data, false, kind === "camera"));
          },
          error: (e) => (error = t("media.encoderFailed", { message: e.message })),
        });
        o.encoder.configure(encoderConfig(codec, enc));
        o.forceKey = true;
      };
      configure();
      const startText = () => ({ type: "start", codec, width: enc.width, height: enc.height, ...(withSound ? { audio: "opus" } : {}), ...own(kind) });
      asked = true;
      const err = await new Promise<string | null>((resolve) => {
        o.started = resolve;
        send(startText());
      });
      o.started = null;
      if (err) throw new Error(err);
      live(null);
      sharing = { ...sharing, [kind]: true };
      if (withSound) {
        o.soundEncoder = new AudioEncoder({
          output: (chunk) => {
            const data = new Uint8Array(chunk.byteLength);
            chunk.copyTo(data);
            if (sock.readyState === WebSocket.OPEN) sock.send(encodeFrame(false, chunk.timestamp, data, true));
          },
          error: () => o.stopSound?.(), // the picture goes on
        });
        o.soundEncoder.configure(SOUND);
        o.stopSound = soundFrames(soundTrack!, (data) => o.soundEncoder?.state === "configured" && o.soundEncoder.encode(data));
      }
      let keyAt = 0;
      let previewAt = performance.now() + PREVIEW_FIRST_MS;
      const onFrame = (frame: VideoFrame) => {
        const now = performance.now();
        if (previews && now >= previewAt) {
          previewAt = now + PREVIEW_EVERY_MS;
          const image = thumbnail(frame);
          if (image.length <= MAX_PREVIEW) send({ type: "preview", image, ...own(kind) });
        }
        const encoder = o.encoder;
        if (!encoder || encoder.state !== "configured" || encoder.encodeQueueSize > 2 || sock.bufferedAmount > MAX_BUFFERED) {
          if (sock.bufferedAmount > MAX_BUFFERED) o.forceKey = true; // viewers miss frames: start whole again
          frame.close();
          return;
        }
        // A resized window, another quality or camera: a new encoder (it
        // starts with a keyframe), and viewers get the new size. Another
        // bitrate or frame rate (also when the other kind of stream starts
        // or stops: they share the upload): the encoder is set again.
        const next = encoding({ width: frame.displayWidth, height: frame.displayHeight }, quality[kind], kind, limits, together());
        if (next.width !== enc.width || next.height !== enc.height) {
          enc = next;
          configure();
          send(startText());
        } else if (next.bitrate !== enc.bitrate || next.framerate !== enc.framerate) {
          enc = next;
          encoder.configure(encoderConfig(codec, enc));
        }
        const key = o.forceKey || now >= keyAt;
        if (key) keyAt = now + KEYFRAME_EVERY_MS;
        o.encoder!.encode(frame, { keyFrame: key });
        frame.close();
        o.forceKey = false;
      };
      o.replace = (media) => {
        o.stopFrames?.();
        if (o.media !== media) o.media?.getTracks().forEach((t) => t.stop());
        o.media = media;
        preview = { ...preview, [kind]: media };
        const t = media.getVideoTracks()[0]!;
        t.onended = () => stopSharing(kind, true);
        o.stopFrames = frames(t, () => quality[kind].fps, onFrame);
      };
      o.replace(o.media);
    } catch (e) {
      o.started = null;
      if (asked && !sharing[kind]) send({ type: "stop", ...own(kind) }); // it may have started meanwhile
      stopSharing(kind, true);
      // Closing the browser's picker is not an error, nor a stop or close meanwhile.
      if (e !== stale && !closed && !(e instanceof DOMException && e.name === "NotAllowedError")) error = e instanceof Error ? e.message : String(e);
    } finally {
      o.starting = false;
    }
  };

  const stopSharing = (kind: Kind, tell: boolean) => {
    const o = out[kind];
    if (tell && sharing[kind]) send({ type: "stop", ...own(kind) });
    o.stops++;
    o.started?.(null); // a start waiting for the server goes on, sees the stop and gives up
    o.stopFrames?.();
    o.stopFrames = null;
    o.replace = null;
    o.stopSound?.();
    o.stopSound = null;
    if (o.soundEncoder && o.soundEncoder.state !== "closed") o.soundEncoder.close();
    o.soundEncoder = null;
    if (o.encoder && o.encoder.state !== "closed") o.encoder.close();
    o.encoder = null;
    o.media?.getTracks().forEach((t) => t.stop());
    o.media = null;
    sharing = { ...sharing, [kind]: false };
    preview = { ...preview, [kind]: null };
    viewers = { ...viewers, [kind]: [] };
  };

  // A new quality, also while live: the capture is asked for it, and the
  // encoder follows with the next frame (see onFrame).
  const setQuality = (kind: Kind, q: Quality) => {
    if (quality[kind].height === q.height && quality[kind].fps === q.fps) return;
    quality = { ...quality, [kind]: q };
    const track = out[kind].media?.getVideoTracks()[0];
    if (!track) return;
    const { width, height, frameRate } = cameraConstraints(null, q);
    void track.applyConstraints(kind === "camera" ? { width, height, frameRate } : { frameRate: q.fps }).catch(() => {}); // the encoder still scales down
  };

  // The next camera (a phone's front and back) while the camera is live.
  // The current one closes first (phones open one camera at a time); if
  // the next does not open, the current one again.
  let switching = false; // a second click meanwhile does nothing
  const switchCamera = async () => {
    const o = out.camera;
    if (!sharing.camera || !o.replace || switching) return;
    switching = true;
    const stops = o.stops;
    const reopen = (id: string | null) => navigator.mediaDevices.getUserMedia({ video: cameraConstraints(id, quality.camera), audio: false });
    try {
      const cameras = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === "videoinput");
      const current = o.media?.getVideoTracks()[0]?.getSettings().deviceId ?? null;
      const next = nextCamera(cameras, current);
      if (!next || o.stops !== stops) return;
      o.stopFrames?.();
      o.stopFrames = null;
      o.media?.getTracks().forEach((t) => t.stop());
      const media = await reopen(next).catch(() => reopen(current));
      if (o.stops !== stops || !o.replace) media.getTracks().forEach((t) => t.stop());
      else o.replace(media);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      if (o.stops === stops) stopSharing("camera", true);
    } finally {
      switching = false;
    }
  };

  // Watching -----------------------------------------------------------

  const watch = async (session: number) => {
    error = null;
    if (watched.has(session)) return;
    const v: Viewer = {
      decoder: null,
      codec: "",
      needKey: true,
      canvas: null,
      sound: null,
      gain: null,
      playout: createPlayout(),
      pending: [],
      decoding: new Map(),
      queue: [],
      failures: decodeWatch(MAX_DECODE_ERRORS),
      stats: createStreamStats(),
    };
    watched.set(session, v);
    startLoop();
    watching = [...watched.keys()];
    try {
      await open();
      // Stopped while the connection opened (perhaps watched again: that
      // asks itself).
      if (watched.get(session) === v) send({ type: "watch", session });
    } catch (e) {
      if (watched.get(session) !== v) return;
      unwatch(session, false);
      error = e instanceof Error ? e.message : String(e);
    }
  };

  // A new size of the same stream (another quality, a resized window): the
  // decoder takes it at the next keyframe, which the server waits for. A
  // new decoder would miss that keyframe while it is set up.
  const configureIfSupported = async (session: number, codec: string, withSound: boolean) => {
    const known = watched.get(session);
    if (known?.decoder?.state === "configured" && known.codec === codec) known.needKey = true;
    else if (!(await VideoDecoder.isConfigSupported({ codec })).supported) {
      unwatch(session);
      error = cannotPlay(codec);
      return;
    } else configure(session, codec);
    const v = watched.get(session);
    if (v && withSound && !v.sound) listen(session, v);
  };

  // Sound plays only while the audio context runs: suspended (or WebKit's
  // "interrupted") its clock stands still, so sound would pile up without
  // a limit and play as one burst when it runs again.
  const soundRuns = () => audio?.state === "running";

  // The stream's sound, into the app's audio context with its own volume.
  // The playout's clock is the audio context's from now on.
  const listen = (session: number, v: Viewer) => {
    const ctx = audio;
    if (!ctx) return;
    v.gain = new GainNode(ctx, { gain: deaf ? 0 : (sounds[session] ?? 1) });
    v.gain.connect(ctx.destination);
    // WebCodecs where it has Opus, else libopus (the iOS app has no
    // AudioDecoder; the Linux app's leaks). Mono: one channel of two.
    v.sound = voiceDecoder({
      output: ([plane], timestamp) => {
        const runs = soundRuns();
        const at = runs ? v.playout.at(timestamp, ctx.currentTime) : null;
        if (runs && at === null) v.stats.late("sound");
        if (at === null || !v.gain || !plane) return;
        const buffer = ctx.createBuffer(1, plane.length, SOUND.sampleRate);
        buffer.copyToChannel(plane, 0);
        const src = new AudioBufferSourceNode(ctx, { buffer, playbackRate: v.playout.rate() });
        src.connect(v.gain);
        src.start(at);
      },
      error: () => {}, // the picture goes on
    });
    // Pictures timed on the page's clock are not shown.
    v.playout.reset();
    for (const p of v.pending) p.when = null;
    for (const t of v.decoding.keys()) v.decoding.set(t, null);
    v.queue.splice(0).forEach((q) => q.frame.close());
    sounds = { ...sounds, [session]: sounds[session] ?? 1 };
  };
  const dropSound = (v: Viewer) => {
    v.sound?.close();
    v.gain?.disconnect();
    v.sound = v.gain = null;
  };

  // Watched pictures are drawn on animation frames; in a hidden tab (none
  // there) on a worker's timer, so picture-in-picture goes on, in sync.
  let raf = 0;
  let ticker: Worker | null = null;
  const pumpAll = () => watched.forEach((v, session) => pump(session, v));
  const tickRate = () => ticker?.postMessage(document.hidden ? 1000 / 60 : 1000);
  const startLoop = () => {
    if (ticker) return;
    ticker = new Ticker();
    ticker.onmessage = () => document.hidden && pumpAll();
    tickRate();
    document.addEventListener("visibilitychange", tickRate);
    const frame = () => {
      raf = requestAnimationFrame(frame);
      pumpAll();
    };
    raf = requestAnimationFrame(frame);
  };
  const stopLoop = () => {
    cancelAnimationFrame(raf);
    document.removeEventListener("visibilitychange", tickRate);
    ticker?.terminate();
    ticker = null;
  };

  const clockOf = (v: Viewer) => (v.sound && audio ? audio.currentTime : performance.now() / 1000);

  // Each tick: pictures whose time nears go to the decoder; the newest
  // decoded one whose time came is drawn.
  const pump = (session: number, v: Viewer) => {
    const now = clockOf(v);
    while (v.pending.length && (v.pending[0]!.when ?? 0) <= now + DECODE_AHEAD) decodeNext(session, v);
    const i = lastDue(
      v.queue.map((q) => q.when),
      now,
    );
    if (i < 0) return;
    const shown = v.queue.splice(0, i + 1);
    draw(v, shown[i]!.frame);
    v.stats.drawn();
    shown.forEach((q) => q.frame.close());
  };

  const decodeNext = (session: number, v: Viewer) => {
    const { chunk, when } = v.pending.shift()!;
    const decoder = v.decoder;
    if (!decoder || decoder.state !== "configured") return;
    if (chunk.type !== "key" && decoder.decodeQueueSize > MAX_DECODE_QUEUE) return skipToKey(session, v); // too slow: skip ahead
    v.decoding.set(chunk.timestamp, when);
    decoder.decode(chunk);
  };

  const skipToKey = (session: number, v: Viewer) => {
    if (!v.needKey) v.stats.skipped();
    if (!v.needKey) send({ type: "keyframe", session });
    v.needKey = true;
    v.pending = [];
  };

  // A decoder for a stream, then a keyframe to start from.
  const configure = (session: number, codec: string) => {
    const v = watched.get(session);
    if (!v) return;
    if (v.decoder && v.decoder.state !== "closed") v.decoder.close();
    v.needKey = true;
    v.pending = [];
    v.decoding.clear();
    v.codec = codec;
    v.decoder = new VideoDecoder({
      output: (frame) => {
        v.failures.decoded();
        const when = v.decoding.get(frame.timestamp);
        for (const t of v.decoding.keys()) {
          v.decoding.delete(t); // and older ones without output
          if (t === frame.timestamp) break;
        }
        if (when == null) return frame.close(); // late
        v.queue.push({ frame, when });
        while (v.queue.length > MAX_QUEUED_PICTURES) v.queue.shift()!.frame.close();
      },
      error: () => {
        if (watched.get(session) !== v) return;
        if (!v.failures.failed()) return configure(session, codec); // start over
        unwatch(session);
        error = cannotPlay(codec);
      },
    });
    v.decoder.configure({ codec, optimizeForLatency: true });
    send({ type: "keyframe", session });
  };

  const onFrame = (buf: ArrayBuffer) => {
    const f = decodeFrame(buf);
    const v = f && watched.get(f.session);
    if (!f || !v?.decoder || v.decoder.state !== "configured") return;
    if (f.sound) {
      v.stats.sound(f.data.byteLength, f.timestamp);
      if (v.sound && soundRuns()) v.sound.decode(f.data, f.timestamp);
      return;
    }
    v.stats.picture(f.data.byteLength);
    if (!f.key && v.needKey) return;
    if (v.pending.length >= MAX_PENDING) {
      skipToKey(f.session, v);
      if (!f.key) return;
    }
    v.needKey = false;
    const chunk = new EncodedVideoChunk({ type: f.key ? "key" : "delta", timestamp: f.timestamp, data: f.data });
    const when = v.playout.at(f.timestamp, clockOf(v));
    if (when === null) v.stats.late("picture");
    v.pending.push({ chunk, when });
  };

  const unwatch = (session: number, tell = true) => {
    const v = watched.get(session);
    if (!v) return;
    if (tell) send({ type: "unwatch", session });
    if (v.decoder && v.decoder.state !== "closed") v.decoder.close();
    dropSound(v);
    v.queue.forEach((q) => q.frame.close());
    watched.delete(session);
    if (!watched.size) stopLoop();
    watching = [...watched.keys()];
    const { [session]: _, ...rest } = sounds;
    sounds = rest;
  };

  return {
    // Our own streams, by kind: live, the capture (a self-preview), who
    // watches, the quality.
    get sharing() {
      return sharing;
    },
    get preview() {
      return preview;
    },
    get viewers() {
      return viewers;
    },
    get quality() {
      return quality;
    },
    get limits() {
      return limits;
    },
    get error() {
      return error;
    },
    get watching() {
      return watching;
    },
    // A watched stream's counts and its delay behind the stream in seconds
    // (see lib/stream/stats), for the tile's "Stats"; null: not watched.
    statsOf: (session: number) => {
      const v = watched.get(session);
      return v ? { totals: v.stats.totals(), delay: v.playout.lead() } : null;
    },
    get gone() {
      return gone;
    },
    // A watched stream's volume (undefined: it has no sound).
    volumeOf: (session: number): number | undefined => sounds[session],
    setVolume: (session: number, volume: number) => {
      const v = watched.get(session);
      if (!v?.gain || !audio) return;
      if (!deaf) v.gain.gain.setTargetAtTime(volume, audio.currentTime, 0.02);
      sounds = { ...sounds, [session]: volume };
    },
    // Whether others see thumbnails of our streams (a setting); turned
    // off, the last ones go too.
    setPreviews: (on: boolean) => {
      if (previews && !on) for (const kind of KINDS) if (sharing[kind]) send({ type: "preview", image: "", ...own(kind) });
      previews = on;
    },
    setDeaf: (on: boolean) => {
      deaf = on;
      if (!audio) return;
      for (const [session, v] of watched) v.gain?.gain.setTargetAtTime(on ? 0 : (sounds[session] ?? 1), audio.currentTime, 0.02);
    },
    // An audio restart (see voice/restart.ts): watched sound goes on in
    // the new context, at its volume.
    setAudio: (ctx: AudioContext) => {
      const withSound = [...watched].filter(([, v]) => v.sound);
      withSound.forEach(([, v]) => dropSound(v));
      audio = ctx;
      withSound.forEach(([session, v]) => listen(session, v));
    },
    // The camera to open next (null: the default).
    setCameraDevice: (deviceId: string | null) => (cameraDevice = deviceId),
    setQuality,
    switchCamera,
    startSharing,
    stopSharing: (kind: Kind = "screen") => stopSharing(kind, true),
    clearError: () => (error = null),
    watch,
    unwatch: (session: number) => unwatch(session),
    // Where a watched stream is drawn (null: nowhere for now).
    attach: (session: number, canvas: HTMLCanvasElement | null) => {
      const v = watched.get(session);
      if (v) v.canvas = canvas;
    },
    close: () => {
      closed = gone = true;
      for (const kind of KINDS) stopSharing(kind, true);
      ws?.close();
    },
  };
};

export type StreamClient = ReturnType<typeof createStreamClient>;

const encoderConfig = (codec: string, e: ReturnType<typeof encoding>): VideoEncoderConfig => ({
  codec,
  ...e,
  latencyMode: "realtime",
  ...(codec.startsWith("avc1") ? { avc: { format: "annexb" as const } } : {}),
});

// The track's frames, at most fps() a second, until the returned stop. Where
// the browser has MediaStreamTrackProcessor (Chrome, Edge) frames come
// straight from the track, also in a hidden tab (the usual case: sharing
// another window). Elsewhere a hidden video element is drawn on a worker's
// timer (page timers are slowed in hidden tabs; worker timers less so).
type ProcessorCtor = new (init: { track: MediaStreamTrack }) => { readable: ReadableStream<VideoFrame> };

const draw = (v: Viewer, frame: VideoFrame) => {
  const c = v.canvas;
  if (!c) return;
  if (c.width !== frame.displayWidth || c.height !== frame.displayHeight) {
    c.width = frame.displayWidth;
    c.height = frame.displayHeight;
  }
  c.getContext("2d")?.drawImage(frame, 0, 0);
};

const soundSupported = async () =>
  typeof AudioEncoder !== "undefined" &&
  !!(globalThis as { MediaStreamTrackProcessor?: unknown }).MediaStreamTrackProcessor &&
  (await AudioEncoder.isConfigSupported(SOUND)).supported === true;

// Picture and sound tracks may stamp their frames on different clocks
// (Chromium's tab capture does): both are put on the page's clock, from
// their first frame on (the spacing of their own timestamps stays).
const onPageClock = () => {
  let base: number | null = null;
  return (timestamp: number) => {
    base ??= performance.now() * 1000 - timestamp;
    return timestamp + base;
  };
};

// A sound track's data, mixed down to mono and at 48 kHz (the encoder's;
// Windows devices may run at 44.1 kHz), until the returned stop. Needs
// MediaStreamTrackProcessor: Chromium, the only one that shares sound.
const soundFrames = (track: MediaStreamTrack, onData: (data: AudioData) => void): (() => void) => {
  const Processor = (globalThis as { MediaStreamTrackProcessor?: new (init: { track: MediaStreamTrack }) => { readable: ReadableStream<AudioData> } })
    .MediaStreamTrackProcessor!;
  const reader = new Processor({ track }).readable.getReader();
  const clock = onPageClock();
  let stopped = false;
  void (async () => {
    while (!stopped) {
      const { value: data, done } = await reader.read().catch(() => ({ value: undefined, done: true }));
      if (done || !data) return;
      if (!stopped) {
        let mono = new Float32Array(data.numberOfFrames);
        const plane = new Float32Array(data.numberOfFrames);
        for (let ch = 0; ch < data.numberOfChannels; ch++) {
          data.copyTo(plane, { planeIndex: ch, format: "f32-planar" });
          for (let i = 0; i < plane.length; i++) mono[i]! += plane[i]! / data.numberOfChannels;
        }
        if (data.sampleRate !== SOUND.sampleRate) mono = resample(mono, data.sampleRate, SOUND.sampleRate);
        const out = new AudioData({ format: "f32-planar", sampleRate: SOUND.sampleRate, numberOfFrames: mono.length, numberOfChannels: 1, timestamp: clock(data.timestamp), data: mono });
        onData(out);
        out.close();
      }
      data.close();
    }
  })();
  return () => {
    stopped = true;
    void reader.cancel().catch(() => {});
    track.stop();
  };
};

// Linear resampling of one chunk (good enough for a stream's sound).
export const resample = (input: Float32Array, from: number, to: number): Float32Array<ArrayBuffer> => {
  const out = new Float32Array(Math.round((input.length * to) / from));
  for (let i = 0; i < out.length; i++) {
    const x = (i * from) / to;
    const j = Math.floor(x);
    const a = input[j] ?? 0;
    const b = input[j + 1] ?? a;
    out[i] = a + (b - a) * (x - j);
  }
  return out;
};

// A frame as a small JPEG data URL.
const thumbnail = (frame: VideoFrame): string => {
  const canvas = document.createElement("canvas");
  ({ width: canvas.width, height: canvas.height } = previewSize(frame.displayWidth, frame.displayHeight));
  canvas.getContext("2d")?.drawImage(frame, 0, 0, canvas.width, canvas.height);
  return canvas.toDataURL("image/jpeg", 0.6);
};

const frames = (track: MediaStreamTrack, fps: () => number, onFrame: (frame: VideoFrame) => void): (() => void) => {
  const Processor = (globalThis as { MediaStreamTrackProcessor?: ProcessorCtor }).MediaStreamTrackProcessor;
  if (Processor) {
    const reader = new Processor({ track }).readable.getReader();
    const clock = onPageClock();
    let stopped = false;
    let last = -Infinity;
    void (async () => {
      while (!stopped) {
        const { value: frame, done } = await reader.read().catch(() => ({ value: undefined, done: true }));
        if (done || !frame) return;
        if (stopped || frame.timestamp - last < 1e6 / fps() - 2000) frame.close();
        else {
          last = frame.timestamp;
          const stamped = new VideoFrame(frame, { timestamp: clock(frame.timestamp) });
          frame.close();
          onFrame(stamped);
        }
      }
    })();
    return () => {
      stopped = true;
      void reader.cancel().catch(() => {});
    };
  }
  const video = document.createElement("video");
  video.muted = true;
  video.srcObject = new MediaStream([track]);
  void video.play().catch(() => {});
  const worker = new Ticker();
  let rate = fps();
  worker.postMessage(1000 / rate);
  worker.onmessage = () => {
    if (fps() !== rate) worker.postMessage(1000 / (rate = fps())); // another quality
    if (video.readyState >= 2) onFrame(new VideoFrame(video, { timestamp: performance.now() * 1000 }));
  };
  return () => {
    worker.terminate();
    video.srcObject = null;
  };
};
