<script lang="ts">
  import type { ComponentProps } from "svelte";
  import { GROUP_TITLES, sectionsFor, type SectionId } from "../lib/settingsSections";
  import CategoryDialog from "./CategoryDialog.svelte";
  import ProfilePicture from "./ProfilePicture.svelte";
  import EmojiList from "./EmojiList.svelte";
  import { BIO_MAX, bioToSave } from "../lib/bio";
  import { notifier, permissionNote, type Permission } from "../lib/notify";
  import { browserPushSupport, pushIsOn, turnOffPush, turnOnPush, type PushPrefs } from "../lib/push";
  import { needsReauth, type AccountActions } from "../lib/accounts";
  import { handleHost } from "../lib/fedi";
  import { i18n, t } from "../lib/i18n.svelte";
  import { around, LOCALE_NAMES, LOCALES, type Language } from "../lib/i18n";
  import { keyLabel, meterPercent, METER_MIN_DB, type Settings, type ThemeSetting } from "../lib/settings";
  import type { TimeFormat } from "../lib/chat";
  import { isDesktop } from "../lib/desktop";
  import { outputDeviceSupported } from "../lib/voice/support";
  import { noiseLabel, type NoiseMode } from "../lib/voice/noise";
  import { BUFFER_MS, BUFFERS, bufferLabel, MAX_BUFFER_MS, MIN_BUFFER_MS, toBufferMs, type PlaybackBuffer } from "../lib/voice/buffer";
  import type { AudioChannels } from "../lib/voice/opus";
  import type { QualityLimits } from "../lib/textChannels";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import SaveStatus from "./SaveStatus.svelte";
  import FediAccount from "./FediAccount.svelte";
  import AllowedSites from "./AllowedSites.svelte";
  import QrShowDialog from "./QrShowDialog.svelte";
  import { webGPU } from "../lib/translate/llm";

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
    phone = false,
    start,
    qualityLimits,
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
    // Phones: a list of categories, each its own page (see lib/settingsSections).
    phone?: boolean;
    // The category to open at (for example the account's, to set a password).
    start?: SectionId;
    // Quality limits from the server.
    qualityLimits?: QualityLimits;
  } = $props();

  // The categories (see CategoryDialog), and the one open.
  const sections = $derived(sectionsFor({ profile: !!(profile || bio || emoji), account: !!account }).map((s) => ({ ...s, title: t(s.title) })));
  const groupTitles = $derived({ user: t(GROUP_TITLES.user), app: t(GROUP_TITLES.app) });
  const footer = $derived(around(t("settings.footer"), "link"));
  let section = $state<SectionId | null>(null);
  // Better translation needs WebGPU with 16-bit floats (asked once).
  let gpu = $state(false);
  void webGPU().then((ok) => (gpu = ok));

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
        if (note || pushOn !== on) throw new Error(note ?? t("settings.pushNotChanged"));
      },
      () => (el.checked = pushOn),
    );
  };

  let dialog: HTMLDialogElement | undefined = $state();
  let echo = $state(false);
  $effect(() => {
    onEcho?.(echo);
  });
  // The echo test and a key being recorded end with their category.
  $effect(() => {
    if (section !== "voice") echo = recordingKey = false;
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
    void loadDevices();
    navigator.mediaDevices.addEventListener("devicechange", loadDevices);
    return () => navigator.mediaDevices.removeEventListener("devicechange", loadDevices);
  });

  let oldPassword = $state("");
  let newPassword = $state("");
  let passwordNote = $state<string | null>(null); // what changed (errors: passwordSave)
  const passwordSave = createSave();
  // A login code for another device (see lib/qrLogin).
  let qrShown = $state(false);
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
        ? t("settings.passwordSetKept")
        : first
          ? t("settings.passwordSetFirst")
          : t("settings.passwordChanged");
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

<CategoryDialog
  title={t("common.settings")}
  categories={sections}
  {groupTitles}
  {start}
  {phone}
  hideOnPhone={profile ? ["profile"] : []}
  bind:section
  bind:dialog
  onClose={() => {
    saveBio();
    onClose();
  }}
