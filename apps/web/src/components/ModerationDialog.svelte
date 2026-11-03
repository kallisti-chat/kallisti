<script lang="ts">
  import { banAddress, type Ban, type Registered } from "@kallisti/protocol";
  import { untrack } from "svelte";
  import CategoryDialog from "./CategoryDialog.svelte";
  import { SERVER_GROUP_TITLES, serverSections, type ServerSectionId } from "../lib/serverSettingsSections";
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
  import SaveStatus from "./SaveStatus.svelte";

  // Server settings for moderators and admins: the ban list, the
  // registered users, the custom emoji, the finished channel recordings,
  // and for admins the guests settings, the message rules and the
  // connections (the server checks every change again).
  let {
    bans,
    registered,
    rights,
    onLoad,
    onSetBans,
    onRename,
    onUnregister,
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
    onClose,
    phone = false,
  }: {
    bans: readonly Ban[] | null;
    registered: readonly Registered[] | null;
    rights: { bans: boolean; editBans: boolean; users: boolean; emoji: boolean };
    onLoad: (what: "bans" | "users") => void;
    onSetBans: (bans: Ban[]) => Promise<void>;
    onRename: (userId: number, name: string) => Promise<void>;
    onUnregister: (userId: number) => Promise<void>;
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
  let renaming = $state<{ id: number; name: string } | null>(null);
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
  const when = (t: number) => new Date(t).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

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
    seconds === 0 ? "for ever" : seconds >= 86400 ? `${Math.round(seconds / 86400)} days` : `${Math.round(seconds / 3600)} hours`;
</script>

<CategoryDialog
  title="Server settings"
  categories={sections}
  groupTitles={SERVER_GROUP_TITLES}
  start={onServerName ? undefined : "bans"}
  {phone}
  bind:section
  {onClose}
>
  {#snippet content(id: ServerSectionId)}
    {#if id === "bans"}
      {#if bans === null}
        <p class="note">Loading…</p>
      {:else if bans.length === 0}
        <p class="note">Nobody is banned.</p>
      {:else}
        <ul>
          {#each bans as ban, i (i)}
            <li>
              <div class="what">
                <strong>{ban.name || banAddress(ban)}</strong>
                <span class="detail">
                  {banAddress(ban)} · {duration(ban.duration)}{ban.start ? ` · since ${ban.start.slice(0, 10)}` : ""}
                  {#if ban.reason} · {ban.reason}{/if}
                </span>
              </div>
              {#if rights.editBans}
                <button type="button" disabled={saves.bans.busy} onclick={() => saves.bans.run(() => onSetBans(bans.filter((_, k) => k !== i)))}>
                  Remove
                </button>
              {/if}
            </li>
          {/each}
        </ul>
        <SaveStatus save={saves.bans.state} done="Removed" />
        {#if !rights.editBans}<p class="note">Only admins change the ban list.</p>{/if}
      {/if}
    {:else if id === "server"}
      <form class="rules" onsubmit={saveName}>
        <label>
          Name
          <input bind:value={nameDraft} required maxlength="100" />
        </label>
        <button type="submit" disabled={saves.name.busy || !nameDraft.trim()}>{busyLabel(saves.name.state, "Save")}</button>
        <SaveStatus save={saves.name.state} />
      </form>
      <p class="note">In the top left, as the tab's title and on the login page. Mumble clients see it as the name of the root channel.</p>
      {#if onUploadLogo}
        <div class="logo-row">
          {#if logo}<img class="logo" src={logo} alt="The server's logo" />{/if}
          <input type="file" accept="image/png,image/jpeg,image/gif,image/webp" aria-label="Logo image" disabled={saves.logo.busy} onchange={(e) => uploadLogo(e.currentTarget)} />
          {#if logo && onRemoveLogo}
            <button type="button" disabled={saves.logo.busy} onclick={() => saves.logo.run(onRemoveLogo)}>Remove</button>
          {/if}
          <SaveStatus save={saves.logo.state} />
        </div>
        <p class="note">
          The logo: a square picture (PNG, JPEG, GIF or WebP, at most 2 MB; the middle of others). In the top left, as the tab's icon,
          on the login page and as the installed app's icon (installed apps may show a new one only later).
        </p>
      {/if}
      {#if onLoadHomeServers}
        <h4>Accounts from other servers</h4>
        {#if homeServers === null}
          <p class="note">Loading…</p>
        {:else}
          <form class="rules" onsubmit={saveHomes}>
            <label>
              Who may log in with an account from another server
              <select bind:value={homesMode}>
                <option value="all">Accounts from all servers</option>
                <option value="block">All but the servers below</option>
                <option value="allow">Only the servers below</option>
              </select>
            </label>
            {#if homesMode !== "all"}
              <label for="home-hosts">Servers, one address per line</label>
              <textarea id="home-hosts" rows="4" bind:value={homesHosts} placeholder="chat.example.org"></textarea>
            {/if}
            <div class="row">
              <button type="submit" disabled={saves.homes.busy}>{busyLabel(saves.homes.state, "Save")}</button>
              <SaveStatus save={saves.homes.state} />
            </div>
          </form>
          <p class="note">
            Another Kallisti server or a fediverse server (Mastodon and others) where the account lives. Accounts that log in only
            with a server you exclude are logged out here when you save.
          </p>
        {/if}
      {/if}
    {:else if id === "registration" && inviteOnly !== null && onInviteOnly && onLoadRegistrationInvites && onCreateRegistrationInvite && onDeleteRegistrationInvite}
      <RegistrationSettings on={inviteOnly} invites={registrationInvites} onSet={onInviteOnly} onLoad={onLoadRegistrationInvites} onCreate={onCreateRegistrationInvite} onDelete={onDeleteRegistrationInvite} />
    {:else if id === "guests"}
      <label class="check">
        <input type="checkbox" checked={guestsReadOnly === true} disabled={saves.guests.busy} onchange={(e) => setGuests(e.currentTarget)} />
        Guests may only read
      </label>
      <SaveStatus save={saves.guests.state} />
      <p class="note">
        Guests (without an account, or with a guest account) can still read and talk, but they cannot write, react, upload or send
        direct messages. Use it during a spam wave.
      </p>
      <label class="check">
        Browser check for new accounts and guests
        <select value={captcha} disabled={saves.captcha.busy} onchange={(e) => setCaptcha(e.currentTarget)}>
          <option value="off">Off</option>
          <option value="normal">Normal</option>
          <option value="hard">Hard</option>
        </select>
      </label>
      <SaveStatus save={saves.captcha.state} />
      <p class="note">
        Before a web browser creates an account or joins as a guest, it does some work by itself: about 1 to 2 seconds (Normal) or 4
        times as long (Hard). This slows down scripts that make many accounts. No third party is involved. Mumble clients and logins
        do no check.
      </p>
    {:else if id === "threads"}
      <label class="check">
        Archive threads after
        <select value={threadArchive} disabled={saves.archive.busy} onchange={(e) => setArchive(e.currentTarget)}>
          {#each ARCHIVE_TIMES as t (t.seconds)}<option value={t.seconds}>{t.label}</option>{/each}
        </select>
      </label>
      <SaveStatus save={saves.archive.state} />
      <p class="note">
        A thread without new messages for this time is archived: it leaves the channel list, but stays readable and searchable in the
        channel's thread list. A new message opens it again.
      </p>
    {:else if id === "rules"}
      {#if messageRules === null}
        <p class="note">Loading…</p>
      {:else}
        <form class="rules" onsubmit={saveRules}>
          <label>
            Limit new accounts for
            <input class="minutes" bind:value={rulesMinutes} inputmode="numeric" aria-label="Minutes" oninput={() => (rulesError = null)} />
            minutes (0: off)
          </label>
          <p class="note">
            Accounts younger than this, and guests, cannot post links, upload files or mention @everyone or @here, and send one
            message every 5 seconds. Accounts from before this setting existed count as old.
          </p>
          <label for="blocked-words">Blocked words and phrases, one per line</label>
          <textarea id="blocked-words" rows="5" bind:value={rulesWords}></textarea>
          <p class="note">
            Whole words in any case. Accents, lookalike letters (Cyrillic а), spaced letters (s p a m) and a letter three times or
            more (spaaam) do not get past it. No patterns.
          </p>
          <label for="blocked-domains">Blocked link domains, one per line</label>
          <textarea id="blocked-domains" rows="4" bind:value={rulesDomains}></textarea>
          <p class="note">With their subdomains: example.com also blocks www.example.com.</p>
          <label for="replacements">Word replacements, one phrase per line</label>
          <textarea id="replacements" rows="4" bind:value={rulesReplacements} placeholder="nice ass => beautiful behind | lovely posterior"
          ></textarea>
          {#each replacements.errors as error (error)}<p class="error" role="alert">{error}</p>{/each}
          <p class="note">
            A message with the phrase is posted with one of its replacements, picked at random, for everyone (staff too). Matched as
            blocked words, but not in code, links or mentions. Replacements are plain text: letters, digits, emoji, spaces and ' .
            , ! ? -.
          </p>
          {#if rulesError}<p class="error" role="alert">{rulesError}</p>{/if}
          <div class="row">
            <button type="submit" disabled={saves.rules.busy}>{busyLabel(saves.rules.state, "Save")}</button>
            <SaveStatus save={saves.rules.state} />
          </div>
        </form>
        <p class="note">
          Staff are exempt from the limits and blocks. The author of a refused message sees why (Mumble clients as "permission denied"); the server log notes
          blocked words and domains.
        </p>
      {/if}
    {:else if id === "connections"}
      {#if sorted === null}
        <p class="note">Loading…</p>
      {:else if sorted.length === 0}
        <p class="note">Nobody is connected.</p>
      {:else}
        <div class="table">
          <table>
            <thead>
              <tr>
                <th>User</th>
                <th>Voice</th>
                <th title="How their voice reaches the server lately: Opus band and bitrate">Sound</th>
                <th title="What a web user's browser says about their microphone (filters on hover)">Microphone</th>
                <th>Ping</th>
                <th
                  title="Late or lost voice in the last 30 to 40 s. ↑ from the user, as the server counted it; ↓ to the user, as their client reported it (Mumble clients count UDP only). –: too little voice."
                >
                  Late/lost ↑ · ↓
                </th>
                <th title="In the same time: voice frames the server dropped for this user (their queue was full) · text messages they lost">
                  Dropped
                </th>
                <th title="Since they connected: stream pictures · stream sound the server dropped for this viewer (their connection was full)">
                  Stream lost
                </th>
                <th>Online</th>
                <th>Client</th>
              </tr>
            </thead>
            <tbody>
              {#each sorted as c (c.session)}
                {@const level = connectionLevel(c)}
                {@const sound = formatSound(c)}
                {@const mic = formatMic(c)}
                <tr>
                  <td><span class="level {level ?? 'unknown'}" title={level ?? "not measured yet"}></span>{c.name}</td>
                  <td>{c.transport === "websocket" ? "WebSocket" : c.transport.toUpperCase()}</td>
                  <td class:muffled={sound.muffled}
                    title={sound.muffled ? "Narrow band sounds muffled: often a Bluetooth headset in its phone mode, or a low-quality microphone" : undefined}
                    >{sound.muffled ? "⚠ " : ""}{sound.text}</td
                  >
                  <td class="detail" class:muffled={mic.phoneMode}
                    title={[mic.phoneMode ? "16 kHz or less: often a Bluetooth headset in its phone mode" : "", mic.filters].filter(Boolean).join(" · ") || undefined}
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
        <p class="note">Worst first; updates every few seconds. No addresses: admins see them in Mumble's user information.</p>
      {/if}
    {:else if id === "recordings"}
      {#if recordings === null}
        <p class="note">Loading…</p>
      {:else if recordings.length === 0}
        <p class="note">No recordings. Record a voice channel from its menu (right-click).</p>
      {:else}
        <ul>
          {#each recordings as r (r.id)}
            {@const d = describeRecording(r)}
            <li>
              <div class="what">
                <strong>{r.channelName} · {when(r.started)}</strong>
                <span class="detail">
                  {d.length} · {d.size} · {d.speakers} · by {r.by}{r.reason === "stopped" ? "" : ` · ended: ${r.reason}`} · until {when(r.expires)}
                </span>
              </div>
              <button type="button" onclick={() => download(r.id)}>Download</button>
            </li>
          {/each}
        </ul>
        {#if downloadError}<p class="note error" role="alert">{downloadError}</p>{/if}
        <p class="note">A zip with one Ogg Opus file per speaker. All files start when the recording started, so they line up in an audio editor.</p>
      {/if}
    {:else if id === "stickers"}
      <StickerList list={stickers} onAdd={onAddSticker} onEdit={onEditSticker} onDelete={onDeleteSticker} />
    {:else if id === "emoji"}
      <EmojiList
        list={emoji}
        formError={(name, file) => emojiFormError(name, file, emoji)}
        onAdd={onAddEmoji}
        onDelete={onDeleteEmoji}
        empty="This server has no custom emoji yet."
      />
    {:else if registered === null}
      <p class="note">Loading…</p>
    {:else}
      <ul>
        {#each registered as user (user.userId)}
          <li>
            {#if renaming?.id === user.userId}
              <form
                class="rename"
                onsubmit={(e) => {
                  e.preventDefault();
                  const name = renaming?.name.trim();
                  if (name) saves.users.run(() => onRename(user.userId, name).then(() => (renaming = null)));
                  else renaming = null;
                }}
              >
                <input bind:value={renaming.name} aria-label="New name for {user.name}" />
                <button type="submit" disabled={saves.users.busy}>{busyLabel(saves.users.state, "Save")}</button>
                <button type="button" onclick={() => (renaming = null)}>Cancel</button>
              </form>
            {:else}
              <div class="what"><strong>{user.name}</strong><span class="detail">#{user.userId}</span></div>
              {#if user.userId !== 0}
                {#if confirming === user.userId}
                  <button
                    type="button"
                    class="danger"
                    disabled={saves.users.busy}
                    onclick={() => (saves.users.run(() => onUnregister(user.userId)), (confirming = null))}
                  >
                    Unregister
                  </button>
                  <button type="button" onclick={() => (confirming = null)}>Cancel</button>
                {:else}
                  <button type="button" onclick={() => (renaming = { id: user.userId, name: user.name })}>Rename</button>
                  <button type="button" onclick={() => (confirming = user.userId)}>Unregister…</button>
                {/if}
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
  .rename {
    display: flex;
    flex: 1;
    gap: 6px;
  }
  .rename input {
    flex: 1;
    padding: 6px 8px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: var(--text);
    font: inherit;
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
