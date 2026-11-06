<script lang="ts">
  import { banAddress, type Ban, type Registered } from "@kallisti/protocol";
  import { untrack } from "svelte";
  import CategoryDialog from "./CategoryDialog.svelte";
  import { serverGroupTitles, serverSections, type ServerSectionId } from "../lib/serverSettingsSections";
  import { around } from "../lib/i18n";
  import { i18n, t } from "../lib/i18n.svelte";
  import type { Key } from "../i18n/en";
  import type { CaptchaLevel } from "../lib/captcha";
  import { ARCHIVE_TIMES } from "../lib/threads";
  import { emojiFormError, type CustomEmoji } from "../lib/customEmoji";
  import EmojiList from "./EmojiList.svelte";
  import StickerList from "./StickerList.svelte";
  import type { Sticker, StickerFields } from "../lib/stickers";
  import {
    connectionLevel,
    formatOnline,
    formatPercent,
    formatPing,
    formatMic,
    formatSound,
    POLL_MS,
    worstFirst,
    type Connection,
  } from "../lib/connections";
  import {
    formatList,
    formatReplacements,
    MAX_NEW_MINUTES,
    minutesError,
    parseReplacements,
    rulesFrom,
    type MessageRules,
  } from "../lib/messageRules";
  import { homeServersFrom, type HomeServers } from "../lib/homeServers";
  import { describeRecording, type FinishedRecording } from "../lib/recording";
  import { busyLabel } from "../lib/saveFeedback";
  import { createSave } from "../lib/saveFeedback.svelte";
  import RegistrationSettings from "./RegistrationSettings.svelte";
  import type { Invite } from "../lib/invites";
  import type { QualityLimits } from "../lib/textChannels";
  import SaveStatus from "./SaveStatus.svelte";

  // Server settings for moderators and admins: the ban list, the custom
  // emoji, the finished channel recordings, and for admins the registered
  // users, the guests settings, the message rules and the
  // connections (the server checks every change again).
  let {
    bans,
    registered,
    rights,
    onLoad,
    onSetBans,
    onUnregister,
    mayDelete = (id) => id !== 0,
    emoji,
    onAddEmoji,
    onDeleteEmoji,
    stickers,
    onAddSticker,
    onEditSticker,
    onDeleteSticker,
    serverName = "",
    onServerName,
    logo = null,
    onUploadLogo,
    onRemoveLogo,
    inviteOnly = null,
    registrationInvites = null,
    onInviteOnly,
    onLoadRegistrationInvites,
    onCreateRegistrationInvite,
    onDeleteRegistrationInvite,
    guestsReadOnly = null,
    onGuestsReadOnly,
    captcha = "off",
    onCaptcha,
    threadArchive,
    onThreadArchive,
    connections = null,
    onPollConnections,
    messageRules = null,
    onLoadMessageRules,
    onSaveMessageRules,
    homeServers = null,
    onLoadHomeServers,
    onSaveHomeServers,
    recordings = null,
    recording = [],
    onLoadRecordings,
    onDownloadRecording,
    qualityLimits = null,
    onSaveQualityLimits,
    onClose,
    phone = false,
  }: {
    bans: readonly Ban[] | null;
    registered: readonly Registered[] | null;
    rights: { bans: boolean; editBans: boolean; users: boolean; emoji: boolean };
    onLoad: (what: "bans" | "users") => void;
    onSetBans: (bans: Ban[]) => Promise<void>;
    onUnregister: (userId: number) => Promise<void>;
    // Whom the list offers to delete (default: all but SuperUser).
    mayDelete?: (userId: number) => boolean;
    emoji: readonly CustomEmoji[];
    onAddEmoji: (name: string, file: File) => Promise<void>;
    onDeleteEmoji: (name: string) => Promise<void>;
    // The server's stickers (with the emoji rights); urls absolute.
    stickers: readonly Sticker[];
    onAddSticker: (fields: StickerFields, file: File) => Promise<void>;
    onEditSticker: (id: number, fields: StickerFields) => Promise<void>;
    onDeleteSticker: (id: number) => Promise<void>;
    // Admins: the server's name; without onServerName, no Overview.
    serverName?: string;
    onServerName?: (name: string) => Promise<void>;
    // The logo (null: none); without onUploadLogo, no logo controls.
    logo?: string | null;
    onUploadLogo?: (file: File) => Promise<void>;
    onRemoveLogo?: () => Promise<void>;
    // Admins: whether guests may only read (null: not ours to change).
    inviteOnly?: boolean | null;
    registrationInvites?: readonly Invite[] | null;
    onInviteOnly?: (on: boolean) => Promise<void>;
    onLoadRegistrationInvites?: () => Promise<void>;
    onCreateRegistrationInvite?: (seconds: number, uses: number) => Promise<string>;
    onDeleteRegistrationInvite?: (id: number) => Promise<void>;
    guestsReadOnly?: boolean | null;
    onGuestsReadOnly?: (on: boolean) => Promise<void>;
    // Admins (with the guests setting): the browser check for new accounts.
    captcha?: CaptchaLevel;
    onCaptcha?: (level: CaptchaLevel) => Promise<void>;
    // Staff: how long threads stay active without messages (seconds);
    // without onThreadArchive, no category.
    threadArchive?: number;
    onThreadArchive?: (seconds: number) => Promise<void>;
    // Admins: every connection (null: not loaded yet), asked for again
    // while the tab is open; without onPollConnections, no category.
    connections?: readonly Connection[] | null;
    onPollConnections?: () => void;
    // Admins: the message rules (null: not loaded yet); without
    // onLoadMessageRules, no category.
    messageRules?: MessageRules | null;
    onLoadMessageRules?: () => void;
    onSaveMessageRules?: (rules: MessageRules) => Promise<void>;
    // Admins: which other servers' accounts log in (null: not loaded yet);
    // without onLoadHomeServers, not shown.
    homeServers?: HomeServers | null;
    onLoadHomeServers?: () => void;
    onSaveHomeServers?: (homes: HomeServers) => Promise<void>;
    // Staff: the finished recordings (null: not loaded yet), asked for
    // again when a recording starts or stops (recording: the recorded
    // channels); without onLoadRecordings, no category.
    recordings?: readonly FinishedRecording[] | null;
    recording?: readonly number[] | null;
    onLoadRecordings?: () => void;
    onDownloadRecording?: (id: string) => Promise<void>;
    qualityLimits?: QualityLimits | null;
    onSaveQualityLimits?: (q: QualityLimits) => Promise<void>;
    onClose: () => void;
    // Phones: a list of categories, each its own page (see CategoryDialog).
    phone?: boolean;
  } = $props();

  // The categories this user may see (see CategoryDialog), and the one open.
  const sections = $derived(
    serverSections({
      server: !!onServerName,
      bans: rights.bans,
      users: rights.users,
      emoji: rights.emoji,
      guests: guestsReadOnly !== null,
      registration: inviteOnly !== null && !!onInviteOnly,
      threads: !!onThreadArchive,
      rules: !!onLoadMessageRules,
      connections: !!onPollConnections,
      recordings: !!onLoadRecordings,
    }),
  );
  let section = $state<ServerSectionId | null>(null);
  let confirming = $state<number | null>(null);
  // Each change waits for the server (see lib/saveFeedback); a checkbox or
  // select goes back to the server's value when refused.
  const saves = {
    bans: createSave(),
    users: createSave(),
    name: createSave(),
    logo: createSave(),
    guests: createSave(),
    captcha: createSave(),
    archive: createSave(),
    rules: createSave(),
    homes: createSave(),
    quality: createSave(),
  };

  let voiceBitrateDraft = $state(128);
  let musicBitrateDraft = $state(320);
  let streamBandwidthDraft = $state(1048576);
  let streamSideDraft = $state(4096);
  let streamFpsDraft = $state(60);

  $effect(() => {
    if (qualityLimits) {
      voiceBitrateDraft = Math.round(qualityLimits.maxBitrateVoice / 1000);
      musicBitrateDraft = Math.round(qualityLimits.maxBitrateMusic / 1000);
      streamBandwidthDraft = qualityLimits.bytesPerSecond;
      streamSideDraft = qualityLimits.maxSide;
      streamFpsDraft = qualityLimits.maxFps;
    }
  });

  const saveQuality = (e: SubmitEvent) => {
    e.preventDefault();
    if (onSaveQualityLimits) {
      saves.quality.run(() =>
        onSaveQualityLimits({
          maxBitrateVoice: voiceBitrateDraft * 1000,
          maxBitrateMusic: musicBitrateDraft * 1000,
          bytesPerSecond: streamBandwidthDraft,
          maxSide: streamSideDraft,
          maxFps: streamFpsDraft,
        })
      );
    }
  };
  const setGuests = (el: HTMLInputElement) =>
    onGuestsReadOnly && saves.guests.run(() => onGuestsReadOnly(el.checked), () => (el.checked = guestsReadOnly === true));
  const setCaptcha = (el: HTMLSelectElement) =>
    onCaptcha && saves.captcha.run(() => onCaptcha(el.value as CaptchaLevel), () => (el.value = captcha));
  const setArchive = (el: HTMLSelectElement) =>
    onThreadArchive && saves.archive.run(() => onThreadArchive(Number(el.value)), () => (el.value = String(threadArchive)));
  let nameDraft = $state(untrack(() => serverName));
  const saveName = (e: SubmitEvent) => {
    e.preventDefault();
    if (onServerName) saves.name.run(() => onServerName(nameDraft));
  };
  // Cleared at once: the same file again is a new upload.
  const uploadLogo = (el: HTMLInputElement) => {
    const file = el.files?.[0];
    el.value = "";
    if (file && onUploadLogo) saves.logo.run(() => onUploadLogo(file));
  };
  $effect(() => {
    if (section === "bans" || section === "users") onLoad(section);
  });
  // Only the category and the right restart polling: the callback is a new
  // function after every state change, the answer's too.
  const mayPoll = $derived(!!onPollConnections);
  $effect(() => {
    if (section !== "connections" || !mayPoll) return;
    const poll = () => untrack(() => onPollConnections)?.();
    poll();
    const timer = setInterval(poll, POLL_MS);
    return () => clearInterval(timer);
  });
  const sorted = $derived(connections && worstFirst(connections));

  const mayRecordings = $derived(!!onLoadRecordings);
  $effect(() => {
    void recording; // a recording started or stopped: the list changed
    if (section === "recordings" && mayRecordings) untrack(() => onLoadRecordings)?.();
  });
  let downloadError = $state<string | null>(null);
  const download = (id: string) => {
    downloadError = null;
    onDownloadRecording?.(id).catch((err) => (downloadError = err instanceof Error ? err.message : String(err)));
  };
  const when = (at: number) => new Date(at).toLocaleString(i18n.locale, { dateStyle: "medium", timeStyle: "short" });

  // The message rules: loaded when their category opens, the form filled with
  // what the server has (again after a save: it tidies the lists).
  const mayRules = $derived(!!onLoadMessageRules);
  $effect(() => {
    if (section === "rules" && mayRules) untrack(() => onLoadMessageRules)?.();
  });
  let rulesMinutes = $state("");
  let rulesWords = $state("");
  let rulesDomains = $state("");
  let rulesReplacements = $state("");
  let rulesError = $state<string | null>(null);
  const replacements = $derived(parseReplacements(rulesReplacements)); // errors shown as you type
  $effect(() => {
    if (!messageRules) return;
    rulesMinutes = String(messageRules.newMinutes);
    rulesWords = formatList(messageRules.words);
    rulesDomains = formatList(messageRules.domains);
    rulesReplacements = formatReplacements(messageRules.replacements);
  });
  // Which other servers' accounts log in: loaded with the Overview.
  $effect(() => {
    if (section === "server") untrack(() => onLoadHomeServers)?.();
  });
  let homesMode = $state<HomeServers["mode"]>("all");
  let homesHosts = $state("");
  $effect(() => {
    if (!homeServers) return;
    homesMode = homeServers.mode;
    homesHosts = homeServers.hosts.join("\n");
  });
  const saveHomes = (e: SubmitEvent) => {
    e.preventDefault();
    if (onSaveHomeServers) saves.homes.run(() => onSaveHomeServers(homeServersFrom(homesMode, homesHosts)));
  };
  const saveRules = (e: SubmitEvent) => {
    e.preventDefault();
    rulesError = minutesError(rulesMinutes);
    if (rulesError || replacements.errors.length > 0 || !onSaveMessageRules) return;
    const rules = rulesFrom(rulesMinutes, rulesWords, rulesDomains, replacements.replacements);
    saves.rules.run(() => onSaveMessageRules(rules));
  };


  const duration = (seconds: number) =>
    seconds === 0
      ? t("admin.banForever")
      : seconds >= 86400
        ? t("admin.banDays", { count: Math.round(seconds / 86400) })
        : t("admin.banHours", { count: Math.round(seconds / 3600) });
  const LEVELS: Record<string, Key> = { good: "admin.levelGood", fair: "admin.levelFair", poor: "admin.levelPoor" };
  const limitNew = $derived(around(t("admin.rulesLimitNew"), "minutes"));
