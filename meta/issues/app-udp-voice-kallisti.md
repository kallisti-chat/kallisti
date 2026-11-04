# Apps: voice over UDP on Kallisti servers

## Summary

On a Kallisti server the desktop and Android apps send voice inside the
WebSocket (TCP), as the browser does. On a bad network one lost packet
holds back everything after it until it is sent again: long silences
instead of short gaps. A user's server log (2026-11-04): an Android app
with ping 420 ms and 15.4 % of the audio late or lost to it
(`connection poor ... transport=websocket`); the others could not hear
each other at times.

For plain Mumble servers the apps already use UDP: the local bridge
(apps/desktop/src-tauri/src/bridge.rs, udp.rs) reads CryptSetup from the
frame stream, pings over UDP and moves UDPTunnel voice to UDP once a pong
came back (TCP stays the fallback). The built-in server already sends
CryptSetup to web sessions too and accepts UDP from clients by IP and key
(mumbled voice.go udpPacket, byHost).

Idea: in the apps, Kallisti connections also go through the bridge. The
bridge talks to the server's WebSocket (wss, with the session token
subprotocol) in place of TLS to a Mumble port, passes all frames through
(also Kallisti's own messages), and does UDP voice to the server's Mumble
port as for Mumble servers.

To check and decide:
- Does the server send voice to a web session over UDP once it had UDP
  audio from it (c.udpVoice), or always inside the WebSocket
  (session.go: web voice paths)? Voice to the client matters most here.
- The UDP address: the server's host and Mumble port (not always the web
  host; behind a proxy only the web port may be public). /api/server
  could tell it; no UDP when it is unknown or blocked (fallback: TCP).
- UDP from the app's real IP: the server knows web sessions by the IP the
  web proxy reports (TRUST_FORWARDED_FOR); the UDP packets must come from
  that IP (NAT, IPv4/IPv6 mix: the bridge connects both from one family).
- Mobile: Android in the background with the voice service (UDP socket
  in the bridge, not the WebView); iOS has no bridge.
- The connection info and the server's connection list show the
  transport ("UDP" / "TCP").

Not in this issue: browsers (they have no UDP; WebRTC or WebTransport
datagrams would be their way, a separate and bigger change).

## Acceptance Criteria

- The desktop and Android apps send and get voice over UDP on Kallisti
  servers when UDP works, and fall back to the WebSocket when it does not.
- Nothing changes for browsers and for plain Mumble servers.
- Checked with packet loss and high ping (for example tc netem): fewer and
  shorter gaps than over the WebSocket.
