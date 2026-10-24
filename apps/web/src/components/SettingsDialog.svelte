<script lang="ts">
  import type { ComponentProps } from "svelte";
  import ProfilePicture from "./ProfilePicture.svelte";
  import EmojiList from "./EmojiList.svelte";
  import { BIO_MAX, bioToSave } from "../lib/bio";
  import { notifier, permissionNote, type Permission } from "../lib/notify";
  import { browserPushSupport, pushIsOn, turnOffPush, turnOnPush, type PushPrefs } from "../lib/push";
  import { needsReauth, type AccountActions } from "../lib/accounts";
  import { handleHost } from "../lib/fedi";
  import { keyLabel, meterPercent, METER_MIN_DB, type Settings, type ThemeSetting } from "../lib/settings";
  import type { TimeFormat } from "../lib/chat";
  import { isDesktop } from "../lib/desktop";
  import { outputDeviceSupported } from "../lib/voice/support";
  import { noiseLabel, type NoiseMode } from "../lib/voice/noise";
  import { BUFFERS, bufferLabel, type PlaybackBuffer } from "../lib/voice/buffer";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";
  import FediAccount from "./FediAccount.svelte";

  let {
    settings,
    level,
    deviceLabel = "",
    fellBack = false,
    noiseError = null,
    voiceError = null,
    noiseRunning = null,
    onChange,
    onClose,
    onEcho,
    account,
    push,
    profile,
    bio,
    emoji,
  }: {
    settings: Settings;
    level: number;
    // The microphone really in use, and whether the chosen one failed.
    deviceLabel?: string;
    fellBack?: boolean;
    // Strong noise suppression failed to load (the standard one is on).
    noiseError?: string | null;
    // The browser cannot do voice, and why.
    voiceError?: string | null;
    // Our filter that runs now (null: the browser's, or still loading).
    noiseRunning?: NoiseMode | null;
    onChange: (patch: Partial<Settings>) => void;
    onClose: () => void;
    // The echo test: hear the microphone as others would (after filters).
    onEcho?: (on: boolean) => void;
    // Logged in to an account: log out, password.
    account?: AccountActions;
    // Push notifications for this account (see lib/push).
    push?: { base: string; name: string; prefs: PushPrefs };
    // Our avatar picture.
    profile?: ComponentProps<typeof ProfilePicture>;
    // Our bio as plain text (loading: a long one is not here yet).
    bio?: { text: string; loading: boolean; onSave: (text: string) => void };
    // Our personal emoji (registered users but guests).
    emoji?: ComponentProps<typeof EmojiList>;
  } = $props();

  // Saved when the field loses focus or the settings close. The draft
  // follows changes of the text only (not of the rest of our state), and
  // keeps the last text while a new long one loads (after our own save).
  let lastBio = "";
  const bioText = $derived.by(() => (bio && !bio.loading ? (lastBio = bio.text) : lastBio));
  let bioDraft = $derived(bioText);
  const saveBio = () => {
    const text = bio ? bioToSave(bioDraft, bioText) : null;
    if (text !== null) bio?.onSave(text);
  };

  // The switch shows the browser's subscription; changes wait for it.
  const pushSupport = browserPushSupport();
  let pushOn = $state(false);
  const pushSave = createSave();
  $effect(() => {
    if (push && pushSupport === "ok") void pushIsOn(push.base, push.name).then((on) => (pushOn = on));
  });
  const setPush = (el: HTMLInputElement) => {
    if (!push) return;
    const { base, name, prefs } = push;
    const on = el.checked;
    pushSave.run(
      async () => {
        const note = on ? await turnOnPush(base, name, prefs) : (await turnOffPush(base), null);
        pushOn = await pushIsOn(base, name);
        if (note || pushOn !== on) throw new Error(note ?? "The browser did not change it.");
      },
      () => (el.checked = pushOn),
    );
  };

  let dialog: HTMLDialogElement | undefined = $state();
  let echo = $state(false);
  $effect(() => {
    onEcho?.(echo);
  });
  $effect(() => () => onEcho?.(false)); // off when the settings close
  let devices = $state<MediaDeviceInfo[]>([]);
  let recordingKey = $state(false);
  const outputSupported = outputDeviceSupported();
  const inputs = $derived(devices.filter((d) => d.kind === "audioinput"));
  const outputs = $derived(devices.filter((d) => d.kind === "audiooutput"));
  // Cameras have no ids before the browser may use one.
  const hasCamera = $derived(devices.some((d) => d.kind === "videoinput"));
  const cameras = $derived(devices.filter((d) => d.kind === "videoinput" && d.deviceId));

  // Device labels are only visible after microphone permission was granted.
  const loadDevices = () =>
    navigator.mediaDevices
      .enumerateDevices()
      .then((list) => (devices = list))
      .catch((err) => console.warn("cannot list devices", err));

  $effect(() => {
    dialog?.showModal();
    void loadDevices();
    navigator.mediaDevices.addEventListener("devicechange", loadDevices);
    return () => navigator.mediaDevices.removeEventListener("devicechange", loadDevices);
  });

  // A guest keeps the account with a password, where the server takes new
  // accounts.
  let registration = $state(true);
  $effect(() => {
    if (account?.temporary) void account.registration().then((on) => (registration = on));
  });
  const canKeep = $derived(!account?.temporary || registration);

  let oldPassword = $state("");
  let newPassword = $state("");
  let passwordNote = $state<string | null>(null); // what changed (errors: passwordSave)
  const passwordSave = createSave();
  // The first password of a fediverse-only account needs a fresh login.
  let reauth = $state(false);
  const reauthSave = createSave();
  const changePassword = (event: SubmitEvent) => {
    event.preventDefault();
    passwordNote = null;
    passwordSave.run(async () => {
      const [kept, first] = [account?.temporary, account?.hasPassword === false];
      await account?.changePassword(oldPassword, newPassword).catch((err: unknown) => {
        reauth = needsReauth(err);
        throw err;
      });
      passwordNote = kept
        ? "Password set: your account stays."
        : first
          ? "Password set: you can also log in with your name. Other devices are logged out."
          : "Password changed. Other devices are logged out.";
      oldPassword = newPassword = "";
    });
  };

  const recordKey = (event: KeyboardEvent) => {
    if (!recordingKey) return;
    event.preventDefault();
    event.stopPropagation();
    if (event.code !== "Escape") onChange({ pttKey: event.code });
    recordingKey = false;
  };
  // Not granted after asking: blocked (or the prompt was dismissed).
  const blockedNote = (p: Permission) => permissionNote(p === "unsupported" ? p : "denied", isDesktop());
  let notificationNote = $state<string | null>(permissionNote(notifier().permission(), isDesktop()));
