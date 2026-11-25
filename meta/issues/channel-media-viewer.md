# Arrow keys through all media of a channel

## Summary

kaia (#kallisti-dev 13631): left/right keys and arrows in a text channel go through its pictures and videos only. Today the picture viewer (Lightbox) steps only through the pictures of one message.

## Requirements

- The viewer opened from a chat goes through all pictures and videos of the loaded messages of that chat, in message order (keys, arrow buttons, swipe as today).
- At the first loaded item it loads older messages and continues, when there are more.
- Videos open in the viewer and play there.
- The count ("3 of 12") stays honest about what is loaded.

## Acceptance Criteria

- From a picture of one message, the right arrow goes on to the next message's picture or video.
- Older messages load when the viewer reaches the oldest loaded item.
- Tests for the item list across messages.
