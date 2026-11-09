// Connects through a running server and prints the synced server state.
// Usage: pnpm --filter @kallisti/devtools smoke [ws://127.0.0.1:8080]
// ORIGIN=https://chat.example: send this Origin (servers that do not allow *).
import WebSocket from "ws";
import {
  channelTree,
  createMessageStream,
  handshake,
  initialState,
  reduce,
  usersIn,
  type ServerState,
} from "../../protocol/src/index.ts";

let state: ServerState = initialState;
const push = createMessageStream();
const ws = new WebSocket(process.argv[2] ?? "ws://127.0.0.1:8080", { origin: process.env.ORIGIN });

ws.on("open", () => ws.send(handshake({ username: `smoke-${process.pid}` })));
ws.on("message", (data, isBinary) => {
  // Text frames are the Kallisti server's JSON API, not Mumble.
  if (!isBinary) return;
  for (const msg of push(new Uint8Array(data as Buffer))) {
    state = reduce(state, msg, Date.now());
  }
  if (state.status === "connecting") return;
  console.log(state.status, { session: state.session, error: state.error });
  console.log("root:", channelTree(state)?.channel.name);
  console.log("users in root:", usersIn(state, 0).map((u) => u.name));
  ws.close();
});
ws.on("close", () => process.exit(state.status === "synced" ? 0 : 1));
setTimeout(() => (console.error("timeout"), process.exit(1)), 5000);
