# Whisper, shout and priority speaker in the web client

## Summary

The built-in voice server has Mumble's voice targets (whisper to users,
shout to channels) and the priority speaker. The web client sends all
voice to its own channel (target 0) and has no control for either, so
Mumble groups lose them when they move to the web client.

## Requirements

- Whisper: from a user's menu, "Whisper" makes my voice go only to that
  user until I stop it. A bar by the voice controls says who hears me and
  has a stop button. Push-to-talk and voice activity work as before.
- Shout: the same from a voice channel's menu, to everyone in that
  channel.
- Hearing: a user who whispers or shouts to me is marked as such while
  they talk.
- Priority speaker: staff with the right to mute set it from the user
  menu. The user shows a mark. While a priority speaker talks, the others
  are quieter for me.
- The whisper ends when its user leaves or its channel goes, and on a
  reconnect.

## Acceptance Criteria

- Unit tests for the target logic and the ducking.
- A voice check with two headless clients (fake devices): a whisper
  reaches only its target.

## Notes

- My choice, open to change: a whisper is a mode you turn on and off, not
  a second hold key as in Mumble. It works on phones and needs no setup.
  A hold key can come later.
- One target (id 1) is enough for a mode.
