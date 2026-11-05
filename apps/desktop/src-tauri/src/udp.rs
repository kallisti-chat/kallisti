//! UDP voice without IO: the crypt state from CryptSetup, pings, whether
//! UDP works, and resync requests. The bridge does the sockets.


use std::time::{Duration, Instant};

use crate::crypt::CryptState;
use crate::frames::{fields, make_frame, CryptSetup, Value, CRYPT_SETUP, UDP_TUNNEL};

pub const PING_INTERVAL: Duration = Duration::from_secs(5);
// Two lost pings in a row are fine, three mean UDP is gone.
const UDP_TIMEOUT: Duration = Duration::from_secs(12);
// As the Mumble client: ask for a new nonce at most this often.
const RESYNC_INTERVAL: Duration = Duration::from_secs(5);

#[derive(Default)]
pub struct UdpVoice {
    crypt: Option<CryptState>,
    legacy: bool,
    last_good: Option<Instant>, // any packet that decrypted (resyncs)
    last_pong: Option<Instant>, // the way up works too
    last_resync: Option<Instant>,
    ping_count: u8,
    was_usable: bool,
    // Voice came over UDP: the server sends it there, no announce needed.
    heard: bool,
}

pub enum Incoming {
    /// A voice packet, the same bytes as in a UDPTunnel frame.
    Audio(Vec<u8>),
    Pong,
    Dropped,
    /// Packets do not decrypt: send this frame to ask for a new nonce.
    Resync(Vec<u8>),
}

/// Servers before 1.5 use the legacy UDP format.
pub fn is_legacy_server(version_body: &[u8]) -> bool {
    let (mut v1, mut v2) = (None, None);
    for (field, value) in fields(version_body).unwrap_or_default() {
        match (field, value) {
            (1, Value::Varint(v)) => v1 = Some(v),
            (5, Value::Varint(v)) => v2 = Some(v),
            _ => {}
        }
    }
    let (major, minor) = match (v2, v1) {
        (Some(v), _) => (v >> 48, (v >> 32) & 0xffff),
        (None, Some(v)) => (v >> 16, (v >> 8) & 0xff),
        (None, None) => return false,
    };
    (major, minor) < (1, 5)
}

fn ping_body(legacy: bool, timestamp: u8) -> Vec<u8> {
    if legacy { vec![0x20, timestamp] } else { vec![0x01, 0x08, timestamp] }
}

// Voice to the loopback target (31) without sound: Opus with sequence 0
// and length 0, or the protobuf format with only the target.
fn empty_voice(legacy: bool) -> Vec<u8> {
    if legacy { vec![0x9f, 0x00, 0x00] } else { vec![0x00, 0x08, 31] }
}

impl UdpVoice {
    pub fn set_legacy(&mut self, legacy: bool) {
        self.legacy = legacy;
    }

    /// Returns a frame for the server if it asked for our nonce.
    pub fn on_crypt_setup(&mut self, cs: &CryptSetup) -> Option<Vec<u8>> {
        match (cs.key, cs.client_nonce, cs.server_nonce) {
            (Some(key), Some(client), Some(server)) => {
                self.crypt = Some(CryptState::new(key, client, server));
                self.last_good = None;
                self.last_pong = None;
                self.heard = false;
                None
            }
            (None, None, Some(server)) => {
                self.crypt.as_mut()?.set_decrypt_iv(server);
                None
            }
            (None, None, None) => {
                let mut body = vec![0x12, 16];
                body.extend_from_slice(&self.crypt.as_ref()?.encrypt_iv());
                Some(make_frame(CRYPT_SETUP, &body))
            }
            _ => None,
        }
    }

    pub fn usable(&self, now: Instant) -> bool {
        self.last_pong.is_some_and(|t| now.saturating_duration_since(t) < UDP_TIMEOUT)
    }

    /// An encrypted ping, once the keys are known.
    pub fn ping(&mut self) -> Option<Vec<u8>> {
        let crypt = self.crypt.as_mut()?;
        // The timestamp only has to come back; keep it to one byte.
        self.ping_count = (self.ping_count + 1) & 0x7f;
        Some(crypt.encrypt(&ping_body(self.legacy, self.ping_count)))
    }

    /// Call on each ping tick. When UDP stops working, returns a frame for
    /// the server: Murmur keeps sending voice over UDP until it gets a
    /// UDPTunnel over TCP. It ignores the ping inside.
    pub fn check(&mut self, now: Instant) -> Option<Vec<u8>> {
        let usable = self.usable(now);
        let stopped = self.was_usable && !usable;
        self.was_usable = usable;
        if stopped {
            self.heard = false;
        }
        stopped.then(|| make_frame(UDP_TUNNEL, &ping_body(self.legacy, 0)))
    }