</script>

<svelte:window onkeydowncapture={recordKey} />

<dialog
  bind:this={dialog}
  onclose={() => {
    saveBio();
    onClose();
  }}
  aria-labelledby="settings-title">
  <header>
    <h2 id="settings-title">Settings</h2>
    <button class="close" onclick={() => dialog?.close()} aria-label="Close">×</button>
  </header>
  {#if voiceError}
    <p class="hint warn voice-error" role="status">{voiceError}</p>
  {/if}

  {#if profile}<ProfilePicture {...profile} />{/if}

  {#if bio}
    <label class="bio">
      <span class="label-row">About me <span class="count">{bioDraft.length}/{BIO_MAX}</span></span>
      <textarea rows="3" maxlength={BIO_MAX} disabled={bio.loading && !bioText} bind:value={bioDraft} onblur={saveBio}></textarea>
      <span class="hint">Everyone on the server sees it, in Mumble apps too.</span>
    </label>
  {/if}

  {#if emoji}
    <section class="my-emoji" aria-labelledby="my-emoji-title">
      <h3 id="my-emoji-title">My emoji</h3>
      <p class="hint">
        Type :name: to use one. Everyone sees them in your messages and reactions; Mumble apps show :name:. To add one from a
        message, click it.
      </p>
      <EmojiList {...emoji} />
    </section>
  {/if}

  <label>
    Theme
    <select value={settings.theme} onchange={(e) => onChange({ theme: e.currentTarget.value as ThemeSetting })}>
      <option value="dark">Dark</option>
      <option value="light">Light</option>
      <option value="system">As the system</option>
    </select>
  </label>

  <label>
    Time format
    <select value={settings.timeFormat} onchange={(e) => onChange({ timeFormat: e.currentTarget.value as TimeFormat })}>
      <option value="auto">As the language</option>
      <option value="24">24-hour (19:05)</option>
      <option value="12">12-hour (7:05 PM)</option>
    </select>
  </label>

  <label>
    Input device
    <select
      value={settings.inputDeviceId ?? ""}
      onchange={(e) => onChange({ inputDeviceId: e.currentTarget.value || null })}
    >
      <option value="">Default</option>
      {#each inputs as d (d.deviceId)}
        {#if d.deviceId !== "default"}
          <option value={d.deviceId}>{d.label || "Microphone"}</option>
        {/if}
      {/each}
    </select>
    {#if fellBack}
      <span class="hint warn" role="status">
        The chosen microphone is not available. Using {deviceLabel || "the default microphone"}.
      </span>
    {:else if deviceLabel}
      <span class="hint">In use: {deviceLabel}</span>
    {/if}
  </label>

  {#if hasCamera}
    <label>
      Camera
      <select value={settings.cameraDeviceId ?? ""} onchange={(e) => onChange({ cameraDeviceId: e.currentTarget.value || null })}>
        <option value="">Default</option>
        {#each cameras as d, i (d.deviceId)}
          <option value={d.deviceId}>{d.label || `Camera ${i + 1}`}</option>
        {/each}
      </select>
      {#if !cameras.length}<span class="hint">The list fills once you have turned the camera on in voice.</span>{/if}
    </label>
  {/if}

  <label>
    Noise suppression
    <select
      value={settings.noiseSuppression}
      disabled={settings.musicMode}
      onchange={(e) => onChange({ noiseSuppression: e.currentTarget.value as NoiseMode })}
    >
      <option value="browser">Standard (the browser's)</option>
      <option value="strong">Strong (RNNoise)</option>
      <option value="strongest">Strongest (DeepFilterNet3)</option>
    </select>
    {#if settings.musicMode}
      <span class="hint">Off in music mode.</span>
    {:else if noiseError}
      <span class="hint warn" role="status">{noiseError}</span>
    {:else if noiseRunning && noiseRunning === settings.noiseSuppression}
      <span class="hint" role="status">{noiseLabel(noiseRunning)} is running.</span>
    {:else if settings.noiseSuppression === "strongest"}
      <span class="hint" role="status">Loading DeepFilterNet3 (about 12 MB, once)…</span>
    {:else}
      <span class="hint">
        Strong removes fan and other steady noise; typing alone does not start sending. Strongest also removes most
        typing, but loads about 12 MB, uses more CPU and adds a little delay.
      </span>
    {/if}
  </label>
  {#if onEcho}
    <div class="row">
      <span class="label">Hear yourself</span>
      <div class="buttons">
        <button type="button" class="secondary" aria-pressed={echo} onclick={() => (echo = !echo)}>
          {echo ? "Stop" : "Start echo test"}
        </button>
      </div>
      <p class="hint">Plays your microphone as the others hear it, after noise suppression. Use headphones.</p>
    </div>
  {/if}

  {#if outputSupported}
    <label>
      Output device
      <select
        value={settings.outputDeviceId ?? ""}
        onchange={(e) => onChange({ outputDeviceId: e.currentTarget.value || null })}
      >
        <option value="">Default</option>
        {#each outputs as d (d.deviceId)}
          {#if d.deviceId !== "default"}
            <option value={d.deviceId}>{d.label || "Speakers"}</option>
          {/if}
        {/each}
      </select>
    </label>
  {/if}
  <label>
    Playback buffer
    <select
      value={settings.playbackBuffer}
      onchange={(e) => onChange({ playbackBuffer: e.currentTarget.value as PlaybackBuffer })}
    >
      {#each BUFFERS as b (b)}
        <option value={b}>{bufferLabel(b)}</option>
      {/each}
    </select>
    <span class="hint">
      How long others' voices wait before they play. More fills gaps on a bad connection, but adds delay. Music
      always gets at least 500 ms.
    </span>
  </label>
  <label class="check">
    <input type="checkbox" checked={settings.sounds} onchange={(e) => onChange({ sounds: e.currentTarget.checked })} />
    <span>Sounds</span>
    <span class="hint">When you mute or deafen yourself, and undeafen.</span>
  </label>
  {#if settings.sounds}
    <label>
      Sound volume
      <input
        type="range"
        min="0"
        max="1"
        step="0.05"
        value={settings.soundVolume}
        oninput={(e) => onChange({ soundVolume: Number(e.currentTarget.value) })}
        aria-valuetext="{Math.round(settings.soundVolume * 100)}%"
      />
    </label>
  {/if}

  <fieldset>
    <legend>Input mode</legend>
    <label class="radio">
      <input
        type="radio"
        name="mode"
        checked={settings.mode === "vad"}
        onchange={() => onChange({ mode: "vad" })}
      />
      Voice activity
    </label>
    <label class="radio">
      <input
        type="radio"
        name="mode"
        checked={settings.mode === "ptt"}
        onchange={() => onChange({ mode: "ptt" })}
      />
      Push to talk
    </label>
  </fieldset>

  {#if settings.mode === "ptt"}
    <div class="row">
      <span class="label">Push-to-talk key</span>
      <button
        class="key"
        class:recording={recordingKey}
        aria-pressed={recordingKey}
        onclick={() => (recordingKey = true)}
      >
        {recordingKey ? "Press a key… (Esc to cancel)" : keyLabel(settings.pttKey)}
      </button>
      <p class="hint">
        {isDesktop()
          ? "Works in the background too. The key is reserved for Kallisti while the app runs."
          : "Works only while this tab has focus."}
      </p>
    </div>
  {:else}
    <div class="row">
      <span class="label">Sensitivity</span>
      <div class="meter" aria-hidden="true">
        <div
          class="level"
          class:active={level >= settings.vadThresholdDb}
          style:width="{meterPercent(level)}%"
        ></div>
      </div>
      <input
        type="range"
        min={METER_MIN_DB}
        max="0"
        step="1"
        value={settings.vadThresholdDb}
        oninput={(e) => onChange({ vadThresholdDb: Number(e.currentTarget.value) })}
        aria-label="Voice activity threshold"
        aria-valuetext="{settings.vadThresholdDb} dB"
      />
      <p class="hint">
        Speak normally: the bar should turn green when you talk, and stay grey when you are quiet.
      </p>
    </div>
  {/if}
  <label class="check">
    <input
      type="checkbox"
      checked={settings.echoCancellation && !settings.musicMode}
      disabled={settings.musicMode}
      onchange={(e) => onChange({ echoCancellation: e.currentTarget.checked })}
    />
    <span>Echo cancellation</span>
    <span class="hint">Needed with speakers. With a headset you can turn it off: on a Mac, the desktop app and Safari otherwise switch Bluetooth headsets to their call mode, with worse sound.</span>
  </label>
  <label class="check">
    <input type="checkbox" checked={settings.musicMode} onchange={(e) => onChange({ musicMode: e.currentTarget.checked })} />
    <span>Music mode</span>
    <span class="hint">For playing music: no noise suppression, echo cancellation or automatic volume, and better sound (stereo if the microphone has two channels). Use a headset.</span>
  </label>
  <label class="check">
    <input
      type="checkbox"
      checked={settings.closeMicWhenMuted}
      onchange={(e) => onChange({ closeMicWhenMuted: e.currentTarget.checked })}
    />
    <span>Close the microphone when muted</span>
    <span class="hint">The browser then stops showing it as recording, but the sound stops for a moment when you mute or unmute.</span>
  </label>
  <label class="check">
    <input type="checkbox" checked={settings.streamPreviews} onchange={(e) => onChange({ streamPreviews: e.currentTarget.checked })} />
    <span>Show a preview of my screen share and camera</span>
    <span class="hint">People who are not watching see a small picture, updated every few seconds.</span>
  </label>
  <label class="check">
    <input
      type="checkbox"
      checked={settings.notifications}
      onchange={async (e) => {
        const box = e.currentTarget;
        const n = notifier();
        let [on, permission] = [box.checked, n.permission()];
        if (on && permission !== "granted" && permission !== "unsupported") permission = await n.request();
        on &&= permission === "granted";
        notificationNote = on || !box.checked ? null : blockedNote(permission);
        box.checked = on;
        onChange({ notifications: on });
      }}
    />
    <span>Desktop notifications (mentions and direct messages; per channel in its menu)</span>
    {#if notificationNote}<span class="hint warn" role="status">{notificationNote}</span>{/if}
  </label>
  {#if push && pushSupport === "ok"}
    <label class="check">
      <input type="checkbox" checked={pushOn} disabled={pushSave.busy} onchange={(e) => setPush(e.currentTarget)} />
      <span>Push notifications on this device</span>
      <span class="hint">Mentions and direct messages also while Kallisti is closed. They say who and where, not the message.</span>
      <SaveStatus save={pushSave.state} />
    </label>
  {:else if push && pushSupport === "install"}
    <p class="hint">
      For push notifications on this iPhone or iPad: tap Share, then "Add to Home Screen", and open Kallisti from there
      (iOS 16.4 or later).
    </p>
  {/if}
  {#if account}
    {#if canKeep}
    <form class="account" onsubmit={changePassword}>
      {#if account.temporary}
        <span class="label">Keep this account</span>
        <p class="hint">
          This is a guest account: it is deleted a day after your last visit. With a password it stays, and you can log
          in on other devices.
        </p>
      {:else if !account.hasPassword}
        <span class="label">Set a password</span>
        <p class="hint">You log in with your fediverse account. A password also works in Mumble apps, with your name. First you confirm with your fediverse server.</p>
      {:else}
        <span class="label">Change password</span>
        <input type="password" bind:value={oldPassword} placeholder="Current password" autocomplete="current-password" required />
      {/if}
      <input type="password" bind:value={newPassword} placeholder="New password (8 or more characters)" autocomplete="new-password" minlength="8" required />
      {#if passwordNote}<p class="hint" role="status">{passwordNote}</p>{/if}
      <div class="buttons">
        <button type="submit" class="secondary" disabled={passwordSave.busy}>
          {busyLabel(passwordSave.state, account.temporary || !account.hasPassword ? "Set password" : "Change password")}
        </button>
        <SaveStatus save={passwordSave.state} done="" />
      </div>
      <!-- The first password: confirm with the fediverse account first (again
           after a refusal: the confirmation lasts a few minutes, once). Not
           in the desktop app, which cannot come back from the instance. -->
      {#if (reauth || (!account.hasPassword && !account.temporary)) && account.fedi && !isDesktop()}
        <div class="buttons">
          <button type="button" class="secondary" disabled={reauthSave.busy} onclick={() => reauthSave.run(account.fediReauth)}>
            Confirm with {handleHost(account.fedi.handle)}
          </button>
          <SaveStatus save={reauthSave.state} done="" />
        </div>
      {/if}
    </form>
    {/if}
    <FediAccount {account} {canKeep} />
    <div class="account">
      <span class="label">Log out</span>
      {#if account.temporary}
        <p class="hint">Logging out ends this guest account{canKeep ? " (set a password above to keep it)" : ""}.</p>
      {:else}
        <p class="hint">"Everywhere" also logs out your other browsers and devices, for example after you used a shared computer.</p>
      {/if}
      <div class="buttons">
        <button type="button" class="logout" onclick={account.logout}>Log out</button>
        {#if !account.temporary}
          <button type="button" class="logout" onclick={account.logoutAll}>Log out everywhere</button>
        {/if}
      </div>
    </div>
  {/if}
  <footer class="hint">
    Kallisti: a Mumble server and client. <a href="https://kallisti.chat" target="_blank" rel="noreferrer">kallisti.chat</a>
  </footer>
</dialog>

<style>
  dialog {
    width: min(440px, 92vw);
    padding: 24px;
    border: none;
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-bottom: 16px;
  }
  h2 {
    margin: 0;
    font-size: 18px;
  }
  .close {
    font-size: 24px;
    line-height: 1;
    color: var(--text-muted);
  }
  label,
  .row,
  fieldset {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0 0 16px;
  }
  label,
  .label,
  legend {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  fieldset {
    border: none;
    padding: 0;
  }
  legend {
    padding: 0;
    margin-bottom: 6px;
  }
  .radio {
    flex-direction: row;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 14px;
    font-weight: normal;
    text-transform: none;
    color: var(--text);
  }
  select,
  .key {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    text-transform: none;
  }
  .key {
    text-align: left;
  }
  .key.recording {
    outline: 2px solid var(--accent);
  }
  .meter {
    height: 8px;
    border-radius: 4px;
    background: var(--bg-rail);
    overflow: hidden;
  }
  .level {
    height: 100%;
    background: var(--text-faint);
    transition: width 0.05s linear;
  }
  .level.active {
    background: var(--accent-strong);
  }
  input[type="range"],
  input[type="radio"] {
    accent-color: var(--accent);
  }
  .voice-error {
    margin-bottom: 16px;
    font-size: 14px;
  }
  .hint.warn {
    color: var(--danger);
  }
  /* Checkbox, text, and a hint under the text. */
  .check {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    column-gap: 8px;
    row-gap: 2px;
    text-transform: none;
    font-weight: normal;
  }
  .check .hint {
    grid-column: 2;
  }
  .account {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 12px;
    margin-bottom: 16px;
    border-top: 1px solid var(--border-subtle);
  }
  footer {
    padding-top: 12px;
    border-top: 1px solid var(--border-subtle);
  }
  footer a {
    color: var(--link);
  }
  .account input {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
  }
  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .secondary {
    padding: 8px 14px;
    border-radius: 6px;
    background: var(--bg-input);
    font-weight: 600;
  }
  .logout {
    padding: 8px 14px;
    border-radius: 6px;
    background: var(--bg-input);
    color: var(--danger);
    font-weight: 600;
  }
  .logout:hover {
    background: var(--bg-hover);
  }
  .bio {
    padding-bottom: 16px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .label-row {
    display: flex;
    justify-content: space-between;
  }
  .count {
    font-weight: normal;
    color: var(--text-faint);
  }
  textarea {
    padding: 10px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
    font-size: 14px;
    text-transform: none;
    resize: vertical;
  }
  .hint {
    margin: 0;
    text-transform: none;
    font-weight: normal;
    font-size: 12px;
    color: var(--text-faint);
  }
  .my-emoji {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .my-emoji h3 {
    margin: 0;
    font-size: 14px;
  }
</style>
