<script lang="ts" module>
  // The mute when deafening, per server: undeafening brings it back. For
  // all panels: one server's voice is toggled from its panel or another's.
  const mutedBeforeDeaf = new Map<string, boolean>();
</script>

<script lang="ts">
  import UserName from "./UserName.svelte";
  import { t } from "../lib/i18n.svelte";
  import type { User } from "@kallisti/protocol";
  import { toggleDeaf, toggleMute, type SelfState } from "../lib/voice";
  import { menuAnchor } from "../lib/menu";
  import { PRESENCE_LABEL, customOf, type Presence, type PresenceView } from "../lib/presence";
  import Avatar from "./Avatar.svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import Emoji from "./Emoji.svelte";
  import Icon from "./Icon.svelte";
  import StatusDialog from "./StatusDialog.svelte";
  import ConnectionInfo from "./ConnectionInfo.svelte";
  import type { ConnectionInfo as Info } from "../lib/connectionInfo.svelte";

  let {
    me,
    status = t("client.voiceConnected"),
    active = true,
    micError,
    noVoice = false,
    onSelfState,
    onSettings,
    presence = "online",
    own = { status: "online" },
    now = Date.now(),
    onStatus,
    info,
    voiceKey = "",
  }: {
    me: User;
    // "Voice connected", "Not in voice", ...
    status?: string;
    // The status in the accent colour (voice is on here).
    active?: boolean;
    micError: string | null;
    // The browser cannot do voice (micError says why).
    noVoice?: boolean;
    onSelfState: (s: SelfState) => void;
    onSettings: () => void;
    // The dot, our chosen status, and the time for custom status ends.
    presence?: Presence;
    own?: PresenceView;
    now?: number;
    // Sets our status (none: the server has no statuses).
    onStatus?: (v: PresenceView) => void;
    // The server whose voice the buttons act on (see mutedBeforeDeaf).
    voiceKey?: string;
    // The connection's quality, while in voice without a voice bar (plain
    // Mumble servers; see VoiceBar).
    info?: Info;
  } = $props();

  const custom = $derived(customOf(own, now));
  let menu = $state<{ x: number; y: number } | null>(null);
  let editing = $state(false);
  const choose = (v: PresenceView) => {
    onStatus?.(v);
    menu = null;
  };
</script>

<section class="panel" aria-label={t("client.yourStatus")}>
  <button
    class="me"
    disabled={!onStatus}
    aria-haspopup="menu"
    title={onStatus ? t("client.setYourStatus") : undefined}
    onclick={(e) => (menu = menuAnchor(e, e.currentTarget.getBoundingClientRect()))}
  >
    <Avatar name={me.name} size={32} status={presence} />
    <div class="who">
      <div class="name ellipsis"><UserName name={me.name} /></div>
      {#if micError}
        <div class="status error ellipsis" title={micError}>{noVoice ? t("client.noVoiceInBrowser") : t("client.noMicrophone")}</div>
      {:else if custom}
        <div class="status custom ellipsis" title={custom.text}><Emoji text={custom.emoji} /> {custom.text}</div>
      {:else}
        <div class="status ellipsis" class:idle={!active}>{status}</div>
      {/if}
    </div>
  </button>
  {#if info}<ConnectionInfo {info} />{/if}
  <button
    title={me.selfMute ? t("client.unmute") : t("client.mute")}
    aria-pressed={me.selfMute}
    onclick={() => onSelfState(toggleMute(me))}
  >
    <Icon name="mic" off={me.selfMute} size={20} />
  </button>
  <button
    title={me.selfDeaf ? t("client.undeafen") : t("client.deafen")}
    aria-pressed={me.selfDeaf}
    onclick={() => {
      if (!me.selfDeaf) mutedBeforeDeaf.set(voiceKey, me.selfMute);
      onSelfState(toggleDeaf(me, mutedBeforeDeaf.get(voiceKey) ?? false));
    }}
  >
    <Icon name="headphones" off={me.selfDeaf} size={20} />
  </button>
  <button title={t("common.settings")} onclick={onSettings}>
    <Icon name="gear" size={20} />
  </button>
</section>

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} label={t("client.yourStatus")} onClose={() => (menu = null)}>
    {#each ["online", "idle", "dnd"] as const as p (p)}
      <button class="item" aria-pressed={own.status === p} onclick={() => choose({ ...own, status: p })}>
        <span class="dot" style:background="var(--{p})"></span>
        {PRESENCE_LABEL[p]}
      </button>
    {/each}
    <div class="sep"></div>
    <button class="item" onclick={() => ((editing = true), (menu = null))}>
      {custom ? t("client.editCustomStatus") : t("client.setCustomStatus")}
    </button>
    {#if custom}
      <button class="item" onclick={() => choose({ status: own.status })}>{t("client.clearCustomStatus")}</button>
    {/if}
  </ContextMenu>
{/if}
{#if editing}
  <StatusDialog start={custom && { ...custom, until: own.until }} onSave={(c) => onStatus?.({ status: own.status, ...c })} onClose={() => (editing = false)} />
{/if}

<style>
  .panel {
    --avatar-cutout: var(--bg-input);
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 8px;
    padding: 8px;
    border-radius: 8px;
    background: var(--bg-input);
  }
  .me {
    display: flex;
    flex: 1;
    align-items: center;
    gap: 8px;
    min-width: 0;
    padding: 2px;
    margin: -2px;
    border-radius: 6px;
    color: var(--text);
    text-align: left;
  }
  .me:disabled {
    cursor: default;
    opacity: 1;
  }
  .me:not(:disabled):hover {
    background: var(--bg-active);
  }
  .who {
    flex: 1;
    min-width: 0;
  }
  .status.custom {
    color: var(--text-muted);
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    text-align: left;
    color: var(--text-muted);
  }
  .item:hover {
    background: var(--accent-strong);
    color: white;
  }
  .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
  }
  .item[aria-pressed="true"] {
    color: var(--text);
    font-weight: 600;
  }
  .sep {
    height: 1px;
    margin: 4px 6px;
    background: var(--border-subtle);
  }
  .name {
    font-weight: 600;
    font-size: 14px;
  }
  .status {
    color: var(--accent);
    font-size: 12px;
  }
  .status.error {
    color: var(--danger);
  }
  .status.idle {
    color: var(--text-muted);
  }

  .panel > button:not(.me) {
    display: inline-flex;
    padding: 6px;
    border-radius: 6px;
    color: var(--text-muted);
  }
  .panel > button:not(.me):hover {
    background: var(--bg-active);
    color: var(--text);
  }
</style>