    /// Call after a pong. While UDP works but no voice came over it, a
    /// packet for the server: it sends voice over UDP only to clients that
    /// sent voice over UDP, so a listener would hear over TCP. Voice
    /// without sound to the loopback target: Kallisti's server drops it,
    /// and nobody hears it (Murmur echoes it: not for Murmur).
    pub fn announce(&mut self, now: Instant) -> Option<Vec<u8>> {
        if self.heard || !self.usable(now) {
            return None;
        }
        Some(self.crypt.as_mut()?.encrypt(&empty_voice(self.legacy)))
    }

    /// The encrypted packet for a UDPTunnel body, or None to keep it on TCP.
    pub fn outgoing(&mut self, now: Instant, plain: &[u8]) -> Option<Vec<u8>> {
        if !self.usable(now) {
            return None;
        }
        Some(self.crypt.as_mut()?.encrypt(plain))
    }

    pub fn incoming(&mut self, now: Instant, packet: &[u8]) -> Incoming {
        let Some(crypt) = self.crypt.as_mut() else { return Incoming::Dropped };
        let Some(plain) = crypt.decrypt(packet) else {
            let stale = |t: Option<Instant>| t.is_none_or(|t| now.saturating_duration_since(t) >= RESYNC_INTERVAL);
            if stale(self.last_good) && stale(self.last_resync) {
                self.last_resync = Some(now);
                return Incoming::Resync(make_frame(CRYPT_SETUP, &[]));
            }
            return Incoming::Dropped;
        };
        self.last_good = Some(now);
        let is_ping = match plain.first() {
            None => return Incoming::Dropped,
            Some(b) if self.legacy => b >> 5 == 1,
            Some(b) => *b == 1,
        };
        if is_ping {
            self.last_pong = Some(now);
            return Incoming::Pong;
        }
        // Voice while UDP works (not a late packet from before an outage).
        if self.usable(now) {
            self.heard = true;
        }
        Incoming::Audio(plain)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::crypt::CryptState;
    use crate::frames::{make_frame, CryptSetup, CRYPT_SETUP, UDP_TUNNEL};
    use std::time::{Duration, Instant};

    const KEY: [u8; 16] = [3; 16];
    const CLIENT: [u8; 16] = [1; 16];
    const SERVER: [u8; 16] = [2; 16];

    fn setup() -> CryptSetup {
        CryptSetup { key: Some(KEY), client_nonce: Some(CLIENT), server_nonce: Some(SERVER) }
    }

    // The server side of the crypt state, to make packets as Murmur does.
    fn murmur() -> CryptState {
        CryptState::new(KEY, SERVER, CLIENT)
    }

    #[test]
    fn udp_is_used_only_after_a_pong() {
        let now = Instant::now();
        let mut v = UdpVoice::default();
        assert!(v.ping().is_none(), "no keys yet");
        assert!(v.on_crypt_setup(&setup()).is_none());
        assert!(!v.usable(now));
        assert!(v.outgoing(now, &[0, 1, 2]).is_none(), "voice stays on TCP");

        // Murmur reads our ping and echoes it.
        let mut server = murmur();
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        assert_eq!(ping[0], 1, "protobuf ping type");
        assert!(matches!(v.incoming(now, &server.encrypt(&ping)), Incoming::Pong));
        assert!(v.usable(now));
        let voice = v.outgoing(now, &[0, 1, 2]).unwrap();
        assert_eq!(server.decrypt(&voice).unwrap(), vec![0, 1, 2]);

        // Without pongs for a while, voice goes back to TCP.
        assert!(!v.usable(now + Duration::from_secs(13)));
    }

    #[test]
    fn announces_udp_once_it_works_so_the_server_answers_over_udp() {
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        assert!(v.announce(now).is_none(), "no pong yet");
        let mut server = murmur();
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        v.incoming(now, &server.encrypt(&ping));
        // An empty voice packet to the loopback target: the server drops
        // it, but sends voice over UDP from now on.
        let announce = v.announce(now).unwrap();
        assert_eq!(server.decrypt(&announce).unwrap(), vec![0x00, 0x08, 31]);
        // Again after each pong (one may be lost), until voice comes.
        assert!(v.announce(now).is_some());
        assert!(matches!(v.incoming(now, &server.encrypt(&[0, 9, 9])), Incoming::Audio(_)));
        assert!(v.announce(now).is_none(), "voice came over UDP");
        // UDP stops and works again: announced again.
        let later = now + Duration::from_secs(13);
        assert!(v.check(now).is_none());
        assert!(v.check(later).is_some());
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        v.incoming(later, &server.encrypt(&ping));
        assert!(v.announce(later).is_some());

        let mut legacy = UdpVoice::default();
        legacy.set_legacy(true);
        legacy.on_crypt_setup(&setup());
        let mut server = murmur();
        let ping = server.decrypt(&legacy.ping().unwrap()).unwrap();
        legacy.incoming(now, &server.encrypt(&ping));
        assert_eq!(server.decrypt(&legacy.announce(now).unwrap()).unwrap(), vec![0x9f, 0x00, 0x00]);
    }

    #[test]
    fn late_voice_from_before_an_outage_does_not_count() {
        // UDP stops, a late voice packet still comes: after UDP works again
        // the server needs the announce again (it went back to TCP).
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        let mut server = murmur();
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        v.incoming(now, &server.encrypt(&ping));
        assert!(v.check(now).is_none());
        let later = now + Duration::from_secs(13);
        assert!(v.check(later).is_some());
        v.incoming(later, &server.encrypt(&[0, 9, 9]));
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        v.incoming(later, &server.encrypt(&ping));
        assert!(v.announce(later).is_some());
    }

    #[test]
    fn only_pongs_keep_udp_usable() {
        // Voice from the server proves only the way down: without pongs,
        // our voice goes back to TCP (the way up may be blocked).
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        let mut server = murmur();
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        v.incoming(now, &server.encrypt(&ping));
        let later = now + Duration::from_secs(13);
        assert!(matches!(v.incoming(later, &server.encrypt(&[0, 9, 9])), Incoming::Audio(_)));
        assert!(!v.usable(later));
        assert!(v.outgoing(later, &[0, 1, 2]).is_none());
    }

    #[test]
    fn tells_the_server_once_when_udp_stops_working() {
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        assert!(v.check(now).is_none(), "UDP never worked");
        let mut server = murmur();
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        v.incoming(now, &server.encrypt(&ping));
        assert!(v.check(now).is_none());
        // A ping in a UDPTunnel frame makes Murmur send voice over TCP again.
        let later = now + Duration::from_secs(13);
        let frame = v.check(later).unwrap();
        assert_eq!(frame, make_frame(UDP_TUNNEL, &[0x01, 0x08, 0]));
        assert!(v.check(later).is_none(), "only once");
    }

    #[test]
    fn audio_from_the_server_is_decrypted() {
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        let mut server = murmur();
        match v.incoming(now, &server.encrypt(&[0, 9, 9])) {
            Incoming::Audio(plain) => assert_eq!(plain, vec![0, 9, 9]),
            _ => panic!("expected audio"),
        }
        assert!(!v.usable(now), "only a pong proves the way up");
    }

    #[test]
    fn legacy_servers_get_legacy_pings() {
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.set_legacy(true);
        v.on_crypt_setup(&setup());
        let mut server = murmur();
        let ping = server.decrypt(&v.ping().unwrap()).unwrap();
        assert_eq!(ping[0] >> 5, 1, "legacy ping type");
        assert!(matches!(v.incoming(now, &server.encrypt(&ping)), Incoming::Pong));
        // 0x01 is not a ping in the legacy format.
        assert!(matches!(v.incoming(now, &server.encrypt(&[1, 5])), Incoming::Audio(_)));
    }

    #[test]
    fn answers_a_resync_request_with_the_client_nonce() {
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        v.ping(); // uses one IV
        let reply = v.on_crypt_setup(&CryptSetup::default()).unwrap();
        let mut expected = CLIENT;
        expected[0] += 1;
        let mut body = vec![0x12, 16];
        body.extend(expected);
        assert_eq!(reply, make_frame(CRYPT_SETUP, &body));
    }

    #[test]
    fn a_new_server_nonce_resyncs_decryption() {
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        let mut server = CryptState::new(KEY, [7; 16], CLIENT);
        assert!(matches!(v.incoming(now, &server.encrypt(&[0, 1])), Incoming::Dropped | Incoming::Resync(_)));
        v.on_crypt_setup(&CryptSetup { server_nonce: Some([7; 16]), ..Default::default() });
        assert!(matches!(v.incoming(now, &server.encrypt(&[0, 2])), Incoming::Audio(_)));
    }

    #[test]
    fn asks_for_a_resync_when_packets_stop_decrypting() {
        let now = Instant::now();
        let mut v = UdpVoice::default();
        v.on_crypt_setup(&setup());
        let mut wrong = CryptState::new(KEY, [7; 16], CLIENT);
        let request = make_frame(CRYPT_SETUP, &[]);
        match v.incoming(now, &wrong.encrypt(&[0, 1])) {
            Incoming::Resync(frame) => assert_eq!(frame, request),
            _ => panic!("expected a resync request"),
        }
        // Not again right away.
        assert!(matches!(v.incoming(now, &wrong.encrypt(&[0, 1])), Incoming::Dropped));
        let later = now + Duration::from_secs(6);
        assert!(matches!(v.incoming(later, &wrong.encrypt(&[0, 1])), Incoming::Resync(_)));
    }

    #[test]
    fn reads_the_server_version() {
        // version_v1 = 1.4.0
        assert!(is_legacy_server(&[0x08, 0x80, 0x88, 0x04]));
        // version_v2 = 1.5.0, the v1 field says 1.4 for old clients.
        let v2: u64 = (1 << 48) | (5 << 32);
        let mut body = vec![0x08, 0x80, 0x88, 0x04, 0x28];
        let mut n = v2;
        while n >= 0x80 {
            body.push((n as u8) | 0x80);
            n >>= 7;
        }
        body.push(n as u8);
        assert!(!is_legacy_server(&body));
    }
}
