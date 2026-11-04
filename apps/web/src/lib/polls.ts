import type { Key } from "../i18n/en";
import { t } from "./i18n.svelte";

// Polls, as in Discord (see the server's sidecar/polls.go): a message with
// a question and answers. Counts show after one's own vote or the end;
// anonymous polls come without names.

export type PollAnswer = { text: string; emoji?: string; votes: number; voters?: string[] };
// ends, ended: Unix milliseconds. voters: who voted (shares in multiple
// choice). mine: our answers, missing where the server sent the poll to
// all readers alike (then what we knew stays, see withPoll).
export type Poll = {
  question: string;
  answers: PollAnswer[];
  multiple?: boolean;
  anonymous?: boolean;
  ends: number;
  ended?: number;
  voters: number;
  mine?: number[];
};

const isNum = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);
const isStr = (v: unknown): v is string => typeof v === "string";
const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const optBool = (v: unknown) => v === undefined || typeof v === "boolean";
const isAnswer = (v: unknown): v is PollAnswer =>
  isObj(v) && isStr(v.text) && (v.emoji === undefined || isStr(v.emoji)) && isNum(v.votes) && (v.voters === undefined || (Array.isArray(v.voters) && v.voters.every(isStr)));

export const isPoll = (v: unknown): v is Poll =>
  isObj(v) &&
  isStr(v.question) &&
  Array.isArray(v.answers) &&
  v.answers.every(isAnswer) &&
  optBool(v.multiple) &&
  optBool(v.anonymous) &&
  isNum(v.ends) &&
  (v.ended === undefined || isNum(v.ended)) &&
  isNum(v.voters) &&
  (v.mine === undefined || (Array.isArray(v.mine) && v.mine.every(isNum)));

// A message with the poll the server sent, keeping our votes when it sent
// none. An open poll after an ended one is older news (a vote's counts
// sent after the end): the ended one stays.
export const withPoll = <M extends { poll?: Poll }>(m: M, poll: Poll): M =>
  m.poll?.ended && !poll.ended ? m : { ...m, poll: poll.mine === undefined && m.poll?.mine ? { ...poll, mine: m.poll.mine } : poll };

// The form (as the server checks it). Durations as in Discord, in hours.
export const POLL_QUESTION_MAX = 300;
export const POLL_ANSWER_MAX = 55;
export const POLL_ANSWERS_MAX = 10;
// The texts: keys (t() where shown).
export const POLL_DURATIONS: readonly (readonly [number, Key])[] = [
  [1, "chat.pollDuration1Hour"],
  [4, "chat.pollDuration4Hours"],
  [8, "chat.pollDuration8Hours"],
  [24, "chat.pollDuration24Hours"],
  [72, "chat.pollDuration3Days"],
  [168, "chat.pollDuration1Week"],
  [336, "chat.pollDuration2Weeks"],
];
export type PollDraft = { question: string; answers: { text: string; emoji?: string }[]; multiple: boolean; anonymous: boolean; hours: number };
export const emptyPollDraft = (): PollDraft => ({ question: "", answers: [{ text: "" }, { text: "" }], multiple: false, anonymous: false, hours: 24 });

// As the server: inner white space is one space.
const oneLine = (s: string) => s.split(/\s+/).filter(Boolean).join(" ");
const filled = (d: PollDraft) => d.answers.map((a) => ({ ...a, text: oneLine(a.text) })).filter((a) => a.text);

export const pollProblem = (d: PollDraft): string | null => {
  const q = oneLine(d.question);
  if (!q) return t("chat.pollAskQuestion");
  if ([...q].length > POLL_QUESTION_MAX) return t("chat.pollQuestionTooLong", { max: POLL_QUESTION_MAX });
  const answers = filled(d);
  if (answers.length < 2) return t("chat.pollTooFewAnswers");
  if (answers.some((a) => [...a.text].length > POLL_ANSWER_MAX)) return t("chat.pollAnswerTooLong", { max: POLL_ANSWER_MAX });
  return null;
};

export const pollBody = (d: PollDraft) => ({
  question: oneLine(d.question),
  answers: filled(d).map((a) => (a.emoji ? { text: a.text, emoji: a.emoji } : { text: a.text })),
  multiple: d.multiple,
  anonymous: d.anonymous,
  hours: d.hours,
});
export type PollBody = ReturnType<typeof pollBody>;

export const voteRequest = (message: number, answers: readonly number[]) => JSON.stringify({ type: "vote", message, answers });
export const endPollRequest = (message: number) => JSON.stringify({ type: "end-poll", message });

export const pollOpen = (p: Poll, now: number) => !p.ended && now < p.ends;
export const showResults = (p: Poll, now: number) => !pollOpen(p, now) || (p.mine?.length ?? 0) > 0;
export const share = (votes: number, voters: number) => (voters ? Math.round((votes * 100) / voters) : 0);
// The answers with the most votes (none without votes).
export const winners = (p: Poll) => {
  const best = Math.max(0, ...p.answers.map((a) => a.votes));
  return best ? p.answers.flatMap((a, i) => (a.votes === best ? [i] : [])) : [];
};

export const timeLeft = (ends: number, now: number) => {
  const minutes = Math.max(1, Math.ceil((ends - now) / 60_000));
  if (minutes <= 60) return t("chat.pollMinutesLeft", { count: minutes });
  const hours = Math.round(minutes / 60);
  return hours < 48 ? t("chat.pollHoursLeft", { count: hours }) : t("chat.pollDaysLeft", { count: Math.round(hours / 24) });
};
