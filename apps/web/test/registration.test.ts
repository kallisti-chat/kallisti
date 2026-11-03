import { describe, expect, it } from "vitest";
import { createAccountsApi } from "../src/lib/accounts";
import { registrationLink, registrationToken } from "../src/lib/registration";
import { parseInviteLink } from "../src/lib/invites";
import { serverSections } from "../src/lib/serverSettingsSections";
import { confirmsManage } from "../src/lib/saveFeedback";
import { initialTextState, manageRequest, parseServerEvent, reduceText } from "../src/lib/textChannels";

const token = "a".repeat(32);
const origin = "https://chat.example";

describe("account invitations", () => {
  it("keeps tokens out of page requests and separates account and channel links", () => {
    const link = registrationLink(origin, token);
    expect(new URL(link).pathname + new URL(link).search).toBe("/register");
    expect(registrationToken(link, origin)).toBe(token);
    expect(registrationToken(token, origin)).toBe(token);
    expect(registrationToken(link, "https://other.example")).toBeNull();
    expect(registrationToken(`${origin}/invite/${token}`, origin)).toBeNull();
    expect(parseInviteLink(link, origin)).toBeNull();
    expect(registrationToken(`${origin}/register#short`, origin)).toBeNull();
  });

  it("sends invitations only when creating accounts; older callers keep their request shape", async () => {
    const calls: { url: string; body: unknown }[] = [];
    const fetcher = (async (url: string, init: RequestInit) => {
      calls.push({ url, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify({ name: "alice" }), { status: 201 });
    }) as typeof fetch;
    const api = createAccountsApi(origin, fetcher);
    await api.create("alice", "password");
    await api.create("alice", "password", undefined, token);
    await api.fediCreate("alice", undefined, token);
    await api.login("alice", "password");
    expect(calls.map((c) => c.body)).toEqual([
      { name: "alice", password: "password" },
      { name: "alice", password: "password", invite: token },
      { name: "alice", invite: token },
      { name: "alice", password: "password" },
    ]);
  });

  it("does not fall back to anonymous access after an invite-only refusal", async () => {
    const api = createAccountsApi(origin, (async () => new Response(JSON.stringify({ error: "invite required", code: "invite_required" }), { status: 403 })) as typeof fetch);
    await expect(api.guest("guest")).rejects.toThrow("invite required");
  });

  it("advertises the policy without enabling controls on older servers", () => {
    const old = parseServerEvent('{"type":"ready","channels":[]}')!;
    expect(reduceText(initialTextState, old).inviteOnly).toBeNull();
    const ready = parseServerEvent('{"type":"ready","channels":[],"inviteOnly":true}')!;
    const state = reduceText(initialTextState, ready);
    expect(state.inviteOnly).toBe(true);
    const change = parseServerEvent('{"type":"invite-only","on":false}')!;
    expect(reduceText(state, change).inviteOnly).toBe(false);
    expect(parseServerEvent('{"type":"invite-only","on":"false"}')).toBeNull();
    expect(confirmsManage({ inviteOnly: false })!(change)).toBe(true);
    expect(confirmsManage({ inviteOnly: true })!(change)).toBe(false);
    expect(manageRequest({ inviteOnly: true })).toBe('{"type":"set-invite-only","on":true}');
    expect(manageRequest({ createRegistrationInvite: true, seconds: 604800, uses: 1 })).toBe('{"type":"create-registration-invite","seconds":604800,"uses":1}');
  });

  it("shows registration controls only with their explicit admin capability", () => {
    const rights = { server: true, bans: true, users: true, emoji: true, guests: true, threads: true, rules: true, connections: true, recordings: true };
    expect(serverSections(rights).some((s) => s.id === "registration")).toBe(false);
    expect(serverSections({ ...rights, registration: true }).some((s) => s.id === "registration")).toBe(true);
  });
});
