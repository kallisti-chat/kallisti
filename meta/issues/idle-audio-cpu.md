# Stop unused audio work and measure idle CPU

## Summary

An isolated browser test found higher idle CPU after leaving voice. The capture worklet kept producing frames after the microphone stopped. The shared voice context also runs while text chat is idle. The user requests fixing both and keeping the changes only if measurements show a useful reduction.

## Requirements

- Stop unused capture processing, frame messages and noise filtering when capture stops; starting capture again must work.
- Suspend the shared audio context when no server needs voice, microphone testing or stream sound.
- Suspend the notification context after its last note, without losing later notification sounds.
- Keep active voice and stream sound working in hidden tabs. Preserve reconnects, microphone handover and platform startup behavior.
- Do not change chat pings, decorative media or UI timers in this work.
- Compare baseline and candidate with the same isolated fixtures and browser settings. Keep only changes with a useful measured benefit.

## Acceptance criteria

- Regression tests fail before implementation and pass after it.
- Repeat CPU measurements for fresh idle text chat and idle after leaving voice, in visible and genuinely hidden tabs, without profiler or diagnostic audio hooks.
- Check capture stop/restart, active background voice, stream sound demand and repeated notification sounds.
- Run web tests and type checks, and obtain an independent code review before the implementation commit.
- Record measurement results and discard changes that do not improve CPU use.

## Notes

The reported five-second collection-heavy bursts were not reproduced. This work addresses measured idle audio cost, not a confirmed cause of that separate profile. Deployment is outside this request.
