// Emoji reactions on stored messages (the server keeps them).
// users: names to show; ids: their Mumble user ids, in the same order.
export type Reaction = { emoji: string; users: string[]; ids: number[] };

// The quick reactions, in the server's form (see normalEmoji).
// Enough to fill both rows (see quickRows) before there is a history.
export const COMMON_EMOJI = [
  "👍", "❤\uFE0F", "😂", "🎉", "😮", "😢", "👀", "🙏", "🔥", "✅",
  "😊", "🤔", "😍", "😭", "💯", "👏", "😎", "🙌", "🥳", "💀",
];

// Ours: by our user id (null: not registered, so none).
export const reactedBy = (r: Reaction, myId: number | null) => myId !== null && r.ids.includes(myId);

// Who reacted, for the hover card: up to `max` names (ours counts as one,
// "you" at the end), then how many more.
export const whoReacted = (r: Reaction, myId: number | null, max = 3) => {
  const you = reactedBy(r, myId);
  const others = r.users.filter((_, i) => r.ids[i] !== myId);
  const names = others.slice(0, you ? max - 1 : max);
  return { names, you, more: others.length - names.length };
};

export const reactRequest = (message: number, emoji: string, on: boolean) =>
  JSON.stringify({ type: "react", message, emoji, on });
