<script lang="ts">
  import { t } from "../lib/i18n.svelte";
  import { onDestroy } from "svelte";
  import type { QrStart, QrState } from "../lib/accounts";
  import { pollState, qrLink } from "../lib/qrLogin";
  import QrCode from "./QrCode.svelte";
  import QrDialog from "./QrDialog.svelte";

  // Shows a login code (see lib/qrLogin) and follows it. Logged in (an
  // offer): after a scan, asks "Log in on <device>?". Logged out (a
  // request): waits for the phone's yes, then this device is logged in
  // (onLoggedIn).
  let {
    api,
    origin,
    device,
    onLoggedIn,
    onClose,
  }: {
    api: {
      qrStart: (device: string) => Promise<QrStart>;
      qrWait: (secret: string) => Promise<QrState>;
      qrAnswer: (secret: string, yes: boolean) => Promise<void>;
    };
    origin: string; // the server's address, in the code
    device: string; // this device, as the other one sees it
    onLoggedIn?: (name: string) => void;
    onClose: () => void;
  } = $props();

  let code = $state<QrStart | null>(null);
  let phase = $state<"loading" | "showing" | "ask" | "confirm" | "approving" | "done" | "refused" | "expired" | "error">("loading");
  let other = $state(""); // the other device
  let error = $state("");
  let stop = () => {};
  let gone = false;
  onDestroy(() => ((gone = true), stop()));

  // Its state, until it ends.
  const follow = (c: QrStart) =>
    pollState(
      () => api.qrWait(c.owner),
      (st) => {
        if (st.device) other = st.device;
        if (st.state === "scanned" && phase === "showing") phase = c.kind === "offer" ? "ask" : "confirm";
        else if (st.state === "approved" && c.kind === "offer") phase = "approving";
        else if (st.state === "done") {
          phase = "done"; // an offer: the phone has its session; a request: this device
          if (c.kind === "request") onLoggedIn?.(st.name ?? "");
        } else if (st.state === "refused" || st.state === "expired") phase = st.state;
        return phase !== "done" && phase !== "refused" && phase !== "expired";
      },
      undefined,
      c.expires + 5000,
    );
  const failed = (err: unknown) => {
    error = err instanceof Error ? err.message : String(err);
    phase = "error";
  };
  const start = async () => {
    stop();
    phase = "loading";
    other = "";
    try {
      const made = await api.qrStart(device);
      if (gone) return;
      code = made;
      phase = "showing";
      stop = follow(code);
    } catch (err) {
      failed(err);
    }
  };
  void start();
  const answer = async (yes: boolean) => {
    if (!code) return;
    try {
      await api.qrAnswer(code.owner, yes);
      phase = yes ? "approving" : "refused";
    } catch (err) {
      failed(err);
    }
  };
</script>

<QrDialog title={code?.kind === "request" ? t("settings.qrLoginTitle") : t("settings.otherDevice")} {onClose}>
  {#if phase === "loading"}
    <p class="note">{t("settings.qrMaking")}</p>
  {:else if phase === "showing" && code}
    <div class="code"><QrCode text={qrLink(origin, code.code)} label={t("settings.qrCodeLabel")} /></div>
    <p class="note">
      {code.kind === "offer" ? t("settings.qrOfferHint") : t("settings.qrRequestHint")}
      {t("settings.qrValid")}
    </p>
  {:else if phase === "ask"}
    <p><strong>{t("settings.qrAsk", { device: other })}</strong></p>
    <p class="note">{t("settings.qrAskHint")}</p>
    <div class="buttons">
      <button type="button" class="secondary" onclick={() => answer(false)}>{t("common.no")}</button>
      <button type="button" class="primary" onclick={() => answer(true)}>{t("settings.qrLogIn")}</button>
    </div>
  {:else if phase === "confirm"}
    <p>{t("settings.qrConfirmOn", { device: other })}</p>
  {:else if phase === "approving"}
    <p>{t("settings.qrLoggingIn", { device: other })}</p>
  {:else if phase === "done"}
    <p>{code?.kind === "offer" ? t("settings.qrLoggedInOn", { device: other }) : t("settings.qrLoggedIn")}</p>
  {:else if phase === "refused" || phase === "expired"}
    <p>{phase === "refused" ? t("settings.qrRefused") : t("settings.qrExpired")}</p>
    <div class="buttons"><button type="button" class="primary" onclick={start}>{t("settings.qrNewCode")}</button></div>
  {:else}
    <p role="alert">{error}</p>
    <div class="buttons"><button type="button" class="primary" onclick={start}>{t("common.retry")}</button></div>
  {/if}
</QrDialog>
