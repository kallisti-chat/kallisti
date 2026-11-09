// Plays an Ogg Opus file into the root channel through a running server.
// Usage: pnpm --filter @kallisti/devtools voicebot file.opus [ws://127.0.0.1:8080] [name]
// Make a test file: ffmpeg -f lavfi -i "sine=f=440:d=5" -ac 1 -ar 48000 \
//   -c:a libopus -frame_duration 20 tone.opus
import { readFile } from "node:fs/promises";
import WebSocket from "ws";
import {
  audioFormat,
  createMessageStream,
  encodeAudio,
  encodeVoicePacket,
  handshake,
  initialState,
  reduce,
  type ServerState,
} from "../../protocol/src/index.ts";

// Ogg pages hold packets as lacing segments. A segment of 255 continues.
const oggPackets = (buf: Uint8Array): Uint8Array[] => {
  const packets: Uint8Array[] = [];
  let current: number[] = [];
  for (let pos = 0; pos + 27 <= buf.length; ) {
    const segments = buf[pos + 26]!;
    const table = buf.subarray(pos + 27, pos + 27 + segments);
    let data = pos + 27 + segments;
    for (const len of table) {
      current.push(...buf.subarray(data, data + len));
      data += len;
      if (len < 255) packets.push(Uint8Array.from(current)), (current = []);
    }
    pos = data;
  }
  return packets.slice(2); // skip OpusHead and OpusTags
};

const [file, url = "ws://127.0.0.1:8080", name = "voicebot"] = process.argv.slice(2);
if (!file) throw new Error("usage: voicebot.ts file.opus [url] [name]");
const packets = oggPackets(new Uint8Array(await readFile(file)));
const FRAME_MS = 20;

let state: ServerState = initialState;
const push = createMessageStream();
const ws = new WebSocket(url);
ws.on("open", () => ws.send(handshake({ username: name })));
ws.on("message", (data, isBinary) => {
  // Text frames are the Kallisti server's JSON API, not Mumble.
  if (!isBinary) return;
  const wasSynced = state.status === "synced";
  for (const msg of push(new Uint8Array(data as Buffer))) state = reduce(state, msg, Date.now());
  if (wasSynced || state.status !== "synced") return;
  const format = audioFormat(state);
  console.log(`synced, sending ${packets.length} packets (${format})`);
  packets.forEach((opus, i) =>
    setTimeout(() => {
      const last = i === packets.length - 1;
      const packet = { target: 0, frameNumber: i * (FRAME_MS / 10), opus, terminator: last };
      ws.send(encodeVoicePacket(encodeAudio(format, packet)));
      if (last) setTimeout(() => ws.close(), 200);
    }, i * FRAME_MS),
  );
});
ws.on("close", () => process.exit(0));