</script>

<CategoryDialog
  title={t("admin.serverSettingsTitle")}
  categories={sections}
  groupTitles={serverGroupTitles()}
  start={onServerName ? undefined : "bans"}
  {phone}
  bind:section
  {onClose}
>
  {#snippet content(id: ServerSectionId)}
    {#if id === "bans"}
      {#if bans === null}
        <p class="note">{t("common.loading")}</p>
      {:else if bans.length === 0}
        <p class="note">{t("admin.nobodyBanned")}</p>
      {:else}
        <ul>
          {#each bans as ban, i (i)}
            <li>
              <div class="what">
                <strong>{ban.name || banAddress(ban)}</strong>
                <span class="detail">
                  {banAddress(ban)} · {duration(ban.duration)}{ban.start ? ` · ${t("admin.banSince", { date: ban.start.slice(0, 10) })}` : ""}
                  {#if ban.reason} · {ban.reason}{/if}
                </span>
              </div>
              {#if rights.editBans}
                <button type="button" disabled={saves.bans.busy} onclick={() => saves.bans.run(() => onSetBans(bans.filter((_, k) => k !== i)))}>
                  {t("common.remove")}
                </button>
              {/if}
            </li>
          {/each}
        </ul>
        <SaveStatus save={saves.bans.state} done={t("admin.banRemoved")} />
        {#if !rights.editBans}<p class="note">{t("admin.onlyAdminsEditBans")}</p>{/if}
      {/if}
    {:else if id === "server"}
      <form class="rules" onsubmit={saveName}>
        <label>
          {t("admin.serverName")}
          <input bind:value={nameDraft} required maxlength="100" />
        </label>
        <button type="submit" disabled={saves.name.busy || !nameDraft.trim()}>{busyLabel(saves.name.state, t("common.save"), t("common.saving"))}</button>
        <SaveStatus save={saves.name.state} />
      </form>
      <p class="note">{t("admin.serverNameNote")}</p>
      {#if onUploadLogo}
        <div class="logo-row">
          {#if logo}<img class="logo" src={logo} alt={t("admin.logoAlt")} />{/if}
          <input type="file" accept="image/png,image/jpeg,image/gif,image/webp" aria-label={t("admin.logoInput")} disabled={saves.logo.busy} onchange={(e) => uploadLogo(e.currentTarget)} />
          {#if logo && onRemoveLogo}
            <button type="button" disabled={saves.logo.busy} onclick={() => saves.logo.run(onRemoveLogo)}>{t("common.remove")}</button>
          {/if}
          <SaveStatus save={saves.logo.state} />
        </div>
        <p class="note">{t("admin.logoNote")}</p>
      {/if}
      {#if onLoadHomeServers}
        <h4>{t("admin.homesHeading")}</h4>
        {#if homeServers === null}
          <p class="note">{t("common.loading")}</p>
        {:else}
          <form class="rules" onsubmit={saveHomes}>
            <label>
              {t("admin.homesMode")}
              <select bind:value={homesMode}>
                <option value="all">{t("admin.homesAll")}</option>
                <option value="block">{t("admin.homesBlock")}</option>
                <option value="allow">{t("admin.homesAllow")}</option>
              </select>
            </label>
            {#if homesMode !== "all"}
              <label for="home-hosts">{t("admin.homesHosts")}</label>
              <textarea id="home-hosts" rows="4" bind:value={homesHosts} placeholder="chat.example.org"></textarea>
            {/if}
            <div class="row">
              <button type="submit" disabled={saves.homes.busy}>{busyLabel(saves.homes.state, t("common.save"), t("common.saving"))}</button>
              <SaveStatus save={saves.homes.state} />
            </div>
          </form>
          <p class="note">{t("admin.homesNote")}</p>
        {/if}
      {/if}
    {:else if id === "quality"}
      <form class="rules" onsubmit={saveQuality}>
        <label>
          {t("admin.qualityMaxVoiceBitrate")} ({voiceBitrateDraft} kbps)
          <input type="range" min="16" max="558" step="1" bind:value={voiceBitrateDraft} />
        </label>
        <p class="note">{t("admin.qualityMaxVoiceBitrateNote")}</p>

        <label>
          {t("admin.qualityMaxMusicBitrate")} ({musicBitrateDraft} kbps)
          <input type="range" min="32" max="558" step="8" bind:value={musicBitrateDraft} />
        </label>
        <p class="note">{t("admin.qualityMaxMusicBitrateNote")}</p>

        <label>
          {t("admin.qualityMaxStreamResolution")}
          <select bind:value={streamSideDraft}>
            <option value={720}>720p (1280×720)</option>
            <option value={1080}>1080p (1920×1080)</option>
            <option value={1440}>1440p (2560×1440)</option>
            <option value={4096}>4K (4096)</option>
          </select>
        </label>
        <p class="note">{t("admin.qualityMaxStreamResolutionNote")}</p>

        <label>
          {t("admin.qualityMaxStreamFramerate")}
          <select bind:value={streamFpsDraft}>
            <option value={15}>15 fps</option>
            <option value={30}>30 fps</option>
            <option value={60}>60 fps</option>
          </select>
        </label>
        <p class="note">{t("admin.qualityMaxStreamFramerateNote")}</p>

        <label>
          {t("admin.qualityMaxStreamBandwidth")} ({((streamBandwidthDraft / 1024 / 1024) * 8).toFixed(1)} Mbit/s)
          <input type="range" min={100_000} max={10_000_000} step={100_000} bind:value={streamBandwidthDraft} />
        </label>
        <p class="note">{t("admin.qualityMaxStreamBandwidthNote")}</p>

        <div class="row">
          <button type="submit" disabled={saves.quality.busy}>{busyLabel(saves.quality.state, t("common.save"), t("common.saving"))}</button>
          <SaveStatus save={saves.quality.state} />
        </div>
      </form>
    {:else if id === "registration" && inviteOnly !== null && onInviteOnly && onLoadRegistrationInvites && onCreateRegistrationInvite && onDeleteRegistrationInvite}
      <RegistrationSettings on={inviteOnly} invites={registrationInvites} onSet={onInviteOnly} onLoad={onLoadRegistrationInvites} onCreate={onCreateRegistrationInvite} onDelete={onDeleteRegistrationInvite} />
    {:else if id === "guests"}
      <label class="check">
        <input type="checkbox" checked={guestsReadOnly === true} disabled={saves.guests.busy} onchange={(e) => setGuests(e.currentTarget)} />
        {t("admin.guestsReadOnly")}
      </label>
      <SaveStatus save={saves.guests.state} />
      <p class="note">{t("admin.guestsReadOnlyNote")}</p>
      <label class="check">
        {t("admin.captchaLabel")}
        <select value={captcha} disabled={saves.captcha.busy} onchange={(e) => setCaptcha(e.currentTarget)}>
          <option value="off">{t("common.off")}</option>
          <option value="normal">{t("admin.captchaNormal")}</option>
          <option value="hard">{t("admin.captchaHard")}</option>
        </select>
      </label>
      <SaveStatus save={saves.captcha.state} />
      <p class="note">{t("admin.captchaNote")}</p>
    {:else if id === "threads"}
      <label class="check">
        {t("admin.archiveAfter")}
        <select value={threadArchive} disabled={saves.archive.busy} onchange={(e) => setArchive(e.currentTarget)}>
          {#each ARCHIVE_TIMES as a (a.seconds)}<option value={a.seconds}>{t(a.label)}</option>{/each}
        </select>
      </label>
      <SaveStatus save={saves.archive.state} />
      <p class="note">{t("admin.archiveNote")}</p>
    {:else if id === "rules"}
      {#if messageRules === null}
        <p class="note">{t("common.loading")}</p>
      {:else}
        <form class="rules" onsubmit={saveRules}>
          <label>
            {limitNew[0]}<input class="minutes" bind:value={rulesMinutes} inputmode="numeric" aria-label={t("admin.rulesMinutes")} oninput={() => (rulesError = null)} />{limitNew[1]}
          </label>
          <p class="note">{t("admin.rulesNewNote")}</p>
          <label for="blocked-words">{t("admin.rulesWords")}</label>
          <textarea id="blocked-words" rows="5" bind:value={rulesWords}></textarea>
          <p class="note">{t("admin.rulesWordsNote")}</p>
          <label for="blocked-domains">{t("admin.rulesDomains")}</label>
          <textarea id="blocked-domains" rows="4" bind:value={rulesDomains}></textarea>
          <p class="note">{t("admin.rulesDomainsNote")}</p>
          <label for="replacements">{t("admin.rulesReplacements")}</label>
          <textarea id="replacements" rows="4" bind:value={rulesReplacements} placeholder={t("admin.rulesReplacementsPlaceholder")}
          ></textarea>
          {#each replacements.errors as error (error)}<p class="error" role="alert">{error}</p>{/each}
          <p class="note">{t("admin.rulesReplacementsNote")}</p>
          {#if rulesError}<p class="error" role="alert">{rulesError}</p>{/if}
          <div class="row">
            <button type="submit" disabled={saves.rules.busy}>{busyLabel(saves.rules.state, t("common.save"), t("common.saving"))}</button>
            <SaveStatus save={saves.rules.state} />
          </div>
        </form>
        <p class="note">{t("admin.rulesStaffNote")}</p>
      {/if}
    {:else if id === "connections"}
      {#if sorted === null}
        <p class="note">{t("common.loading")}</p>
      {:else if sorted.length === 0}
        <p class="note">{t("admin.nobodyConnected")}</p>
      {:else}
        <div class="table">
          <table>
            <thead>
              <tr>
                <th>{t("admin.connUser")}</th>
                <th>{t("admin.connVoice")}</th>
                <th title={t("admin.connSoundTitle")}>{t("admin.connSound")}</th>
                <th title={t("admin.connMicTitle")}>{t("admin.connMic")}</th>
                <th>{t("admin.connPing")}</th>
                <th title={t("admin.connLateTitle")}>{t("admin.connLate")}</th>
                <th title={t("admin.connDroppedTitle")}>{t("admin.connDropped")}</th>
                <th title={t("admin.connStreamLostTitle")}>{t("admin.connStreamLost")}</th>
                <th>{t("admin.connOnline")}</th>
                <th>{t("admin.connClient")}</th>
              </tr>
            </thead>
            <tbody>
              {#each sorted as c (c.session)}
                {@const level = connectionLevel(c)}
                {@const sound = formatSound(c)}
                {@const mic = formatMic(c)}
                <tr>
                  <td><span class="level {level ?? 'unknown'}" title={level ? t(LEVELS[level]!) : t("admin.levelUnknown")}></span>{c.name}</td>
                  <td>{c.transport === "websocket" ? "WebSocket" : c.transport.toUpperCase()}</td>
                  <td class:muffled={sound.muffled}
                    title={sound.muffled ? t("admin.muffledTitle") : undefined}
                    >{sound.muffled ? "⚠ " : ""}{sound.text}</td
                  >
                  <td class="detail" class:muffled={mic.phoneMode}
                    title={[mic.phoneMode ? t("admin.phoneModeTitle") : "", mic.filters].filter(Boolean).join(" · ") || undefined}
                    >{mic.phoneMode ? "⚠ " : ""}{mic.text}</td
                  >
                  <td>{formatPing(c.ping)}</td>
                  <td>{formatPercent(c.fromClient)} · {formatPercent(c.fromServer)}</td>
                  <td>{c.dropped} · {c.overflows}</td>
                  <td>{c.streamPictures ?? "–"} · {c.streamSound ?? "–"}</td>
                  <td>{formatOnline(c.online)}</td>
                  <td class="detail">{[c.client, c.os].filter(Boolean).join(" · ")}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        <p class="note">{t("admin.connNote")}</p>
      {/if}
    {:else if id === "recordings"}
      {#if recordings === null}
        <p class="note">{t("common.loading")}</p>
      {:else if recordings.length === 0}
        <p class="note">{t("admin.noRecordings")}</p>
      {:else}
        <ul>
          {#each recordings as r (r.id)}
            {@const d = describeRecording(r)}
            <li>
              <div class="what">
                <strong>{r.channelName} · {when(r.started)}</strong>
                <span class="detail">
                  {t(r.reason === "stopped" ? "admin.recordingDetail" : "admin.recordingDetailEnded", { ...d, by: r.by, reason: r.reason, expires: when(r.expires) })}
                </span>
              </div>
              <button type="button" onclick={() => download(r.id)}>{t("admin.download")}</button>
            </li>
          {/each}
        </ul>
        {#if downloadError}<p class="note error" role="alert">{downloadError}</p>{/if}
        <p class="note">{t("admin.recordingNote")}</p>
      {/if}
    {:else if id === "stickers"}
      <StickerList list={stickers} onAdd={onAddSticker} onEdit={onEditSticker} onDelete={onDeleteSticker} />
    {:else if id === "emoji"}
      <EmojiList
        list={emoji}
        formError={(name, file) => emojiFormError(name, file, emoji)}
        onAdd={onAddEmoji}
        onDelete={onDeleteEmoji}
        empty={t("admin.noCustomEmoji")}
      />
    {:else if registered === null}
      <p class="note">{t("common.loading")}</p>
    {:else}
      <ul>
        {#each registered as user (user.userId)}
          <li>
            <div class="what">
              <strong>{user.name}</strong>
              <span class="detail">#{user.userId}{confirming === user.userId ? ` · ${t("admin.userDeleteNote")}` : ""}</span>
            </div>
            {#if mayDelete(user.userId)}
              {#if confirming === user.userId}
                <button
                  type="button"
                  class="danger"
                  disabled={saves.users.busy}
                  onclick={() => (saves.users.run(() => onUnregister(user.userId)), (confirming = null))}
                >
                  {t("admin.deleteUser")}
                </button>
                <button type="button" onclick={() => (confirming = null)}>{t("common.cancel")}</button>
              {:else}
                <button type="button" onclick={() => (confirming = user.userId)}>{t("admin.deleteUserAsk")}</button>
              {/if}
            {/if}
          </li>
        {/each}
      </ul>
      <SaveStatus save={saves.users.state} />
    {/if}
  {/snippet}
</CategoryDialog>

<style>
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .what {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .detail {
    font-size: 12px;
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }
  li button {
    padding: 6px 10px;
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
  }
  li .danger {
    background: var(--danger);
    color: white;
  }
  .note {
    color: var(--text-muted);
    font-size: 14px;
  }
  .logo-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-top: 16px;
  }
  .logo {
    width: 48px;
    height: 48px;
    border-radius: 12px;
    object-fit: cover;
  }
  .logo-row input {
    max-width: 100%;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .check select {
    padding: 4px 6px;
    border: none;
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
    font: inherit;
  }
  .error {
    color: var(--danger);
    font-size: 14px;
  }
  .table {
    overflow-x: auto;
  }
  .rules {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .rules .note {
    margin: 0 0 8px;
  }
  .rules textarea,
  .rules .minutes {
    padding: 6px 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
  }
  .rules textarea {
    resize: vertical;
  }
  .rules .minutes {
    width: 5em;
  }
  .rules .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .rules button {
    align-self: flex-start;
    padding: 6px 10px;
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
    white-space: nowrap;
  }
  th,
  td {
    padding: 6px 8px 6px 0;
    border-bottom: 1px solid var(--border-subtle);
    text-align: left;
  }
  th {
    color: var(--text-muted);
    font-weight: 600;
  }
  th[title] {
    cursor: help;
  }
  .level {
    display: inline-block;
    width: 8px;
    height: 8px;
    margin-right: 6px;
    border-radius: 50%;
    background: var(--text-faint);
  }
  .level.good {
    background: var(--online);
  }
  .level.fair {
    background: var(--idle);
  }
  .level.poor {
    background: var(--dnd);
  }
  .emoji-image {
    width: 32px;
    height: 32px;
    object-fit: contain;
  }
  td.muffled {
    color: var(--warning, #e0a030);
  }
</style>
