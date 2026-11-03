// Account invitations use a separate path and a fragment, so the token is
// not sent in page requests or Referer headers. Channel links stay /invite/.
export const registrationLink = (origin: string, token: string) => `${origin}/register#${token}`;

export const registrationToken = (value: string, origin: string): string | null => {
  if (/^[\w-]{32}$/.test(value)) return value;
  try {
    const u = new URL(value, origin);
    return u.origin === new URL(origin).origin && u.pathname === "/register" && /^[\w-]{32}$/.test(u.hash.slice(1))
      ? u.hash.slice(1)
      : null;
  } catch {
    return null;
  }
};
