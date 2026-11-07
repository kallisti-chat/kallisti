// Short, soft notes (browser only) on the chosen output device: the mention
// ping, and the voice sounds (sounds.ts).
let ctx: AudioContext | null = null;
let last = 0;
// Scheduled oscillators plus calls awaiting resume keep the context active.
let active = 0;
const release = () => {
  if (--active === 0) void ctx?.suspend().catch(() => {});
};

// Each note starts `step` seconds after the one before; peak: its loudness.
export const playNotes = async (outputDeviceId: string | null, notes: readonly number[], step: number, peak: number) => {
  if (peak <= 0 || notes.length === 0) return;
  try {
    ctx ??= new AudioContext();
    active++;
    try {
      await ctx.resume();
      const sink = ctx as AudioContext & { setSinkId?: (id: string) => Promise<void> };
      void sink.setSinkId?.(outputDeviceId ?? "").catch(() => {});
      const t = ctx.currentTime;
      for (const [i, freq] of notes.entries()) {
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.frequency.value = freq;
        gain.gain.setValueAtTime(0.0001, t + i * step);
        gain.gain.exponentialRampToValueAtTime(peak, t + i * step + 0.01);
        gain.gain.exponentialRampToValueAtTime(0.0001, t + i * step + 0.18);
        osc.connect(gain).connect(ctx.destination);
        osc.onended = () => {
          osc.disconnect();
          gain.disconnect();
          release();
        };
        osc.start(t + i * step);
        osc.stop(t + i * step + 0.2);
        active++;
      }
    } finally {
      release();
    }
  } catch {
    // No audio: no sound.
  }
};

// Mentions: two notes, at most once a second.
export const playPing = (outputDeviceId: string | null) => {
  const now = performance.now();
  if (now - last < 1000) return;
  last = now;
  playNotes(outputDeviceId, [880, 1320], 0.09, 0.15);
};
