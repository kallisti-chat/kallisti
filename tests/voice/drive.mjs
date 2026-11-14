// Drives a browser through WebDriver for the voice test (see run.sh): join
// as a guest, replace the microphone with an oscillator, join the
// "General" voice channel, and talk in spurts with pauses. voicecheck
// listens and judges the packets.
//
//   node drive.mjs <browser> <webdriver URL> <app URL>
//
// No dependencies: plain W3C WebDriver over fetch.

const [browser, driverUrl, appUrl] = process.argv.slice(2);
const SPURTS = 4;
const ELEMENT = "element-6066-11e4-a52e-4f735466cecf";

const capabilities = {
  chrome: {
    browserName: "chrome",
    "goog:chromeOptions": { args: ["--headless=new", "--autoplay-policy=no-user-gesture-required", "--use-fake-ui-for-media-stream"] },
  },
  firefox: {
    browserName: "firefox",
    "moz:firefoxOptions": {
      args: ["-headless"],
      prefs: { "media.navigator.permission.disabled": true, "media.autoplay.default": 0, "media.autoplay.blocking_policy": 0 },
    },
  },
  safari: { browserName: "safari" },
}[browser];
if (!capabilities) throw new Error(`unknown browser ${browser}`);

const call = async (method, path, body) => {
  const res = await fetch(driverUrl + path, {
    method,
    headers: { "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const json = await res.json();
  if (!res.ok) throw new Error(`${method} ${path}: ${JSON.stringify(json.value)}`);
  return json.value;
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The microphone: an oscillator behind a gain the test turns up and down.
// Devices are stubbed too (headless Chrome crashes on enumerateDevices).
const stubMicrophone = `
  const trace = window.__voiceTest;
  trace.micStage = 'installed';
  const md = navigator.mediaDevices;
  md.enumerateDevices = async () => [];
  md.getUserMedia = async () => {
    trace.micRequests++;
    try {
      trace.micStage = 'context';
      const c = new AudioContext({ sampleRate: 48000 });
      trace.micStage = 'nodes';
      const o = c.createOscillator();
      o.frequency.value = 440;
      const g = c.createGain();
      g.gain.value = 0;
      window.__mic = g;
      const d = c.createMediaStreamDestination();
      o.connect(g);
      g.connect(d);
      o.start();
      trace.micStage = 'resume';
      await c.resume();
      trace.micStage = 'running';
      return d.stream;
    } catch (error) {
      trace.errors.push('microphone: ' + String(error));
      throw error;
    }
  };`;

const { sessionId } = await call("POST", "/session", { capabilities: { alwaysMatch: capabilities } });
const s = `/session/${sessionId}`;
const run = (script, args = []) => call("POST", `${s}/execute/sync`, { script, args });
const waitFor = async (what, script, timeout = 20_000) => {
  for (const end = Date.now() + timeout; Date.now() < end; await sleep(250)) {
    const found = await run(script);
    if (found) return found;
  }
  throw new Error(`timed out waiting for ${what}`);
};
const click = (el) => call("POST", `${s}/element/${el[ELEMENT]}/click`, {});

try {
  await call("POST", `${s}/url`, { url: appUrl });
  await run(`
    const trace = window.__voiceTest = { micRequests: 0, micStage: 'not installed', clicks: [], errors: [] };
    const record = (value) => { if (trace.errors.length < 20) trace.errors.push(String(value).slice(0, 2000)); };
    window.addEventListener('error', (event) => record(event.error ?? event.message));
    window.addEventListener('unhandledrejection', (event) => record(event.reason));
    document.addEventListener('click', (event) => {
      const button = event.target.closest?.('button');
      if (button && trace.clicks.length < 20) trace.clicks.push(button.textContent.trim().slice(0, 100));
    }, true);
  `);
  // "Log in" comes first.
  await click(await waitFor("the Guest tab", `return [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Guest") ?? null`));
  const name = await waitFor("the guest form", `return document.querySelector("button[aria-pressed=true]")?.textContent.trim() === "Guest" ? document.querySelector("input[required]") : null`);
  await call("POST", `${s}/element/${name[ELEMENT]}/value`, { text: `${browser}${Math.floor(Math.random() * 1000)}` });
  await click(await waitFor("the join button", `return document.querySelector("button[type=submit]")`));
  const general = await waitFor(
    "the General voice channel",
    `return [...document.querySelectorAll("nav button")].find((b) => /^\\s*General/.test(b.textContent)) ?? null`,
  );
  await run(stubMicrophone);
  await click(general); // a real click: a user gesture for audio
  // Safari on the CI machine can take over 20 s to start the microphone.
  await waitFor("the microphone", `return !!window.__mic`, browser === "safari" ? 45_000 : 20_000);
  console.log("voice:", await run(`return document.querySelector('[aria-label="Your status"]')?.textContent.replace(/\\s+/g, " ")`));
  console.log("audio:", await run(`return window.__mic.context.state`));
  await sleep(2000);
  for (let i = 0; i < SPURTS; i++) {
    await run(`window.__mic.gain.value = 0.5`);
    await sleep(1500);
    await run(`window.__mic.gain.value = 0`);
    await sleep(2000);
  }
  await sleep(1000);
} catch (error) {
  try {
    console.error("voice startup diagnostics:", JSON.stringify(await run(`return {
      trace: window.__voiceTest,
      status: document.querySelector('[aria-label="Your status"]')?.textContent,
      alerts: [...document.querySelectorAll('[role="alert"], .mic-error')].map((el) => el.textContent.slice(0, 2000)),
      channels: [...document.querySelectorAll('nav button[aria-current], nav button[aria-pressed="true"]')].map((el) => el.textContent.trim()),
      secureContext: window.isSecureContext,
      audioContext: typeof AudioContext,
      mediaDevices: !!navigator.mediaDevices,
      microphoneContext: window.__mic?.context.state,
    }`)));
  } catch (diagnosticError) {
    console.error("voice diagnostics unavailable:", String(diagnosticError));
  }
  throw error;
} finally {
  await call("DELETE", s).catch(() => {});
}
