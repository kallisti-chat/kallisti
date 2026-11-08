import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../src/lib/stream/ticker.worker?worker", () => ({ default: class { postMessage() {} terminate() {} } }));
// Stream sound's Opus decoder (WebCodecs or libopus, see voice/codec).
const sound = vi.hoisted(() => ({ out: null as ((planes: Float32Array[], timestamp: number) => void) | null, decoded: [] as number[], closes: 0 }));
vi.mock("../src/lib/voice/codec", () => ({
  voiceDecoder: (sink: { output: (planes: Float32Array[], timestamp: number) => void }) => {
    sound.out = sink.output;
    return { decode: (_: Uint8Array, timestamp: number) => void sound.decoded.push(timestamp), close: () => void sound.closes++ };
  },
}));
const { createStreamClient } = await import("../src/lib/stream/client.svelte");
const { encodeFrame } = await import("../src/lib/stream/protocol");

// Starting a screen share while the connection goes away: the browser parts
// are fakes (a socket that is ready at once, a picker the test answers).
class FakeSocket {
  static OPEN = 1;
  static all: FakeSocket[] = [];
  readyState = 0;
  binaryType = "";
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor() {
    FakeSocket.all.push(this);
    queueMicrotask(() => {
      this.readyState = 1;
      this.onopen?.();
      this.onmessage?.({ data: '{"type":"ready"}' });
    });
  }
  sent: string[] = [];
  send(data: string) {
    this.sent.push(data);
  }
  types() {
    return this.sent.map((d) => JSON.parse(d).type as string);
  }
  close() {
    if (this.readyState === 3) return;
    this.readyState = 3;
    queueMicrotask(() => this.onclose?.());
  }
}

class FakeEncoder {
  static made = 0;
  static configs: VideoEncoderConfig[] = [];
  static isConfigSupported = async () => ({ supported: true });
  state = "configured";
  encodeQueueSize = 0;
  constructor() {
    FakeEncoder.made++;
  }
  configure(c: VideoEncoderConfig) {
    FakeEncoder.configs.push(c);
  }
  encode() {}
  close() {
    this.state = "closed";
  }
}

const capture = (width = 1920, height = 1080) => {
  const track = { stop: vi.fn(), getSettings: () => ({ width, height }), applyConstraints: vi.fn(async () => {}), onended: null as (() => void) | null };
  return { track, stream: { getVideoTracks: () => [track], getAudioTracks: () => [], getTracks: () => [track] } };
};

let pickers: ((s: unknown) => void)[] = [];
let cameras: { constraints: MediaStreamConstraints; answer: (s: unknown) => void }[] = [];
// Frames of the shared screen or camera: those the test sends.
let frameReads: ((v: { value: unknown; done: boolean }) => void)[] = [];
let frameTime = 0;
const sendFrame = (width: number, height: number) =>
  frameReads.shift()!({ value: { displayWidth: width, displayHeight: height, timestamp: (frameTime += 100_000), close() {} }, done: false });
const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  FakeSocket.all = [];
  FakeEncoder.made = 0;
  FakeEncoder.configs = [];
  pickers = [];
  cameras = [];
  frameReads = [];
  vi.stubGlobal("WebSocket", FakeSocket);
  vi.stubGlobal("VideoEncoder", FakeEncoder);
  vi.stubGlobal(
    "VideoFrame",
    class {
      displayWidth: number;
      displayHeight: number;
      constructor(f: { displayWidth: number; displayHeight: number }) {
        [this.displayWidth, this.displayHeight] = [f.displayWidth, f.displayHeight];
      }
      close() {}
    },
  );
  const reader = { read: () => new Promise((r) => frameReads.push(r)), cancel: async () => {} };
  vi.stubGlobal("MediaStreamTrackProcessor", class { readable = { getReader: () => reader }; });
  vi.stubGlobal("navigator", {
    mediaDevices: {
      getDisplayMedia: () => new Promise((r) => pickers.push(r)),
      getUserMedia: (constraints: MediaStreamConstraints) => new Promise((answer) => cameras.push({ constraints, answer })),
    },
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("starting a screen share", () => {
  it("stops the capture at once when the client closed while the picker was open", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const start = client.startSharing();
    await flush();
    expect(pickers).toHaveLength(1);
    client.close();
    await flush();
    const { track, stream } = capture();
    pickers[0]!(stream);
    await start;
    expect(track.stop).toHaveBeenCalled();
    expect(FakeEncoder.made).toBe(0);
    expect(client.sharing.screen).toBe(false);
  });

  it("can start again after the connection dropped while the picker was open", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const start = client.startSharing();
    await flush();
    FakeSocket.all[0]!.close();
    await flush();
    const { track, stream } = capture();
    pickers[0]!(stream);
    await start;
    expect(track.stop).toHaveBeenCalled();
    void client.startSharing();
    await flush();
    expect(pickers).toHaveLength(2);
  });

  it("is not cancelled by a late reply to the share before", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const start = client.startSharing();
    await flush();
    FakeSocket.all[0]!.onmessage?.({ data: '{"type":"stopped"}' });
    pickers[0]!(capture().stream);
    await flush();
    expect(FakeSocket.all[0]!.types()).toContain("start");
    FakeSocket.all[0]!.onmessage?.({ data: '{"type":"started"}' });
    await start;
    expect(client.sharing.screen).toBe(true);
  });

  it("tells the server when stopped after asking to start", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const start = client.startSharing();
    await flush();
    const { track, stream } = capture();
    pickers[0]!(stream);
    await flush();
    track.onended?.(); // the browser's "Stop sharing"
    await start;
    expect(FakeSocket.all[0]!.types()).toEqual(["auth", "start", "stop"]);
    expect(client.error).toBeNull();
  });
});

