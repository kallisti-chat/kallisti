<script lang="ts">
  import { untrack } from "svelte";
  import ModerationDialog from "../../src/components/ModerationDialog.svelte";
  import type { FinishedRecording } from "../../src/lib/recording";

  // As Client: the text state is replaced as a whole on each event, and the
  // dialog gets its fields as expressions.
  // recorded: the recorded channels (a "recording" event when it changes).
  let { onAsk, recorded = [] }: { onAsk: () => void; recorded?: number[] } = $props();
  let text = $state.raw<{ recording: readonly number[] | null; recordings: FinishedRecording[] | null }>({ recording: [], recordings: null });
  $effect(() => {
    const channels = recorded;
    untrack(() => (text = { ...text, recording: channels }));
  });
  const noop = async () => {};
  // The server's answer to a "recordings" request: a new list, later.
  const load = () => {
    onAsk();
    queueMicrotask(() => (text = { ...text, recordings: [{ id: "r1", channel: 5, channelName: "General", by: "admin", started: 1, stopped: 60_001, expires: 9e12, reason: "", size: 1, tracks: [] }] }));
  };
</script>

<ModerationDialog
  bans={null}
  registered={null}
  rights={{ bans: false, editBans: false, users: false, emoji: false }}
  onLoad={() => {}}
  onSetBans={noop}
  onUnregister={noop}
  emoji={[]}
  onAddEmoji={noop}
  onDeleteEmoji={noop}
  stickers={[]}
  onAddSticker={noop}
  onEditSticker={noop}
  onDeleteSticker={noop}
  onClose={() => {}}
  recording={text.recording}
  recordings={text.recordings}
  onLoadRecordings={load}
  onDownloadRecording={noop}
/>
