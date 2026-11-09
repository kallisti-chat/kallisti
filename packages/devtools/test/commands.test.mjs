import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { WebSocketServer } from "ws";

const cwd = fileURLToPath(new URL("../", import.meta.url));
const { scripts } = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
const execute = promisify(execFile);
const run = async (name, args, env = {}) => {
  const [runtime, ...command] = scripts[name].split(" ");
  assert.equal(runtime, "node", "devtools must run with Node, without Bun");
  const { stdout } = await execute(process.execPath, [...command, ...args], {
    cwd, env: { ...process.env, ...env }, timeout: 8000,
  });
  return stdout;
};

const frame = (type, payload) => {
  const header = Buffer.alloc(6);
  header.writeUInt16BE(type);
  header.writeUInt32BE(payload.length, 2);
  return Buffer.concat([header, Buffer.from(payload)]);
};
const fixture = async (t) => {
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  const messages = [];
  let origin;
  t.after(() => {
    for (const client of server.clients) client.terminate();
    return new Promise((resolve) => server.close(resolve));
  });
  server.on("connection", (client, request) => {
    origin = request.headers.origin;
    client.on("message", (data) => {
      for (let offset = 0; offset < data.length;) {
        const type = data.readUInt16BE(offset);
        const end = offset + 6 + data.readUInt32BE(offset + 2);
        messages.push({ type, payload: data.subarray(offset + 6, end) });
        offset = end;
        if (type === 2) client.send(Buffer.concat([
          frame(0, [8, 128, 138, 4]), // Version 1.5.0: protobuf voice packets.
          frame(7, [8, 0, 26, 7, ...Buffer.from("Fixture")]), // Root ChannelState.
          frame(5, [8, 7]), // ServerSync, session 7.
        ]));
      }
    });
  });
  await once(server, "listening");
  return { url: `ws://127.0.0.1:${server.address().port}`, messages, origin: () => origin };
};

test("smoke runs the configured Node command and reports a synced local server", async (t) => {
  const server = await fixture(t);
  const output = await run("smoke", [server.url], { ORIGIN: "https://fixture.invalid" });
  assert.match(output, /synced.*session: 7/);
  assert.match(output, /root: Fixture/);
  assert.equal(server.origin(), "https://fixture.invalid");
  assert.deepEqual(server.messages.map((m) => m.type), [0, 2]);
  assert.ok(server.messages[1].payload.includes(Buffer.from("smoke-")));
});

test("voicebot reads a spaced file path and sends Opus packets through Node", async (t) => {
  const server = await fixture(t);
  const dir = await mkdtemp(join(tmpdir(), "kallisti devtools "));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const opus = [248, 255, 254]; // An Opus silence packet, never played on a device.
  const packets = [Buffer.from("OpusHead"), Buffer.from("OpusTags"), Buffer.from(opus), Buffer.from(opus)];
  const header = Buffer.alloc(27);
  header.write("OggS");
  header[26] = packets.length;
  const file = join(dir, "silence.opus");
  await writeFile(file, Buffer.concat([header, Buffer.from(packets.map((p) => p.length)), ...packets]));
  const output = await run("voicebot", [file, server.url, "fixture-bot"]);
  assert.match(output, /synced, sending 2 packets \(protobuf\)/);
  assert.ok(server.messages.find((m) => m.type === 2).payload.includes(Buffer.from("fixture-bot")));
  // Protobuf target 0, frame numbers 0/2, Opus field 5, final terminator field 16.
  assert.deepEqual(server.messages.filter((m) => m.type === 1).map((m) => [...m.payload]), [
    [0, 8, 0, 42, 3, ...opus],
    [0, 8, 0, 32, 2, 42, 3, ...opus, 128, 1, 1],
  ]);
});
