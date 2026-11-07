# IRC bridge flood protection

## Summary

Add sender limits in both directions and paced, bounded delivery toward Kallisti in the standalone IRC bot.

## Requirements

- Limit each sender without letting one sender fill a channel's whole queue.
- Keep separate channel queues and serve outgoing IRC channels fairly.
- Pace Kallisti writes below its native-client limit; never block the IRC reader on a write.
- Bound sender-tracking memory and expire queued messages.
- Discard queued traffic when its source or destination disconnects or leaves a channel.
- Limit flood-drop logging rather than writing a log line for every rejected message.

## Acceptance criteria

- Local tests cover bursts, refill, other senders, channel fairness, expiry, bounded state, and disconnects.
- Race tests and vet pass, with independent code and security reviews.
- No test traffic is sent to live community channels; use local fixtures or isolated temporary channels only.

## Notes

Implementation belongs in `../kallisti-irc`. No Kallisti server changes are needed. Deployment details stay in untracked ops notes.
