# Voice CI: Safari fails now and then

## Summary

24 of the last 100 voice workflow runs failed, recently nearly all in
Safari. Two kinds:

- A stall: Safari sends nothing for more than 300 ms in a spurt, then the
  waiting packets at once (frame numbers continue, nothing lost).
  voicecheck counts every pause over 300 ms as a spurt end, so it reports
  a spurt without terminator.
- Safari (or its WebDriver) takes over 20 s to give the microphone.

## Acceptance Criteria

- voicecheck counts a pause without terminator whose next packet
  continues the frame numbers as a stall inside the spurt, reports it,
  and fails only for a stall over 1 s. A real missing terminator still
  fails (two spurts merge; the test expects 4).
- drive.mjs waits 45 s for Safari's microphone.

## Notes

- 2026-11-15: drive.mjs now prints startup diagnostics on failure (cortexobscura, from #28). First Safari failure with them (run 43724149048): after the click on "General" the page never asked for the microphone (`micRequests: 0`, stub installed, no page errors, status "Online"). So the "microphone" timeouts are, at least sometimes, a join that does not start capture, not a slow microphone. About 1 in 10 Safari runs fail (main and branches); the rerun passed.