describe("watching", () => {
  beforeEach(() => {
    vi.stubGlobal("document", { hidden: false, addEventListener() {}, removeEventListener() {} });
    vi.stubGlobal("requestAnimationFrame", () => 0);
    vi.stubGlobal("cancelAnimationFrame", () => {});
  });

  it("asks for nothing when stopped before the connection is open", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const watch = client.watch(7);
    client.unwatch(7);
    await watch;
    await flush();
    expect(FakeSocket.all[0]!.types()).toEqual(["auth"]);
    expect(client.watching).toEqual([]);
  });

  it("watches when started again while the first start waited", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const first = client.watch(7);
    client.unwatch(7);
    const again = client.watch(7);
    await Promise.all([first, again]);
    expect(FakeSocket.all[0]!.types()).toEqual(["auth", "watch"]);
    expect(client.watching).toEqual([7]);
  });
});

describe("watching with sound", () => {
  // The app's audio context: frozen (suspended, or WebKit's "interrupted")
  // until the test lets it run.
  const ctx = { state: "suspended", currentTime: 5, destination: {}, createBuffer: () => ({ copyToChannel() {} }) };
  let started: number[] = [];
  let gains: { ctx: unknown; disconnected: boolean }[] = [];

  beforeEach(() => {
    ctx.state = "suspended";
    sound.decoded = [];
    sound.closes = 0;
    sound.out = null;
    gains = [];
    started = [];
    vi.stubGlobal("document", { hidden: false, addEventListener() {}, removeEventListener() {} });
    vi.stubGlobal("requestAnimationFrame", () => 0);
    vi.stubGlobal("cancelAnimationFrame", () => {});
    vi.stubGlobal(
      "VideoDecoder",
      class {
        static isConfigSupported = async () => ({ supported: true });
        state = "configured";
        configure() {}
        close() {}
      },
    );
    vi.stubGlobal(
      "GainNode",
      class {
        gain = { setTargetAtTime() {} };
        disconnected = false;
        constructor(public ctx: unknown) {
          gains.push(this);
        }
        connect() {}
        disconnect() {
          this.disconnected = true;
        }
      },
    );
    vi.stubGlobal("AudioBufferSourceNode", class { connect() {} start(at: number) { started.push(at); } });
  });

  const watchWithSound = async () => {
    const client = createStreamClient("ws://x", async () => "t", ctx as unknown as AudioContext);
    await client.watch(7);
    const sock = FakeSocket.all[0]!;
    sock.onmessage?.({ data: JSON.stringify({ type: "stream", session: 7, codec: "vp8", audio: "opus" }) });
    await flush();
    return Object.assign((timestamp: number) => {
      const f = encodeFrame(true, timestamp, new Uint8Array(4), true);
      const buf = new Uint8Array(4 + f.length);
      new DataView(buf.buffer).setUint32(0, 7);
      buf.set(f, 4);
      (sock.onmessage as unknown as (e: { data: ArrayBuffer }) => void)({ data: buf.buffer });
    }, { client });
  };
  const frame = () => [new Float32Array(960)];

  it.each([1, 2])("plays all %i decoded audio planes, including right-only stereo", async (channels) => {
    ctx.state = "running";
    const { client } = await watchWithSound();
    const copyToChannel = vi.fn();
    const createBuffer = vi.spyOn(ctx, "createBuffer").mockReturnValue({ copyToChannel });
    const planes = channels === 1 ? [new Float32Array(960).fill(0.5)] : [new Float32Array(960), new Float32Array(960).fill(0.5)];
    try {
      sound.out!(planes, 1_000_000);
      expect(createBuffer).toHaveBeenCalledExactlyOnceWith(channels, 960, 48_000);
      expect(copyToChannel).toHaveBeenCalledTimes(channels);
      planes.forEach((plane, channel) => expect(copyToChannel).toHaveBeenNthCalledWith(channel + 1, plane, channel));
      expect(started).toHaveLength(1);
    } finally {
      client.close();
      createBuffer.mockRestore();
    }
  });

  it("drops stream sound while the audio context does not run (it would pile up and play as one burst)", async () => {
    const send = await watchWithSound();
    send(1_000_000);
    expect(sound.decoded).toHaveLength(0);
    ctx.state = "running";
    send(1_020_000);
    expect(sound.decoded).toHaveLength(1);
  });

  it("plays stream sound without WebCodecs' AudioDecoder (the iOS app has none)", async () => {
    expect(typeof AudioDecoder).toBe("undefined");
    ctx.state = "running";
    const send = await watchWithSound();
    send(1_000_000);
    expect(sound.decoded).toEqual([1_000_000]);
    sound.out!(frame(), 1_000_000);
    expect(started).toHaveLength(1);
  });

  it("does not schedule sound decoded after the audio context stopped running", async () => {
    await watchWithSound();
    ctx.state = "interrupted";
    sound.out!(frame(), 0);
    expect(started).toEqual([]);
    ctx.state = "running";
    sound.out!(frame(), 0);
    expect(started).toHaveLength(1);
  });

  it("moves the sound to a new audio context (an audio restart), at its volume", async () => {
    const send = await watchWithSound();
    send.client.setVolume(7, 0.5);
    const next = { ...ctx, state: "running", currentTime: 1 };
    send.client.setAudio(next as unknown as AudioContext);
    expect(sound.closes).toBe(1);
    expect(gains[0]!.disconnected).toBe(true);
    expect(gains.at(-1)!.ctx).toBe(next);
    expect(send.client.volumeOf(7)).toBe(0.5);
    send(1_000_000); // the new context runs
    expect(sound.decoded).toHaveLength(1);
  });
});

