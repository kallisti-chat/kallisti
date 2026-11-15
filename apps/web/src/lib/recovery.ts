import { linkToken } from "./registration";

// An admin's recovery link (a new password, see the server's
// sidecar/recovery.go): like account invitations, the token is in the
// fragment, so page requests and Referer headers do not carry it.
export const recoveryLink = (origin: string, token: string) => `${origin}/recover#${token}`;

export const recoveryToken = (href: string, origin: string) => linkToken(href, origin, "/recover");
