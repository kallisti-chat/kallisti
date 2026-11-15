// Account invitations use a separate path and a fragment, so the token is
// not sent in page requests or Referer headers. Channel links stay /invite/.
export const registrationLink = (origin: string, token: string) => `${origin}/register#${token}`;

// The token of this server's link at path (in its fragment), else null.
export const linkToken = (href: string, origin: string, path: string): string | null => {
  try {
    const u = new URL(href, origin);
    const token = u.hash.slice(1);
    return u.origin === new URL(origin).origin && u.pathname === path && /^[\w-]{32}$/.test(token) ? token : null;
  } catch {
    return null;
  }
};

export const registrationToken = (value: string, origin: string): string | null =>
  /^[\w-]{32}$/.test(value) ? value : linkToken(value, origin, "/register");
