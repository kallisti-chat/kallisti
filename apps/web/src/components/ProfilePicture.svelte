<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import { encodeAvatar } from "../lib/avatarEncode";
  import Avatar from "./Avatar.svelte";

  // Choose or remove our avatar picture.
  // Only registered users may set one (the server keeps it); guests would
  // lose it when they leave.
  let {
    name,
    hasPicture,
    registered,
    onSet,
  }: { name: string; hasPicture: boolean; registered: boolean; onSet: (texture: Uint8Array) => void } = $props();

  let input: HTMLInputElement | undefined = $state();
  let error = $state<string | null>(null);

  const choose = async (file: File | undefined) => {
    if (!file) return;
    error = null;
    try {
      onSet(await encodeAvatar(file));
    } catch {
      error = t("settings.pictureError");
    } finally {
      if (input) input.value = "";
    }
  };
</script>

<div class="profile">
  <Avatar {name} size={64} />
  <div class="actions">
    <span class="label">{t("settings.profilePicture")}</span>
    <div class="buttons">
      {#if registered}
        <button type="button" class="secondary" onclick={() => input?.click()}>
          {hasPicture ? t("settings.changePicture") : t("settings.choosePicture")}
        </button>
      {/if}
      {#if hasPicture}
        <button type="button" class="secondary" onclick={() => onSet(new Uint8Array())}>{t("common.remove")}</button>
      {/if}
    </div>
    {#if error}<p class="hint warn" role="status">{error}</p>{/if}
    {#if !registered}<p class="hint">{t("settings.pictureRegisterHint")}</p>{/if}
  </div>
  <input
    bind:this={input}
    type="file"
    accept="image/png,image/jpeg,image/gif,image/webp"
    hidden
    onchange={(e) => choose(e.currentTarget.files?.[0])}
  />
</div>

<style>
  .profile {
    --avatar-cutout: var(--bg-main);
    display: flex;
    align-items: center;
    gap: 16px;
    padding-bottom: 16px;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .actions {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .label {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .buttons {
    display: flex;
    gap: 6px;
  }
  .secondary {
    padding: 8px 14px;
    border-radius: 6px;
    background: var(--bg-input);
    color: var(--text);
  }
  .hint {
    margin: 0;
    font-size: 12px;
    color: var(--text-muted);
  }
  .hint.warn {
    color: var(--danger);
  }
</style>
