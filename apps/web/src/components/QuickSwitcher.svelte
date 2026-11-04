<script lang="ts" module>
  // kind: how it shows (and what "open" means, run).
  // unread: for Alt+Shift+Up/Down. locked: a private channel. category: a
  // text channel's (shown after the name).
  export type Place = { key: string; name: string; kind: "text" | "voice" | "dm" | "thread"; unread?: boolean; locked?: boolean; category?: string; run: () => void };
</script>

<script lang="ts">
  import { IS_MAC, matchPlaces, MOD, navAction } from "../lib/switcher";
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import { t } from "../lib/i18n.svelte";

  // Ctrl/Cmd+K: find a channel or a person by name and go there.
  let { places, onClose }: { places: Place[]; onClose: () => void } = $props();

  const MAX = 10;
  let dialog: HTMLDialogElement | undefined = $state();
  let query = $state("");
  let chosen = $state(0);
  const found = $derived(matchPlaces(places, query).slice(0, MAX));
  const active = $derived(Math.max(0, Math.min(chosen, found.length - 1))); // the list can change
  $effect(() => {
    const p = found[active];
    if (p) document.getElementById(`switcher-${p.key}`)?.scrollIntoView({ block: "nearest" });
  });
  $effect(() => dialog?.showModal());

  const open = (p: Place | undefined) => {
    if (!p) return;
    dialog?.close();
    p.run();
  };
</script>

<dialog bind:this={dialog} onclose={onClose} aria-label={t("sidebar.quickSwitcher")}>
  <input
    type="text"
    role="combobox"
    aria-expanded="true"
    aria-controls="switcher-list"
    aria-activedescendant={found[active] && `switcher-${found[active]!.key}`}
    aria-label={t("sidebar.whereTo")}
    placeholder={t("sidebar.wherePlaceholder")}
    bind:value={query}
    aria-autocomplete="list"
    oninput={() => (chosen = 0)}
    onkeydown={(e) => {
      if (e.isComposing || e.keyCode === 229) return;
      if (navAction(e, IS_MAC) === "switcher") {
        e.preventDefault();
        dialog?.close(); // Ctrl/Cmd+K again: closed, focus back
      } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const n = found.length;
        if (n) chosen = (active + (e.key === "ArrowDown" ? 1 : n - 1)) % n;
      } else if (e.key === "Enter") {
        e.preventDefault();
        open(found[active]);
      }
    }}
  />
  <ul id="switcher-list" role="listbox" aria-label={t("sidebar.places")}>
    {#each found as p, i (p.key)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <li id="switcher-{p.key}" role="option" aria-selected={i === active} onclick={() => open(p)} onmousemove={() => (chosen = i)}>
        {#if p.kind === "dm"}<Avatar name={p.name} registered size={20} />{:else}<Icon name={p.kind === "text" ? "hash" : p.kind === "thread" ? "thread" : "speaker"} size={20} />{/if}
        <span class="ellipsis">{p.name}</span>
        {#if p.locked}<span class="lock"><Icon name="lock" size={14} /><span class="sr-only">{t("sidebar.privateMark")}</span></span>{/if}
        {#if p.category}<span class="hint">{p.category}</span>{/if}
        {#if p.kind === "voice"}<span class="hint">{t("sidebar.joinHint")}</span>{/if}
      </li>
    {/each}
  </ul>
  {#if !found.length}<p class="none">{t("sidebar.nothingFound")}</p>{/if}
  <p class="keys">{t("sidebar.switcherKeys", { mod: MOD })}</p>
</dialog>

<style>
  dialog {
    width: min(540px, 92vw);
    margin-top: 15vh;
    padding: 16px;
    border: none;
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  input {
    width: 100%;
    padding: 12px;
    border: none;
    border-radius: 4px;
    background: var(--bg-rail);
    color: inherit;
    font: inherit;
    font-size: 18px;
  }
  ul {
    margin: 12px 0 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    gap: 10px;
    align-items: center;
    padding: 8px;
    border-radius: 4px;
    color: var(--text-muted);
    cursor: pointer;
  }
  li[aria-selected="true"] {
    background: var(--bg-active);
    color: var(--text);
  }
  .lock {
    display: flex;
  }
  .hint {
    margin-left: auto;
    font-size: 12px;
  }
  .none {
    margin: 12px 8px 0;
    color: var(--text-muted);
  }
  .keys {
    margin: 12px 0 0;
    color: var(--text-muted);
    font-size: 12px;
  }
</style>
