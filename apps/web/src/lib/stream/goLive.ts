import { Perm } from "@kallisti/protocol";
import type { Kind } from "./quality";
import { t } from "../i18n.svelte";

// The "Go live" menu of the voice bar: share the screen, the camera, or set
// up OBS.

export type GoLiveAction = Kind | "obs";
// off: why the entry cannot be used now.
export type GoLiveEntry = { action: GoLiveAction; label: string; off?: string };
export type GoLiveState = {
  sharing: Record<Kind, boolean>;
  // Our stream from OBS runs (it is our screen share on the server).
  obsLive: boolean;
  // What this browser can share (see sources).
  sources: Record<Kind, boolean>;
  // The server takes streams from OBS, and the account has a password.
  whip: boolean;
  password: boolean;
  // Why we may not stream here (see streamBlock), or null.
  blocked: string | null;
};

export const goLiveEntries = (s: GoLiveState): GoLiveEntry[] => {
  const entry = (kind: Kind, stop: string, start: string, busy: string | null = null): GoLiveEntry[] => {
    if (!s.sources[kind]) return [];
    if (s.sharing[kind]) return [{ action: kind, label: stop }];
    const off = s.blocked ?? busy;
    return [{ action: kind, label: start, ...(off && { off }) }];
  };
  return [
    ...entry("screen", t("media.stopSharing"), t("media.shareScreen"), s.obsLive ? t("media.stopObsFirst") : null),
    ...entry("camera", t("media.cameraOff"), t("media.cameraOn")),
    ...(s.whip && s.password ? [{ action: "obs" as const, label: t("media.streamFromObs") }] : []),
  ];
};

export const isLive = (s: Pick<GoLiveState, "sharing" | "obsLive">) => s.sharing.screen || s.sharing.camera || s.obsLive;

// Why the server would refuse a stream (as for speaking), from what we know:
// our mute and suppression, and the channel's permissions (undefined: not
// known yet, then the server decides).
export const streamBlock = (me: { mute: boolean; suppress: boolean }, permissions: number | undefined): string | null =>
  me.mute
    ? t("media.mutedByModerator")
    : me.suppress || (permissions !== undefined && (permissions & Perm.Speak) === 0)
      ? t("media.mayNotSpeak")
      : null;
