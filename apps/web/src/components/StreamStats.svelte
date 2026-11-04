<script lang="ts">
  import { untrack } from "svelte";
  import { perSecond, type StreamTotals } from "../lib/stream/stats";
  import { t } from "../lib/i18n.svelte";

  // A watched stream's numbers over the tile (see lib/stream/stats), each
  // second: its playout buffer, what comes per second, what went wrong since it
  // was opened.
  let { read }: { read: () => { totals: StreamTotals; delay: number } | null } = $props();

  let now = $state(untrack(() => read()));
  let rates = $state<ReturnType<typeof perSecond> | null>(null);
  $effect(() => {
    // The real time between readings: hidden tabs run timers less often.
    let before = read();
    let then = performance.now();
    const timer = setInterval(() => {
      const next = read();
      const at = performance.now();
      if (before && next) rates = perSecond(before.totals, next.totals, (at - then) / 1000);
      before = now = next;
      then = at;
    }, 1000);
    return () => clearInterval(timer);
  });
  const round = (n: number) => Math.round(n * 10) / 10;
</script>

{#if now}
  <dl class="stats" aria-label={t("media.statsLabel")}>
    <dt>{t("media.statsBuffer")}</dt>
    <dd>{t("media.statsMilliseconds", { ms: Math.round(now.delay * 1000) })}</dd>
    <dt>{t("media.statsPictures")}</dt>
    <dd>{rates ? t("media.statsPictureRate", { pictures: round(rates.pictures), drawn: round(rates.drawn) }) : "…"}</dd>
    <dt>{t("media.statsSound")}</dt>
    <dd>{rates ? t("media.statsPerSecond", { rate: round(rates.sound) }) : "…"}</dd>
    <dt>{t("media.statsBitrate")}</dt>
    <dd>{rates ? t("media.statsKbps", { kbps: Math.round(rates.kbps) }) : "…"}</dd>
    <dt>{t("media.statsLate")}</dt>
    <dd>{t("media.statsLateValue", { pictures: now.totals.latePictures, sound: now.totals.lateSound })}</dd>
    <dt>{t("media.statsSkipped")}</dt>
    <dd>{t("media.statsSkippedValue", { count: now.totals.skipped })}</dd>
    <dt>{t("media.statsLostSound")}</dt>
    <dd>{t("media.statsLostValue", { count: now.totals.lostSound })}</dd>
  </dl>
{/if}

<style>
  .stats {
    position: absolute;
    top: 8px;
    left: 8px;
    z-index: 2;
    display: grid;
    grid-template-columns: auto auto;
    gap: 1px 10px;
    margin: 0;
    padding: 6px 8px;
    border-radius: 6px;
    background: rgb(0 0 0 / 0.7);
    color: #eee;
    font: 11px/1.4 ui-monospace, monospace;
    pointer-events: none;
  }
  dt {
    color: #aaa;
  }
  dd {
    margin: 0;
  }
</style>
