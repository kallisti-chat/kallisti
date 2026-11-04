<script lang="ts">
  import { onDestroy, untrack } from "svelte";
  import { apiStatus, type QrScan, type QrState } from "../lib/accounts";
  import { pollState } from "../lib/qrLogin";
  import QrDialog from "./QrDialog.svelte";
  import { t } from "../lib/i18n.svelte";

  // A scanned login code (see lib/qrLogin): the link opened, or the app's
  // scanner. Logged in, a logged-out device's code: "Log in on <device>?"
  // (the phishing warning: whoever shows it gets the account). Logged out,
  // a logged-in device's code: wait for its yes, then this device is
  // logged in (onLoggedIn). A logged-in device does not change accounts by
  // a code (the server refuses: 409).
  let {
    api,
    token,
    device,
    me,
    onLoggedIn,
    onClose,
  }: {
    api: {
      qrScan: (token: string, device: string) => Promise<QrScan>;
      qrWait: (secret: string) => Promise<QrState>;
      qrAnswer: (secret: string, yes: boolean) => Promise<void>;
    };
    token: string;
    device: string; // this device, as the other one sees it
    me: string | null; // the account logged in here (null: none, or not known yet)
    onLoggedIn: (name: string) => void;
    onClose: () => void;
  } = $props();

  let phase = $state<"scanning" | "ask" | "waiting" | "done" | "refused" | "expired" | "error">("scanning");
  let other = $state("");
  let error = $state("");
  let stop = () => {};
  let gone = false;
  onDestroy(() => ((gone = true), stop()));

  const failed = (err: unknown, scanning: boolean) => {
    const status = apiStatus(err);
    error =
      status === 404
        ? t("login.qrInvalid")
        : status === 409
          ? t("login.qrNeedsLoggedOut")
          : status === 401 && scanning
            ? t("login.qrLogInFirst")
            : err instanceof Error
              ? err.message
              : String(err);
    phase = "error";
  };
  // Once, as it opens (a code is scanned once).
  void untrack(() => api.qrScan(token, device)).then(
    (s) => {
      if (gone) return; // closed while it was read: no login after all
      other = s.device;
      if (s.kind === "request") return void (phase = "ask");
      phase = "waiting";
      stop = pollState(
        () => api.qrWait(s.claim),
        (st) => {
          if (st.state === "done") (phase = "done"), onLoggedIn(st.name ?? "");
          else if (st.state === "refused" || st.state === "expired") phase = st.state;
          return phase === "waiting";
        },
      );
      // (A 409 while waiting: logged in meanwhile, see the server; the
      // wait is asked again and ends at the deadline.)
    },
    (err) => failed(err, true),
  );
  const answer = async (yes: boolean) => {
    try {
      await api.qrAnswer(token, yes);
      phase = yes ? "done" : "refused";
    } catch (err) {
      failed(err, false);
    }
  };
</script>

<QrDialog title={t("login.qrLogin")} {onClose}>
  {#if phase === "scanning"}
    <p class="note">{t("login.qrReading")}</p>
  {:else if phase === "ask"}
    <p><strong>{me ? t("login.qrAskAs", { device: other, name: me }) : t("login.qrAskWithAccount", { device: other })}</strong></p>
    <p class="warn">{t("login.qrWarning")}</p>
    <div class="buttons">
      <button type="button" class="secondary" onclick={() => answer(false)}>{t("common.no")}</button>
      <button type="button" class="primary" onclick={() => answer(true)}>{t("login.logIn")}</button>
    </div>
  {:else if phase === "waiting"}
    <p>{t("login.qrConfirmOn", { device: other })}</p>
  {:else if phase === "done"}
    <p>{me ? t("login.qrDeviceLoggedIn", { device: other }) : t("login.qrLoggedIn")}</p>
  {:else if phase === "refused"}
    <p>{t("login.qrRefused")}</p>
  {:else if phase === "expired"}
    <p>{t("login.qrExpired")}</p>
  {:else}
    <p role="alert">{error}</p>
  {/if}
</QrDialog>
