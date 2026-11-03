// How long a toast shows: 5 s, or 0.3 s a word for a long text (reading
// at about 200 words a minute).
export const toastMs = (text: string) => Math.max(5000, text.trim().split(/\s+/).length * 300);
