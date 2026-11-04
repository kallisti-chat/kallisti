<script lang="ts" generics="Id extends string">
  import { t } from "../lib/i18n.svelte";
  import { tick, untrack, type Snippet } from "svelte";
  import { groupsOf, startCategory, type Category } from "../lib/dialogCategories";

  // A dialog in categories (see lib/dialogCategories): on desktop the menu
  // beside the open one; on phones the whole screen, a list where each
  // category opens as its own page ("‹ <title>"; Escape goes back to the
  // list first). The parts are the caller's: content (the open category),
  // menuTop and menuEnd (around the menu's items; class "item" for more,
  // "item danger" in red).
  let {
    title,
    categories,
    groupTitles,
    start,
    phone = false,
    hideOnPhone = [],
    section = $bindable(null),
    dialog = $bindable(),
    onClose,
    menuTop,
    menuEnd,
    content,
  }: {
    title: string;
    categories: readonly Category<Id>[];
    groupTitles: Record<string, string>;
    // The category to open at.
    start?: Id;
    phone?: boolean;
    // Not in the phone's list (menuTop leads there).
    hideOnPhone?: readonly Id[];
    // The open category (null: the list, on phones).
    section?: Id | null;
    dialog?: HTMLDialogElement;
    onClose: () => void;
    menuTop?: Snippet<[open: (id: Id) => void]>;
    menuEnd?: Snippet;
    content: Snippet<[Id]>;
  } = $props();

  untrack(() => {
    if (section === null) section = startCategory(start, categories, phone);
  });
  const groups = $derived(groupsOf(categories, groupTitles));
  const current = $derived(categories.find((c) => c.id === section) ?? null);
  const open = (id: Id) => (section = id);
  $effect(() => dialog?.showModal());
  // A wider screen shows a category beside the menu (also when the open
  // one went, as one only for a user that is reconnecting).
  $effect(() => {
    if (!phone && !categories.some((c) => c.id === section)) section = untrack(() => categories[0]?.id ?? null);
  });
  // Phones: focus to the page's title as it opens, back to its list item
  // after (the button that had it is gone).
  let lastPage: Id | null = null;
  $effect(() => {
    if (!phone) return;
    const opened = section;
    void tick().then(() => {
      const to = opened ? dialog?.querySelector<HTMLElement>(".layout > .content > .title") : dialog?.querySelector<HTMLElement>(`[data-category="${lastPage}"]`);
      to?.focus();
      lastPage = opened ?? lastPage;
    });
  });
  const id = $props.id();
</script>

<dialog
  bind:this={dialog}
  class:phone
  onclose={onClose}
  oncancel={(e) => {
    if (phone && section !== null) (e.preventDefault(), (section = null));
  }}
  aria-labelledby="{id}-title"
>
  <header>
    {#if phone && current}
      <button class="back" onclick={() => (section = null)}>‹ {title}</button>
      <h2 id="{id}-title" class="sr-only">{t("settings.categoryHeading", { title, category: current.title })}</h2>
    {:else}
      <h2 id="{id}-title">{title}</h2>
    {/if}
    <button class="close" onclick={() => dialog?.close()} aria-label={t("common.close")}>×</button>
  </header>
  <div class="layout">
    {#if !phone || !current}
      <nav class="menu" aria-label={t("settings.categoriesLabel", { title })}>
        {@render menuTop?.(open)}
        {#each groups as g (g.id)}
          <p class="group">{g.title}</p>
          {#each g.categories as c (c.id)}
            {#if !(phone && hideOnPhone.includes(c.id))}
              <button class="item" data-category={c.id} aria-current={c.id === section ? "page" : undefined} onclick={() => open(c.id)}>
                {c.title}{#if phone}<span aria-hidden="true">›</span>{/if}
              </button>
            {/if}
          {/each}
        {/each}
        {@render menuEnd?.()}
      </nav>
    {/if}
    {#if current}
      <div class="content">
        <h3 class="title" tabindex="-1">{current.title}</h3>
        {@render content(current.id)}
      </div>
    {/if}
  </div>
</dialog>

<style>
  /* Desktop: the menu beside the open category; phones: the whole
     screen, the list or one category. */
  dialog {
    width: min(860px, 94vw);
    height: min(660px, 90vh);
    padding: 0;
    border: none;
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  dialog[open] {
    display: flex;
    flex-direction: column;
  }
  /* The whole screen: not app.css's safe-area inset of modals (the
     padding keeps clear of the notch instead). */
  dialog.phone {
    inset: 0;
    width: 100vw;
    max-width: none;
    height: 100dvh;
    max-height: none;
    margin: 0;
    border-radius: 0;
    padding-top: env(safe-area-inset-top);
    padding-bottom: env(safe-area-inset-bottom);
  }
  /* Not the other dialogs' sticky header (app.css): this dialog has no
     padding, and its parts scroll on their own. */
  header {
    position: static;
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: 0;
    padding: 14px 20px;
    border-bottom: 1px solid var(--border);
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
  .back {
    padding: 0;
    font-size: 16px;
    color: var(--accent);
  }
  .layout {
    display: flex;
    flex: 1;
    min-height: 0;
  }
  .menu {
    display: flex;
    flex-direction: column;
    width: 220px;
    flex: none;
    padding: 4px 10px 16px;
    overflow-y: auto;
    background: var(--bg-sidebar);
  }
  .phone .menu {
    width: 100%;
    padding: 0;
    background: none;
  }
  .group {
    margin: 14px 10px 6px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .phone .group {
    margin: 0;
    padding: 18px 20px 6px;
    border-bottom: 1px solid var(--border);
  }
  /* Also the caller's extra items (menuTop, menuEnd). */
  .menu :global(.item) {
    display: flex;
    justify-content: space-between;
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 15px;
    text-align: left;
    color: var(--text-muted);
  }
  .menu :global(.item:hover),
  .menu :global(.item[aria-current="page"]) {
    background: var(--bg-hover);
    color: var(--text);
  }
  .menu :global(.item[aria-current="page"]) {
    background: var(--bg-active);
  }
  .phone .menu :global(.item) {
    padding: 15px 20px;
    border-bottom: 1px solid var(--border);
    border-radius: 0;
    color: var(--text);
  }
  .phone .menu :global(.item[aria-current="page"]) {
    background: none;
  }
  .menu :global(.item.danger),
  .phone .menu :global(.item.danger) {
    color: var(--danger);
  }
  .content {
    flex: 1;
    min-width: 0;
    padding: 20px 28px 28px;
    overflow-y: auto;
  }
  .phone .content {
    padding: 16px 20px 28px;
  }
  .title {
    margin: 0 0 18px;
    font-size: 19px;
  }
  .title:focus {
    outline: none;
  }
</style>
