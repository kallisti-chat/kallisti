import { inviteOf, inviteRoute } from "./invites";
import { parseMessageLink } from "./messageLink";

// Links from elsewhere (several servers in one client): an invite clicked
// in any client or opened with ?join=, a push notice's message (?open=).
// That server goes on screen (added first, or its login first, if needed)
// and its client takes the link (see Client link): an invite is redeemed,
// a message opens. The links end with their home login.
export const createServerLinks = (deps: {
  worker?: Pick<EventTarget, "addEventListener" | "removeEventListener">;
  servers: () => readonly { origin: string; name: string; session: unknown }[];
  home: () => string;
  owner: () => string | null;
  select: (id: string) => void;
  // The add dialog: for a server of the list without a login here, or a
  // new address.
  add: (target: { server: { origin: string; name: string } } | { address: string }) => void;
}) => {
  let pending = $state<Record<string, string>>({});
  let message = $state<{ id: string; url: string; origin: string } | null>(null);
  let of = deps.owner();
  let disposed = false;
  const syncOwner = () => {
    const owner = deps.owner();
    if (owner === of) return;
    of = owner;
    pending = {};
    message = null;
  };
  const homeOrigin = () => new URL(deps.home()).origin;
  // One validated message, kept until its Client can use the link prop.
  const openMessageLink = (url: string): boolean => {
    if (disposed) return false;
    syncOwner();
    let origin: string;
    try {
      origin = new URL(url).origin;
    } catch {
      return false;
    }
    const id = origin === homeOrigin() ? "" : deps.servers().find((s) => s.origin === origin && s.session)?.origin;
    if (id === undefined || !parseMessageLink(url, origin)) return false;
    delete pending[id];
    message = { id, url, origin };
    deps.select(id);
    return true;
  };
  const onMessage = (event: Event) => {
    const data: unknown = (event as MessageEvent).data;
    if (data && typeof data === "object" && "type" in data && data.type === "open" && "url" in data && typeof data.url === "string")
      openMessageLink(data.url);
  };
  // The App owns this listener, not each mounted server Client.
  deps.worker?.addEventListener("message", onMessage);
  const dispose = $effect.root(() => {
    $effect(() => {
      syncOwner();
    });
  });
  return {
    // An invite link; false: not one, or a guest (who adds no servers).
    openInvite: (url: string): boolean => {
      if (disposed) return false;
      syncOwner();
      const inv = inviteOf(url);
      if (!inv) return false;
      const known = deps.servers().find((s) => s.origin === inv.origin);
      const route = inviteRoute(inv.origin, homeOrigin(), known && { session: !!known.session }, deps.owner() === null);
      if (route === null) return false;
      const id = route === "home" ? "" : inv.origin;
      if (message?.id === id) message = null;
      pending[id] = url;
      if (route !== "add") deps.select(id);
      if (route === "login") deps.add({ server: { origin: known!.origin, name: known!.name } });
      if (route === "add") deps.add({ address: inv.origin });
      return true;
    },
    // A message, chat or voice channel link of a server here (with a
    // session): shown there; false: others (they open as a page).
    openMessageLink,
    // The link for a server's client ("": home), until it is used.
    linkFor: (id: string): string | null => {
      if (disposed || deps.owner() !== of) return null;
      if (message?.id === id) {
        const valid = id === "" ? message.origin === homeOrigin() : deps.servers().some((s) => s.origin === id && s.session);
        return valid ? message.url : null;
      }
      return pending[id] ?? null;
    },
    used: (id: string) => {
      if (message?.id === id) message = null;
      delete pending[id];
    },
    dispose: () => {
      disposed = true;
      deps.worker?.removeEventListener("message", onMessage);
      dispose();
      pending = {};
      message = null;
    },
  };
};