>
  {#snippet menuTop(open: (id: SectionId) => void)}
    {#if phone && profile}
      <button class="me" onclick={() => open("profile")}>
        <span class="me-name">{profile.name}</span>
        <span class="me-edit">{t("settings.editProfile")}</span>
      </button>
    {/if}
  {/snippet}
  {#snippet menuEnd()}
    <!-- Guests log out on Account, where it says that this ends the account. -->
    {#if account && !account.temporary}
      {#if !phone}<div class="sep"></div>{/if}
      <button class="item danger" onclick={account.logout}>{t("settings.logOut")}</button>
    {/if}
    <footer class="hint menu-footer">
      {footer[0]}<a href="https://kallisti.chat" target="_blank" rel="noreferrer">kallisti.chat</a>{footer[1]}
    </footer>
  {/snippet}
  {#snippet content(id: SectionId)}
    {#if id === "profile"}
      {#if profile}<ProfilePicture {...profile} />{/if}

      {#if bio}
        <label class="bio">
          <span class="label-row">{t("settings.aboutMe")} <span class="count">{bioDraft.length}/{BIO_MAX}</span></span>
          <textarea rows="3" maxlength={BIO_MAX} disabled={bio.loading && !bioText} bind:value={bioDraft} onblur={saveBio}></textarea>
          <span class="hint">{t("settings.bioHint")}</span>
        </label>
      {/if}

      {#if emoji}
        <section class="my-emoji" aria-labelledby="my-emoji-title">
          <h4 id="my-emoji-title">{t("settings.myEmoji")}</h4>
          <p class="hint">{t("settings.myEmojiHint")}</p>
          <EmojiList {...emoji} />
        </section>
      {/if}
    {:else if id === "account" && account}
        <form class="account" onsubmit={changePassword}>
          {#if account.temporary}
            <span class="label">{t("settings.keepAccount")}</span>
            <p class="hint">{t("settings.guestAccountHint")}</p>
          {:else if !account.hasPassword}
            <span class="label">{t("settings.setPasswordTitle")}</span>
            <p class="hint">{t("settings.fediOnlyHint")}</p>
          {:else}
            <span class="label">{t("settings.changePassword")}</span>
            <input type="password" bind:value={oldPassword} placeholder={t("settings.currentPassword")} autocomplete="current-password" required />
          {/if}
          <input type="password" bind:value={newPassword} placeholder={t("settings.newPassword")} autocomplete="new-password" minlength="8" required />
          {#if passwordNote}<p class="hint" role="status">{passwordNote}</p>{/if}
          <div class="buttons">
            <button type="submit" class="secondary" disabled={passwordSave.busy}>
              {busyLabel(passwordSave.state, account.temporary || !account.hasPassword ? t("settings.setPassword") : t("settings.changePassword"))}
            </button>
            <SaveStatus save={passwordSave.state} done="" />
          </div>
          <!-- The first password: confirm with the fediverse account first (again
               after a refusal: the confirmation lasts a few minutes, once). Not
               in the desktop app, which cannot come back from the instance. -->
          {#if (reauth || (!account.hasPassword && !account.temporary)) && account.fedi && !isDesktop()}
            <div class="buttons">
              <button type="button" class="secondary" disabled={reauthSave.busy} onclick={() => reauthSave.run(account.fediReauth)}>
                {t("settings.confirmWith", { host: handleHost(account.fedi.handle) })}
              </button>
              <SaveStatus save={reauthSave.state} done="" />
            </div>
          {/if}
        </form>
        <FediAccount {account} />
        {#if account.sites && !account.temporary}<AllowedSites sites={account.sites} />{/if}
        {#if account.qr && !account.temporary}
          <div class="account">
            <span class="label">{t("settings.otherDevice")}</span>
            <p class="hint">
              {t("settings.otherDeviceHint")}{#if account.qr.scan}{" "}{t("settings.otherDeviceScanHint")}{/if}
            </p>
            <div class="buttons">
              <button type="button" class="secondary" onclick={() => (qrShown = true)}>{t("settings.showLoginCode")}</button>
              {#if account.qr.scan}<button type="button" class="secondary" onclick={account.qr.scan}>{t("settings.scanCode")}</button>{/if}
            </div>
          </div>
        {/if}
        <div class="account">
          <span class="label">{t("settings.logOut")}</span>
          {#if account.temporary}
            <p class="hint">{t("settings.logOutGuestHint")}</p>
          {:else}
            <p class="hint">{t("settings.logOutEverywhereHint")}</p>
          {/if}
          <div class="buttons">
            <button type="button" class="logout" onclick={account.logout}>{t("settings.logOut")}</button>
            {#if !account.temporary}
              <button type="button" class="logout" onclick={account.logoutAll}>{t("settings.logOutEverywhere")}</button>
            {/if}
          </div>
        </div>
  
    {:else if id === "voice"}
      {#if voiceError}
        <p class="hint warn voice-error" role="status">{voiceError}</p>
      {/if}
      <label>
        {t("settings.inputDevice")}
        <select
          value={settings.inputDeviceId ?? ""}
          onchange={(e) => onChange({ inputDeviceId: e.currentTarget.value || null })}
        >
          <option value="">{t("settings.deviceDefault")}</option>
          {#each inputs as d (d.deviceId)}
            {#if d.deviceId !== "default"}
              <option value={d.deviceId}>{d.label || t("settings.microphone")}</option>
            {/if}
          {/each}
        </select>
        {#if fellBack}
          <span class="hint warn" role="status">
            {deviceLabel ? t("settings.micFellBack", { device: deviceLabel }) : t("settings.micFellBackDefault")}
          </span>
        {:else if deviceLabel}
          <span class="hint">{t("settings.micInUse", { device: deviceLabel })}</span>
        {/if}
      </label>

      {#if hasCamera}
        <label>
          {t("settings.camera")}
          <select value={settings.cameraDeviceId ?? ""} onchange={(e) => onChange({ cameraDeviceId: e.currentTarget.value || null })}>
            <option value="">{t("settings.deviceDefault")}</option>
            {#each cameras as d, i (d.deviceId)}
              <option value={d.deviceId}>{d.label || t("settings.cameraNumbered", { number: i + 1 })}</option>
            {/each}
          </select>
          {#if !cameras.length}<span class="hint">{t("settings.cameraListHint")}</span>{/if}
        </label>
      {/if}

      <label>
        {t("settings.noiseSuppression")}
        <select
          value={settings.noiseSuppression}
          disabled={settings.musicMode}
          onchange={(e) => onChange({ noiseSuppression: e.currentTarget.value as NoiseMode })}
        >
          <option value="off">{t("settings.noiseOff")}</option>
          <option value="browser">{t("settings.noiseStandard")}</option>
          <option value="strong">{t("settings.noiseStrong")}</option>
          <option value="strongest">{t("settings.noiseStrongest")}</option>
        </select>
        {#if settings.musicMode}
          <span class="hint">{t("settings.noiseOffMusic")}</span>
        {:else if noiseError}
          <span class="hint warn" role="status">{noiseError}</span>
        {:else if noiseRunning && noiseRunning === settings.noiseSuppression}
          <span class="hint" role="status">{t("settings.noiseRunning", { filter: noiseLabel(noiseRunning) })}</span>
        {:else if settings.noiseSuppression === "strongest"}
          <span class="hint" role="status">{t("settings.noiseLoading")}</span>
        {:else}
          <span class="hint">{t("settings.noiseHint")}</span>
        {/if}
      </label>
      {#if onEcho}
        <div class="row">
          <span class="label">{t("settings.hearYourself")}</span>
          <div class="buttons">
            <button type="button" class="secondary" aria-pressed={echo} onclick={() => (echo = !echo)}>
              {echo ? t("settings.echoTestStop") : t("settings.echoTestStart")}
            </button>
          </div>
          <p class="hint">{t("settings.echoTestHint")}</p>
        </div>
      {/if}

      {#if outputSupported}
        <label>
          {t("settings.outputDevice")}
          <select
            value={settings.outputDeviceId ?? ""}
            onchange={(e) => onChange({ outputDeviceId: e.currentTarget.value || null })}
          >
            <option value="">{t("settings.deviceDefault")}</option>
            {#each outputs as d (d.deviceId)}
              {#if d.deviceId !== "default"}
                <option value={d.deviceId}>{d.label || t("settings.speakers")}</option>
              {/if}
            {/each}
          </select>
        </label>
      {/if}

      <div class="row">
        <div class="header-with-value">
          <span class="label">{t("settings.playbackBuffer")}</span>
          <span class="value-readout">{toBufferMs(settings.playbackBuffer)} ms</span>
        </div>
        <input
          type="range"
          min={MIN_BUFFER_MS}
          max={MAX_BUFFER_MS}
          step="10"
          list="buffer-ticks"
          value={toBufferMs(settings.playbackBuffer)}
          oninput={(e) => onChange({ playbackBuffer: Number(e.currentTarget.value) })}
          aria-label={t("settings.playbackBuffer")}
        />
        <datalist id="buffer-ticks">
          <option value="60" label="60 ms"></option>
          <option value="120" label="120 ms"></option>
          <option value="250" label="250 ms"></option>
        </datalist>
        <div class="presets">
          {#each BUFFERS as b (b)}
            <button
              type="button"
              class="preset-btn"
              class:selected={toBufferMs(settings.playbackBuffer) === BUFFER_MS[b]}
              onclick={() => onChange({ playbackBuffer: b })}
            >
              {bufferLabel(b)}
            </button>
          {/each}
        </div>
        <p class="hint">{t("settings.playbackBufferHint")}</p>
      </div>

      <div class="row">
        <div class="header-with-value">
          <span class="label">{t("settings.voiceBitrate")}</span>
          <span class="value-readout">{Math.round((settings.voiceBitrate ?? 40_000) / 1000)} kbps</span>
        </div>
        <input
          type="range"
          min="16"
          max={Math.round((qualityLimits?.maxBitrateVoice ?? 128_000) / 1000)}
          step="1"
          value={Math.round((settings.voiceBitrate ?? 40_000) / 1000)}
          oninput={(e) => onChange({ voiceBitrate: Number(e.currentTarget.value) * 1000 })}
          aria-label={t("settings.voiceBitrate")}
        />
        <p class="hint">{t("settings.voiceBitrateHint")}</p>
      </div>

      <div class="row">
        <div class="header-with-value">
          <span class="label">{t("settings.musicBitrate")}</span>
          <span class="value-readout">{Math.round((settings.musicBitrate ?? 128_000) / 1000)} kbps</span>
        </div>
        <input
          type="range"
          min="32"
          max={Math.round((qualityLimits?.maxBitrateMusic ?? 320_000) / 1000)}
          step="8"
          value={Math.round((settings.musicBitrate ?? 128_000) / 1000)}
          oninput={(e) => onChange({ musicBitrate: Number(e.currentTarget.value) * 1000 })}
          aria-label={t("settings.musicBitrate")}
        />
        <p class="hint">{t("settings.musicBitrateHint")}</p>
      </div>

      <label>
        {t("settings.audioChannels")}
        <select
          value={settings.audioChannels ?? "auto"}
          onchange={(e) => onChange({ audioChannels: e.currentTarget.value as AudioChannels })}
        >
          <option value="auto">{t("settings.audioChannelsAuto")}</option>
          <option value="mono">{t("settings.audioChannelsMono")}</option>
          <option value="stereo">{t("settings.audioChannelsStereo")}</option>
        </select>
        <span class="hint">{t("settings.audioChannelsHint")}</span>
      </label>
      <fieldset>
        <legend>{t("settings.inputMode")}</legend>
        <label class="radio">
          <input
            type="radio"
            name="mode"
            checked={settings.mode === "vad"}
            onchange={() => onChange({ mode: "vad" })}
          />
          {t("settings.voiceActivity")}
        </label>
        <label class="radio">
          <input
            type="radio"
            name="mode"
            checked={settings.mode === "ptt"}
            onchange={() => onChange({ mode: "ptt" })}
          />
          {t("settings.pushToTalk")}
        </label>
      </fieldset>

      {#if settings.mode === "ptt"}
        <div class="row">
          <span class="label">{t("settings.pttKey")}</span>
          <button
            class="key"
            class:recording={recordingKey}
            aria-pressed={recordingKey}
            onclick={() => (recordingKey = true)}
          >
            {recordingKey ? t("settings.pttRecording") : keyLabel(settings.pttKey)}
          </button>
          <p class="hint">
            {isDesktop()
              ? t("settings.pttHintDesktop")
              : t("settings.pttHintBrowser")}
          </p>
        </div>
      {:else}
        <div class="row">
          <span class="label">{t("settings.sensitivity")}</span>
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
            aria-label={t("settings.vadThreshold")}
            aria-valuetext={t("settings.decibels", { value: settings.vadThresholdDb })}
          />
          <p class="hint">{t("settings.sensitivityHint")}</p>
        </div>
      {/if}
      <label class="check">
        <input
          type="checkbox"
          checked={settings.echoCancellation && !settings.musicMode}
          disabled={settings.musicMode}
          onchange={(e) => onChange({ echoCancellation: e.currentTarget.checked })}
        />
        <span>{t("settings.echoCancellation")}</span>
        <span class="hint">{t("settings.echoCancellationHint")}</span>
      </label>
      <label class="check">
        <input
          type="checkbox"
          checked={settings.autoGainControl && !settings.musicMode}
          disabled={settings.musicMode}
          onchange={(e) => onChange({ autoGainControl: e.currentTarget.checked })}
        />
        <span>{t("settings.autoGainControl")}</span>
        <span class="hint">{t("settings.autoGainControlHint")}</span>
      </label>
      <label class="check">
        <input type="checkbox" checked={settings.musicMode} onchange={(e) => onChange({ musicMode: e.currentTarget.checked })} />
        <span>{t("settings.musicMode")}</span>
        <span class="hint">{t("settings.musicModeHint")}</span>
      </label>
      <label class="check">
        <input
          type="checkbox"
          checked={settings.closeMicWhenMuted}
          onchange={(e) => onChange({ closeMicWhenMuted: e.currentTarget.checked })}
        />
        <span>{t("settings.closeMic")}</span>
        <span class="hint">{t("settings.closeMicHint")}</span>
      </label>
      <label class="check">
        <input type="checkbox" checked={settings.streamPreviews} onchange={(e) => onChange({ streamPreviews: e.currentTarget.checked })} />
        <span>{t("settings.streamPreviews")}</span>
        <span class="hint">{t("settings.streamPreviewsHint")}</span>
      </label>

      <div class="row">
        <span class="label">{t("settings.streamQualityScreen")}</span>
        <div class="quality-selects">
          <select
            aria-label={t("media.screenResolution")}
            value={settings.streamQuality.screen.height}
            onchange={(e) =>
              onChange({
                streamQuality: {
                  ...settings.streamQuality,
                  screen: { ...settings.streamQuality.screen, height: Number(e.currentTarget.value) },
                },
              })}
          >
            {#each [480, 720, 1080].filter((h) => !qualityLimits?.maxSide || h <= qualityLimits.maxSide) as h (h)}
              <option value={h}>{h}p</option>
            {/each}
          </select>
          <select
            aria-label={t("media.screenFrameRate")}
            value={settings.streamQuality.screen.fps}
            onchange={(e) =>
              onChange({
                streamQuality: {
                  ...settings.streamQuality,
                  screen: { ...settings.streamQuality.screen, fps: Number(e.currentTarget.value) },
                },
              })}
          >
            {#each [15, 30, 60].filter((f) => !qualityLimits?.maxFps || f <= qualityLimits.maxFps) as f (f)}
              <option value={f}>{t("media.framesPerSecond", { fps: f })}</option>
            {/each}
          </select>
        </div>
        <p class="hint">{t("settings.streamQualityScreenHint")}</p>
      </div>

      <div class="row">
        <span class="label">{t("settings.streamQualityCamera")}</span>
        <div class="quality-selects">
          <select
            aria-label={t("media.cameraResolution")}
            value={settings.streamQuality.camera.height}
            onchange={(e) =>
              onChange({
                streamQuality: {
                  ...settings.streamQuality,
                  camera: { ...settings.streamQuality.camera, height: Number(e.currentTarget.value) },
                },
              })}
          >
            {#each [480, 720, 1080].filter((h) => !qualityLimits?.maxSide || h <= qualityLimits.maxSide) as h (h)}
              <option value={h}>{h}p</option>
            {/each}
          </select>
          <select
            aria-label={t("media.cameraFrameRate")}
            value={settings.streamQuality.camera.fps}
            onchange={(e) =>
              onChange({
                streamQuality: {
                  ...settings.streamQuality,
                  camera: { ...settings.streamQuality.camera, fps: Number(e.currentTarget.value) },
                },
              })}
          >
            {#each [15, 30, 60].filter((f) => !qualityLimits?.maxFps || f <= qualityLimits.maxFps) as f (f)}
              <option value={f}>{t("media.framesPerSecond", { fps: f })}</option>
            {/each}
          </select>
        </div>
        <p class="hint">{t("settings.streamQualityCameraHint")}</p>
      </div>
    {:else if id === "notifications"}
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
        <span>{t("settings.desktopNotifications")}</span>
        {#if notificationNote}<span class="hint warn" role="status">{notificationNote}</span>{/if}
      </label>
      {#if push && pushSupport === "ok"}
        <label class="check">
          <input type="checkbox" checked={pushOn} disabled={pushSave.busy} onchange={(e) => setPush(e.currentTarget)} />
          <span>{t("settings.pushNotifications")}</span>
          <span class="hint">{t("settings.pushHint")}</span>
          <SaveStatus save={pushSave.state} />
        </label>
      {:else if push && pushSupport === "install"}
        <p class="hint">{t("settings.pushInstallHint")}</p>
      {/if}
      <label class="check">
        <input type="checkbox" checked={settings.sounds} onchange={(e) => onChange({ sounds: e.currentTarget.checked })} />
        <span>{t("settings.sounds")}</span>
        <span class="hint">{t("settings.soundsHint")}</span>
      </label>
      {#if settings.sounds}
        <label>
          {t("settings.soundVolume")}
          <input
            type="range"
            min="0"
            max="1"
            step="0.05"
            value={settings.soundVolume}
            oninput={(e) => onChange({ soundVolume: Number(e.currentTarget.value) })}
            aria-valuetext={t("settings.percent", { value: Math.round(settings.soundVolume * 100) })}
          />
        </label>
      {/if}
    {:else if id === "appearance"}
      <label>
        {t("settings.language")}
        <select value={i18n.language} onchange={(e) => void i18n.setLanguage(e.currentTarget.value as Language)}>
          <option value="auto">{t("settings.languageAuto")}</option>
          {#each LOCALES as locale (locale)}
            <option value={locale} lang={locale}>{LOCALE_NAMES[locale]}</option>
          {/each}
        </select>
      </label>

      <label>
        {t("settings.theme")}
        <select value={settings.theme} onchange={(e) => onChange({ theme: e.currentTarget.value as ThemeSetting })}>
          <option value="dark">{t("settings.themeDark")}</option>
          <option value="light">{t("settings.themeLight")}</option>
          <option value="system">{t("settings.themeSystem")}</option>
        </select>
      </label>

      <label>
        {t("settings.timeFormat")}
        <select value={settings.timeFormat} onchange={(e) => onChange({ timeFormat: e.currentTarget.value as TimeFormat })}>
          <option value="auto">{t("settings.timeAuto")}</option>
          <option value="24">{t("settings.time24")}</option>
          <option value="12">{t("settings.time12")}</option>
        </select>
      </label>
    {:else if id === "translation"}
      <p class="hint intro">{t("settings.translationIntro")}</p>
      <label class="check">
        <input
          type="checkbox"
          checked={settings.betterTranslation && gpu}
          disabled={!gpu}
          onchange={(e) => onChange({ betterTranslation: e.currentTarget.checked })}
        />
        <span>{t("settings.betterTranslation")}</span>
        <span class="hint">{gpu ? t("settings.betterTranslationHint") : t("settings.betterTranslationNoGpu")}</span>
      </label>
    {/if}
  {/snippet}
</CategoryDialog>
  {#if qrShown && account?.qr}
    <QrShowDialog api={account.qr.api} origin={account.qr.origin} device={account.qr.device} onClose={() => (qrShown = false)} />
  {/if}

<style>
  .me {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border);
    text-align: left;
  }
  .me-name {
    font-weight: 700;
    color: var(--text);
  }
  .me-edit {
    font-size: 13px;
    color: var(--accent);
  }
  .menu-footer {
    margin-top: auto;
    padding: 16px 10px 0;
  }
  .sep {
    height: 1px;
    margin: 10px;
    background: var(--border-subtle);
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
  /* A section's opening sentence: spaced as a field. */
  .hint.intro {
    margin-bottom: 16px;
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
  .my-emoji h4 {
    margin: 0;
    font-size: 14px;
  }
  .header-with-value {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .value-readout {
    font-size: 13px;
    font-weight: 600;
    color: var(--accent-strong);
    font-variant-numeric: tabular-nums;
  }
  .presets {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }
  .preset-btn {
    flex: 1;
    padding: 6px 10px;
    font-size: 12px;
    background: var(--bg-rail);
    border: 1px solid transparent;
    border-radius: 4px;
    color: var(--text);
    cursor: pointer;
    text-align: center;
  }
  .preset-btn:hover {
    background: var(--bg-hover);
  }
  .preset-btn.selected {
    border-color: var(--accent);
    color: var(--accent-strong);
    font-weight: 600;
  }
  .quality-selects {
    display: flex;
    gap: 8px;
  }
  .quality-selects select {
    flex: 1;
  }
</style>
