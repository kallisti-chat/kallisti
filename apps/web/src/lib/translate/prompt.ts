// The prompt for the translation model (Hy-MT2, see llm.ts): Tencent's
// template with background information (the app, the messages before) and
// reference translations, made for each message.

const APP =
  "Kallisti is a voice and text chat app like Discord (servers, text channels, voice channels, a web version and phone apps). People write short, casual chat messages.";

// The app's words, per language: the first form is the word to translate
// into, all forms are found in a message (Latin scripts: whole words).
type Words = Partial<Record<string, string[]>>;
const WORDS: Words[] = [
  {
    en: ["voice channel"],
    de: ["Sprachkanal", "Sprachkanäle"],
    fr: ["salon vocal", "salons vocaux"],
    es: ["canal de voz", "canales de voz"],
    ja: ["ボイスチャンネル"],
    zh: ["语音频道"],
    "zh-Hant": ["語音頻道"],
  },
  {
    en: ["text channel"],
    de: ["Textkanal", "Textkanäle"],
    fr: ["salon textuel", "salons textuels"],
    es: ["canal de texto", "canales de texto"],
    ja: ["テキストチャンネル"],
    zh: ["文字频道"],
    "zh-Hant": ["文字頻道"],
  },
  { en: ["server"], de: ["Server"], fr: ["serveur"], es: ["servidor"], ja: ["サーバー"], zh: ["服务器"], "zh-Hant": ["伺服器"] },
  { en: ["thread"], de: ["Thread"], fr: ["fil"], es: ["hilo"], ja: ["スレッド"], zh: ["子区"], "zh-Hant": ["子區"] },
  {
    en: ["crash", "crashes", "crashed"],
    de: ["abstürzen", "stürzt", "abgestürzt", "Absturz"],
    fr: ["planter", "plante", "planté", "plantage"],
    es: ["se cuelga", "se cierra sola", "crashea"],
    ja: ["クラッシュ", "落ちる", "落ちた"],
    zh: ["崩溃", "闪退"],
    "zh-Hant": ["崩潰", "閃退", "當機"],
  },
  {
    en: ["muted", "mute"],
    de: ["stummgeschaltet", "stumm"],
    fr: ["en sourdine", "muet"],
    es: ["silenciado", "silenciar"],
    ja: ["ミュート"],
    zh: ["静音", "闭麦"],
    "zh-Hant": ["靜音"],
  },
  {
    en: ["in the background", "background"],
    de: ["im Hintergrund", "Hintergrund"],
    fr: ["en arrière-plan", "arrière-plan"],
    es: ["en segundo plano", "segundo plano"],
    ja: ["バックグラウンド"],
    zh: ["后台"],
    "zh-Hant": ["背景執行", "後台"],
  },
  { en: ["phone"], de: ["Handy"], fr: ["tel", "tél", "portable"], es: ["móvil", "celular"], ja: ["スマホ"], zh: ["手机"], "zh-Hant": ["手機"] },
];

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
// A word or name in a text: whole words in Latin scripts (CJK has no spaces).
const has = (text: string, word: string) =>
  /^[\p{Script=Latin}\d\s'-]+$/u.test(word) ? new RegExp(`(?<![\\p{L}\\d])${escape(word)}(?![\\p{L}\\d])`, "iu").test(text) : text.includes(word);

// The reference translations for a message: the app's words in it, and the
// names in it (users, channels), kept as they are.
export const glossary = (text: string, from: string, to: string, names: readonly string[]): string[] => [
  ...WORDS.flatMap((w) => {
    const found = w[from]?.find((form) => has(text, form));
    const into = w[to]?.[0];
    return found && into ? [`${found} translates to ${into}`] : [];
  }),
  ...names.filter((n) => n.length > 1 && has(text, n)).map((n) => `${n} translates to ${n}`),
];

const NAMES: Record<string, string> = { zh: "Simplified Chinese", "zh-Hant": "Traditional Chinese" };
export const languageName = (code: string) => NAMES[code] ?? new Intl.DisplayNames("en", { type: "language" }).of(code) ?? code;

export type Context = { before: readonly { author: string; text: string }[]; names: readonly string[] };

const short = (s: string) => (s.length > 200 ? `${s.slice(0, 200)}…` : s);

export const promptFor = (text: string, from: string, to: string, { before, names }: Context) => {
  const terms = glossary(text, from, to, names);
  const background = [
    APP,
    ...(before.length ? ["The messages before it in the chat:", ...before.map((m) => `${m.author}: ${short(m.text)}`)] : []),
    ...(terms.length ? [`Reference translations: ${terms.join("; ")}`] : []),
  ].join("\n");
  return `[Background Information]\n${background}\n\nPlease translate the following text into ${languageName(to)}, taking the provided background information into consideration.\n\n[Source Text]\n${text}`;
};
