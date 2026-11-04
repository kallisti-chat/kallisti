<script lang="ts">
  import type { Save } from "../lib/saveFeedback";
  import { t } from "../lib/i18n.svelte";

  // Next to a control: "Saved" for a moment once the server confirms, or
  // its error (see lib/saveFeedback). The status region stays, so screen
  // readers announce the change. done "": the control shows the result
  // itself.
  let { save, done }: { save: Save; done?: string } = $props();
  const shown = $derived(done ?? t("common.saved"));
</script>

<span role="status">{#if save.phase === "saved" && shown}<span class="saved">{shown}</span>{/if}</span>
{#if save.phase === "error"}<span class="error" role="alert">{save.message}</span>{/if}

<style>
  span {
    font-size: 14px;
  }
  .saved {
    color: var(--online);
    animation: fade 2s ease-in forwards;
  }
  @keyframes fade {
    60% {
      opacity: 1;
    }
    to {
      opacity: 0;
    }
  }
  .error {
    color: var(--danger);
  }
</style>
