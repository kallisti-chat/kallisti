type ScrollState = { scrollHeight: number; scrollTop: number; clientHeight: number };

const AT_BOTTOM_PX = 80;

// Where the chat should scroll after its content changed: follow the bottom
// if the reader was there (or the conversation changed), else keep what the
// reader sees in place. `prepended`: content was added above (older history).
// Without a snapshot of before the change (a re-run with nothing new):
// null, stay where the reader is; or the end of a new conversation.
export const nextScrollTop = (
  before: ScrollState | null,
  newScrollHeight: number,
  switched: boolean,
  prepended = false,
): number | null => {
  if (!before) return switched ? newScrollHeight : null;
  const fromBottom = before.scrollHeight - before.scrollTop - before.clientHeight;
  if (switched || fromBottom <= AT_BOTTOM_PX) return newScrollHeight - before.clientHeight;
  return prepended ? before.scrollTop + (newScrollHeight - before.scrollHeight) : before.scrollTop;
};

// Far enough up in the history to offer "Jump to present": more than half
// a screen above the bottom.
export const awayFromBottom = (s: ScrollState) => s.scrollHeight - s.scrollTop - s.clientHeight > s.clientHeight / 2;

// Messages that came after the reader left the bottom (at: the newest
// message's time then; null: still at the bottom).
export const newSince = (times: readonly number[], at: number | null) => (at === null ? 0 : times.filter((t) => t > at).length);

// Close enough to the top to load older messages: within one and a half
// screens (the next page is there before the reader). A hidden list has
// no screen.
export const nearTop = (s: ScrollState) => s.scrollTop < s.clientHeight * 1.5;
