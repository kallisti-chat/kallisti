import { describe, expect, it } from "vitest";
import { recoveryLink, recoveryToken } from "../src/lib/recovery";
import { manageRequest, parseServerEvent } from "../src/lib/textChannels";
import { confirmsManage } from "../src/lib/saveFeedback";
import { createAccountsApi } from "../src/lib/accounts";

const origin = "https://chat.example";
const token = "a".repeat(32);

describe("recovery links", () => {
  it("carry the token in the fragment, for this server only", () => {
    expect(recoveryLink(origin, token)).toBe(`${origin}/recover#${token}`);
    expect(recoveryToken(`${origin}/recover#${token}`, origin)).toBe(token);
    expect(recoveryToken(`https://other.example/recover#${token}`, origin)).toBeNull();
    expect(recoveryToken(`${origin}/register#${token}`, origin)).toBeNull();
    expect(recoveryToken(`${origin}/recover#short`, origin)).toBeNull();
  });

  it("are asked for one registered user and answered to the admin only", () => {
    expect(manageRequest({ recoveryLink: 7 })).toBe('{"type":"recovery-link","user":7}');
    const answer = parseServerEvent(`{"type":"recovery-link","user":7,"token":"${token}"}`)!;
    expect(answer).toEqual({ type: "recovery-link", user: 7, token });
    expect(confirmsManage({ recoveryLink: 7 })!(answer)).toBe(true);
    expect(confirmsManage({ recoveryLink: 8 })!(answer)).toBe(false);
    expect(parseServerEvent('{"type":"recovery-link","user":7}')).toBeNull();
  });

  it("set a new password and log in", async () => {
    let sent: unknown;
    const fetchFn = (async (url: string, init: RequestInit) => {
      sent = [url, JSON.parse(init.body as string)];
      return new Response(JSON.stringify({ name: "erin" }));
    }) as typeof fetch;
    expect(await createAccountsApi(origin, fetchFn).recover(token, "a new password")).toBe("erin");
    expect(sent).toEqual([`${origin}/api/recover`, { token, new: "a new password" }]);
  });
});
