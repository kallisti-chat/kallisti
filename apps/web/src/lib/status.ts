import type { ServerState } from "@kallisti/protocol";
import { refusedForGood } from "./reconnect";
import { t } from "./i18n.svelte";

// Null while the connection is healthy, otherwise a message for the user.
// Close reasons meant for people (a guest refused on another site, the
// desktop app's own bridge): the others are technical, or another
// server's text.
const shown = (reason: string, localBridge: boolean) => refusedForGood(reason) || (localBridge && reason.startsWith("cannot reach "));

// closeReason: the reason for closing the socket, if one was given;
// localBridge: the desktop app's own bridge to a Mumble server closed it.
export const connectionError = (state: ServerState, closed: boolean, closeReason = "", localBridge = false): string | null => {
  if (state.status === "rejected") return state.error || t("client.serverRejected");
  if (state.status === "kicked") return state.error || t("client.kicked");
  if (!closed) return null;
  return shown(closeReason, localBridge) ? closeReason : t("client.connectionLost");
};

// The Kallisti server serves the web app and the bridge (at /ws) from one
// origin. Pages served over https must use wss, or the browser blocks it.
export const proxyUrl = ({ protocol, host }: { protocol: string; host: string }) =>
  `${protocol === "https:" ? "wss" : "ws"}://${host}/ws`;