describe("cameras", () => {
  const sent = (sock: FakeSocket) => sock.sent.map((d) => JSON.parse(d) as Record<string, unknown>);
  const startCamera = async (client: ReturnType<typeof createStreamClient>) => {
    const start = client.startSharing("camera");
    await flush();
    const cam = capture(1280, 720);
    cameras.at(-1)!.answer(cam.stream);
    await flush();
    FakeSocket.all[0]!.onmessage?.({ data: '{"type":"started","camera":true}' });
    await start;
    return cam;
  };

  it("opens the chosen camera without sound and starts a camera stream", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    client.setCameraDevice("cam-2");
    const cam = await startCamera(client);
    expect(cam.track.stop).not.toHaveBeenCalled();
    expect(cameras[0]!.constraints).toMatchObject({ audio: false, video: { deviceId: { exact: "cam-2" }, height: { ideal: 720 } } });
    expect(sent(FakeSocket.all[0]!).find((m) => m.type === "start")).toMatchObject({ camera: true, width: 1280, height: 720 });
    expect(client.sharing).toEqual({ screen: false, camera: true });
    expect(client.preview.camera).not.toBeNull();
  });

  it("runs next to a screen share, and stops on its own", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const screen = client.startSharing();
    await flush();
    pickers[0]!(capture().stream);
    await flush();
    FakeSocket.all[0]!.onmessage?.({ data: '{"type":"started"}' });
    await screen;
    await startCamera(client);
    expect(client.sharing).toEqual({ screen: true, camera: true });
    client.stopSharing("camera");
    expect(sent(FakeSocket.all[0]!).at(-1)).toEqual({ type: "stop", camera: true });
    expect(client.sharing).toEqual({ screen: true, camera: false });
    FakeSocket.all[0]!.onmessage?.({ data: '{"type":"stopped","camera":true}' }); // the server's answer
    expect(client.sharing.screen).toBe(true);
  });

  it("changes the quality while live: a new encoder, and viewers get the new size", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    const cam = await startCamera(client);
    sendFrame(1280, 720);
    await flush();
    const before = FakeEncoder.made;
    client.setQuality("camera", { height: 480, fps: 15 });
    expect(cam.track.applyConstraints).toHaveBeenCalledWith({ width: { ideal: 854 }, height: { ideal: 480 }, frameRate: { ideal: 15 } });
    sendFrame(1280, 720);
    await flush();
    expect(FakeEncoder.made).toBe(before + 1);
    expect(FakeEncoder.configs.at(-1)).toMatchObject({ width: 852, height: 480, framerate: 15 });
    expect(sent(FakeSocket.all[0]!).at(-1)).toMatchObject({ type: "start", camera: true, width: 852, height: 480 });
  });

  it("sets the same encoder again for another frame rate (the size stays)", async () => {
    const client = createStreamClient("ws://x", async () => "t", null);
    await startCamera(client);
    sendFrame(1280, 720);
    await flush();
    const [made, starts] = [FakeEncoder.made, FakeSocket.all[0]!.types().filter((t) => t === "start").length];
    client.setQuality("camera", { height: 720, fps: 15 });
    sendFrame(1280, 720);
    await flush();
    expect(FakeEncoder.made).toBe(made);
    expect(FakeEncoder.configs.at(-1)).toMatchObject({ width: 1280, height: 720, framerate: 15 });
    expect(FakeSocket.all[0]!.types().filter((t) => t === "start")).toHaveLength(starts);
  });
});
