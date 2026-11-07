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

## Result

Completed with capture pause/terminal shutdown, shared voice context demand,
and notification context suspension. All changes exceeded the preselected
0.3 CPU percentage-point retention threshold in this isolated LibreWolf test.

Mean content-process CPU from two runs per build; 100% is one CPU core:

| State | Baseline | Capture only | All changes |
| --- | ---: | ---: | ---: |
| Fresh text, visible | 1.56% | 1.46% | 0.10% |
| Fresh text, hidden | 1.64% | 1.59% | 0.36% |
| After leaving voice, visible | 3.42% | 1.84% | 0.14% |
| After leaving voice, hidden | 3.82% | 2.26% | 0.54% |

Baseline/capture and context/full used separate ABBA blocks. The shared-context
reduction is observed between blocks, not isolated in a capture/context block.
These phases occur before notification audio is created. In the matched
context/full block after mute/unmute sounds, notification suspension reduced
hidden content CPU from 2.10% to 0.46%, and whole-browser CPU from 4.09% to
0.98%. Broader whole-browser comparisons were noisy and are not used for the
main claim. Hidden windows were verified individually; snapshots briefly
selected the app between them.

Tests first failed, then passed. All 1,404 web tests, Svelte checks and the
production build passed. A separate browser run checked hidden capture and
incoming playback, native peer receipt before and after rejoin, settings
capture/close, and repeated hidden notification playback. Stream sound demand,
including zero volume and deafen, is covered by unit/integration tests.
Independent source and measurement reviews completed. No live server or real
microphone was used. The original collection-heavy bursts remain unexplained.
