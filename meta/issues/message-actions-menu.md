# Message actions: a short bar and a menu (as Discord)

## Requirements

- The hover bar shows every action as a word (React, Reply, Thread, Pin, Link, Original, Unread, Info, Delete): too wide, mostly unused (user, 2026-11-06).
- As Discord: a short bar of icons (React, Reply, Edit for own messages, Translate on messages in another language, and "More"); "More" and a right-click open a menu with all actions (the phone sheet's list).
- Keyboard and screen readers: the bar's buttons have labels; the menu is a menu (arrow keys, Escape).

## Acceptance Criteria

- The same actions as before, all reachable; delete still asks first.
- Checked in headless Chromium.
