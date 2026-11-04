//! Local bridge: the web client talks WebSocket to 127.0.0.1, the bridge
//! talks TLS to the Mumble server. The same unchanged web client then works
//! against any Mumble server, without the Kallisti server.
//!
//! Only this app may use it: it listens on 127.0.0.1 only, needs a random
//! secret in the URL, and checks the Origin (websites in a normal browser can
//! also open WebSockets to localhost).

use std::io::ErrorKind;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};
use std::time::Instant;

use crate::frames::{self, CryptSetup, Splitter, CRYPT_SETUP, UDP_TUNNEL, VERSION};
use crate::pins::{self, Check, PinStore};
use crate::udp::{self, Incoming, UdpVoice};

use futures_util::{SinkExt, StreamExt};
use rand::RngCore;
use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream, UdpSocket};
use tokio_tungstenite::tungstenite::{
    client::IntoClientRequest,
    handshake::server::{ErrorResponse, Request, Response},
    http::{header::SEC_WEBSOCKET_PROTOCOL, HeaderValue, StatusCode},
    protocol::{frame::coding::CloseCode, CloseFrame},
    Message,
};

pub struct Bridge {
    pub port: u16,
    pub secret: String,
}

const DEFAULT_PORT: u16 = 64738;

/// Where the bridge connects: a Mumble server (TLS), or a Kallisti server's
/// WebSocket, with voice over UDP to its Mumble port when it has one.
#[derive(Debug, PartialEq)]
pub enum Target {
    Mumble { host: String, port: u16 },
    Kallisti { url: String, udp: Option<u16> },
}

/// Reads the target server from the WebSocket URL query and checks the
/// secret.
pub fn parse_target(query: &str, secret: &str) -> Result<Target, &'static str> {
    let mut given_secret = None;
    let mut host = None;
    let mut port = None;
    let mut kallisti = None;
    let mut udp = None;
    for (key, value) in url::form_urlencoded::parse(query.as_bytes()) {
        match key.as_ref() {
            "secret" => given_secret = Some(value.into_owned()),
            "host" => host = Some(value.into_owned()),
            "port" => port = Some(value.into_owned()),
            "kallisti" => kallisti = Some(value.into_owned()),
            "udp" => udp = Some(value.into_owned()),
            _ => {}
        }
    }
    if !given_secret.is_some_and(|g| constant_time_eq(g.as_bytes(), secret.as_bytes())) {
        return Err("wrong secret");
    }
    let parse_port = |p: String| p.parse::<u16>().ok().filter(|p| *p > 0).ok_or("bad port");
    if let Some(url) = kallisti {
        let parsed = url::Url::parse(&url).map_err(|_| "bad url")?;
        if !matches!(parsed.scheme(), "ws" | "wss") || parsed.host_str().is_none_or(str::is_empty) {
            return Err("bad url");
        }
        return Ok(Target::Kallisti { url, udp: udp.map(parse_port).transpose()? });
    }
    let host = host.filter(|h| !h.is_empty()).ok_or("no host")?;
    let port = port.map(parse_port).transpose()?.unwrap_or(DEFAULT_PORT);
    Ok(Target::Mumble { host, port })
}

fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    a.len() == b.len() && a.iter().zip(b).fold(0u8, |acc, (x, y)| acc | (x ^ y)) == 0
}

/// Starts the bridge on 127.0.0.1 with a random port and secret.
pub async fn start(allowed_origins: Vec<String>, pins: Arc<PinStore>) -> std::io::Result<Bridge> {
    let listener = TcpListener::bind("127.0.0.1:0").await?;
    let port = listener.local_addr()?.port();
    let mut bytes = [0u8; 24];
    rand::rng().fill_bytes(&mut bytes);
    let secret = hex::encode(bytes);
    let shared = Arc::new((secret.clone(), allowed_origins));
    tokio::spawn(async move {
        loop {
            match listener.accept().await {
                Ok((tcp, _)) => {
                    let (shared, pins) = (shared.clone(), pins.clone());
                    tokio::spawn(async move {
                        let _ = serve(tcp, &shared.0, &shared.1, &pins).await;
                    });
                }
                // For example too many open files: keep the bridge alive.
                Err(err) => {
                    eprintln!("bridge: accept failed: {err}");
                    tokio::time::sleep(std::time::Duration::from_millis(200)).await;
                }
            }
        }
    });
    Ok(Bridge { port, secret })
}

async fn serve(
    tcp: TcpStream,
    secret: &str,
    origins: &[String],
    pins: &PinStore,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let mut target = None;
    let mut origin = String::new();
    let mut protocols = Vec::new();
    let check = |req: &Request, mut resp: Response| -> Result<Response, ErrorResponse> {
        let given = req.headers().get("Origin").and_then(|o| o.to_str().ok()).unwrap_or("");
        let refuse = |status: StatusCode| {
            let mut r = ErrorResponse::new(None);
            *r.status_mut() = status;
            r
        };
        if !origins.iter().any(|o| o == given) {
            return Err(refuse(StatusCode::FORBIDDEN));
        }
        match parse_target(req.uri().query().unwrap_or(""), secret) {
            Ok(t) => {
                // A Kallisti server gets the client's subprotocols (its
                // session token) and answers "kallisti", as here.
                protocols = asked_protocols(req);
                if matches!(t, Target::Kallisti { .. }) && protocols.iter().any(|p| p == "kallisti") {
                    resp.headers_mut().insert(SEC_WEBSOCKET_PROTOCOL, HeaderValue::from_static("kallisti"));
                }
                origin = given.to_string();
                target = Some(t);
                Ok(resp)
            }
            Err(_) => Err(refuse(StatusCode::FORBIDDEN)),
        }
    };
    // Idle local sockets must not pile up.
    let mut ws = tokio::time::timeout(HANDSHAKE_TIMEOUT, tokio_tungstenite::accept_hdr_async(tcp, check)).await??;
    let target = target.ok_or("no target")?;
    let name = match &target {
        Target::Mumble { host, port } => format!("{host}:{port}"),
        Target::Kallisti { url, .. } => url.clone(),
    };

    // A client that leaves cancels the connect.
    let opening = open_upstream(&target, &origin, &protocols);
    let Some((opened, early)) = while_reading(&mut ws, opening).await else {
        return Ok(());
    };
    let upstream = match opened {
        Ok(u) => u,
        Err(err) => {
            // Tell the client why, instead of just dropping the socket.
            close_with(&mut ws, CloseCode::Error, format!("cannot reach {name}: {err}")).await;
            return Err(err);
        }
    };
    // Trust on first use: nothing from the client reaches an unknown or
    // changed Mumble server. The client asks the user and trusts via the
    // app. A Kallisti server's certificate is checked as browsers do.
    if let Some((fp, trusted_ca)) = &upstream.cert {
        let refused = refusal(*trusted_ca, pins.check(&name, fp), fp);
        if refused.is_some() {
            pins.offer(&name, fp);
        }
        if let Some(reason) = refused {
            close_with(&mut ws, CloseCode::Policy, reason).await;
            return Ok(());
        }
    }
    relay(ws, early, upstream).await?;
    Ok(())
}

/// The subprotocols the client asked for, in order.
fn asked_protocols(req: &Request) -> Vec<String> {
    req.headers()
        .get_all(SEC_WEBSOCKET_PROTOCOL)
        .iter()
        .filter_map(|v| v.to_str().ok())
        .flat_map(|v| v.split(','))
        .map(|p| p.trim().to_string())
        .filter(|p| !p.is_empty())
        .collect()
}

/// Anything both ways: TLS, plain TCP (a local Kallisti server).
trait Io: AsyncRead + AsyncWrite + Unpin + Send {}
impl<T: AsyncRead + AsyncWrite + Unpin + Send> Io for T {}

type KallistiSocket = tokio_tungstenite::WebSocketStream<Box<dyn Io>>;

/// The server side: the Mumble stream over TLS, or a Kallisti server's
/// WebSocket (binary messages: the Mumble stream; text: its own API).
struct Upstream {
    read: UpRead,
    write: UpWrite,
    udp: Option<SocketAddr>,     // where voice goes over UDP, if anywhere
    cert: Option<(String, bool)>, // a Mumble server's: fingerprint, CA-valid
}

enum UpRead {
    Tls(tokio::io::ReadHalf<tokio_rustls::client::TlsStream<TcpStream>>),
    Ws(futures_util::stream::SplitStream<KallistiSocket>),
}

enum UpWrite {
    Tls(tokio::io::WriteHalf<tokio_rustls::client::TlsStream<TcpStream>>),
    Ws(futures_util::stream::SplitSink<KallistiSocket, Message>),
}

enum UpEvent {
    Mumble(Vec<u8>),
    Text(tokio_tungstenite::tungstenite::Utf8Bytes),
}

impl UpRead {
    /// The next data from the server; None at its end.
    async fn next(&mut self, buf: &mut [u8]) -> std::io::Result<Option<UpEvent>> {
        match self {
            UpRead::Tls(r) => {
                let n = r.read(buf).await?;
                Ok((n > 0).then(|| UpEvent::Mumble(buf[..n].to_vec())))
            }
            UpRead::Ws(r) => loop {
                match r.next().await {
                    Some(Ok(Message::Binary(data))) => return Ok(Some(UpEvent::Mumble(data.to_vec()))),
                    Some(Ok(Message::Text(text))) => return Ok(Some(UpEvent::Text(text))),
                    Some(Ok(Message::Close(_))) | None => return Ok(None),
                    Some(Ok(_)) => continue, // pings: answered by tungstenite
                    Some(Err(err)) => return Err(std::io::Error::other(err)),
                }
            },
        }
    }
}

impl UpWrite {
    async fn mumble(&mut self, bytes: Vec<u8>) -> std::io::Result<()> {
        match self {
            UpWrite::Tls(w) => w.write_all(&bytes).await,
            UpWrite::Ws(w) => w.send(Message::Binary(bytes.into())).await.map_err(std::io::Error::other),
        }
    }

    /// Kallisti's own API; a Mumble server has none.
    async fn text(&mut self, text: tokio_tungstenite::tungstenite::Utf8Bytes) -> std::io::Result<()> {
        match self {
            UpWrite::Tls(_) => Ok(()),
            UpWrite::Ws(w) => w.send(Message::Text(text)).await.map_err(std::io::Error::other),
        }
    }

    async fn close(&mut self) -> std::io::Result<()> {
        match self {
            UpWrite::Tls(w) => w.shutdown().await,
            UpWrite::Ws(w) => w.close().await.map_err(std::io::Error::other),
        }
    }
}

async fn open_upstream(
    target: &Target,
    origin: &str,
    protocols: &[String],
) -> Result<Upstream, Box<dyn std::error::Error + Send + Sync>> {
    match target {
        Target::Mumble { host, port } => {
            let (stream, fp, trusted, peer) = connect_upstream(host, *port, HANDSHAKE_TIMEOUT).await?;
            let (read, write) = tokio::io::split(stream);
            Ok(Upstream { read: UpRead::Tls(read), write: UpWrite::Tls(write), udp: Some(peer), cert: Some((fp, trusted)) })
        }
        Target::Kallisti { url, udp } => {
            let (socket, peer) = connect_kallisti(url, origin, protocols, HANDSHAKE_TIMEOUT).await?;
            let (write, read) = socket.split();
            // The Mumble port on the host that serves the WebSocket: the
            // server knows the session by this IP.
            let udp = udp.map(|port| SocketAddr::new(peer.ip(), port));
            Ok(Upstream { read: UpRead::Ws(read), write: UpWrite::Ws(write), udp, cert: None })
        }
    }
}

/// Connects to a Kallisti server's WebSocket within `limit`, with the
/// client's Origin and subprotocols. Returns the server's address.
async fn connect_kallisti(
    url: &str,
    origin: &str,
    protocols: &[String],
    limit: std::time::Duration,
) -> Result<(KallistiSocket, SocketAddr), Box<dyn std::error::Error + Send + Sync>> {
    tokio::time::timeout(limit, async {
        let parsed = url::Url::parse(url)?;
        let addrs = parsed.socket_addrs(|| None)?;
        let tcp = TcpStream::connect(&*addrs).await?;
        tcp.set_nodelay(true)?;
        let peer = tcp.peer_addr()?;
        let stream: Box<dyn Io> = if parsed.scheme() == "wss" {
            let host = parsed.host_str().ok_or("no host")?.trim_start_matches('[').trim_end_matches(']');
            let name = rustls::pki_types::ServerName::try_from(host.to_string())?;
            Box::new(web_tls().connect(name, tcp).await?)
        } else {
            Box::new(tcp)
        };
        let mut request = url.into_client_request()?;
        let headers = request.headers_mut();
        if !origin.is_empty() {
            headers.insert("Origin", origin.parse()?);
        }
        if !protocols.is_empty() {
            headers.insert(SEC_WEBSOCKET_PROTOCOL, protocols.join(", ").parse()?);
        }
        let (socket, _) = tokio_tungstenite::client_async(request, stream).await?;
        Ok((socket, peer))
    })
    .await?
}

/// TLS as browsers check it (the usual certificate authorities), over
/// HTTP/1.1 (WebSockets need it).
fn web_tls() -> tokio_rustls::TlsConnector {
    static CONFIG: std::sync::OnceLock<Arc<rustls::ClientConfig>> = std::sync::OnceLock::new();
    let config = CONFIG.get_or_init(|| {
        let roots = rustls::RootCertStore { roots: webpki_roots::TLS_SERVER_ROOTS.to_vec() };
        let mut config = rustls::ClientConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_safe_default_protocol_versions()
            .expect("TLS versions")
            .with_root_certificates(roots)
            .with_no_client_auth();
        config.alpn_protocols = vec![b"http/1.1".to_vec()];
        Arc::new(config)
    });
    tokio_rustls::TlsConnector::from(config.clone())
}

/// Moves data between the client and the server until either ends. Voice
/// goes over UDP while UDP works, everything else the way it came.
async fn relay(
    ws: tokio_tungstenite::WebSocketStream<TcpStream>,
    early: Vec<Message>,
    upstream: Upstream,
) -> std::io::Result<()> {
    let Upstream { read: mut up_read, write: up_write, udp: udp_peer, .. } = upstream;
    let udp = match udp_peer {
        Some(peer) => udp_socket(peer).await.ok(),
        None => None,
    };
    let voice = Mutex::new(UdpVoice::default());
    let up_write = tokio::sync::Mutex::new(up_write);
    let (ws_write, ws_read) = ws.split();
    let mut ws_read = futures_util::stream::iter(early.into_iter().map(Ok)).chain(ws_read);
    let ws_write = tokio::sync::Mutex::new(ws_write);

    // Client to server: binary messages are the Mumble stream.
    let to_server = async {
        let mut splitter = Splitter::default();
        while let Some(Ok(msg)) = ws_read.next().await {
            let data = match msg {
                Message::Binary(data) => data,
                Message::Text(text) => {
                    up_write.lock().await.text(text).await?;
                    continue;
                }
                Message::Close(_) => break,
                _ => continue,
            };
            let mut tcp = Vec::with_capacity(data.len());
            for frame in splitter.feed(&data).map_err(std::io::Error::other)? {
                let via_udp = udp.as_ref().filter(|_| frame.typ == UDP_TUNNEL);
                let packet = via_udp.and_then(|_| voice.lock().unwrap().outgoing(Instant::now(), frame.body()));
                match (via_udp, packet) {
                    (Some(socket), Some(packet)) => {
                        let _ = socket.send(&packet).await;
                    }
                    _ => tcp.extend_from_slice(&frame.raw),
                }
            }
            if !tcp.is_empty() {
                up_write.lock().await.mumble(tcp).await?;
            }
        }
        up_write.lock().await.close().await
    };
    // Server to client, unchanged. The bridge reads the server's Version
    // (UDP format) and CryptSetup (UDP keys) on the way. Only whole frames
    // go to the client, so UDP voice never lands inside a TCP frame.
    let to_client = async {
        let mut splitter = Splitter::default();
        let mut buf = vec![0u8; 32 * 1024];
        while let Some(event) = up_read.next(&mut buf).await? {
            let data = match event {
                UpEvent::Text(text) => {
                    if ws_write.lock().await.send(Message::Text(text)).await.is_err() {
                        break;
                    }
                    continue;
                }
                UpEvent::Mumble(data) => data,
            };
            let mut out = Vec::with_capacity(data.len());
            for frame in splitter.feed(&data).map_err(std::io::Error::other)? {
                match frame.typ {
                    VERSION => voice.lock().unwrap().set_legacy(udp::is_legacy_server(frame.body())),
                    CRYPT_SETUP => {
                        let reply = CryptSetup::parse(frame.body()).and_then(|cs| voice.lock().unwrap().on_crypt_setup(&cs));
                        if let Some(reply) = reply {
                            up_write.lock().await.mumble(reply).await?;
                        }
                        send_ping(&udp, &voice).await; // find out at once if UDP works
                    }
                    _ => {}
                }
                out.extend_from_slice(&frame.raw);
            }
            if !out.is_empty() && ws_write.lock().await.send(Message::Binary(out.into())).await.is_err() {
                break;
            }
        }
        let _ = ws_write.lock().await.close().await;
        Ok::<_, std::io::Error>(())
    };
    // UDP from the server, and pings.
    let udp_loop = async {
        let Some(socket) = &udp else { return std::future::pending::<std::io::Result<()>>().await };
        let mut buf = vec![0u8; 2048];
        let mut pings = tokio::time::interval(udp::PING_INTERVAL);
        loop {
            tokio::select! {
                _ = pings.tick() => {
                    let to_tcp = voice.lock().unwrap().check(Instant::now());
                    if let Some(frame) = to_tcp {
                        up_write.lock().await.mumble(frame).await?;
                    }
                    send_ping(&udp, &voice).await;
                }
                r = socket.recv(&mut buf) => {
                    let n = match r {
                        Ok(n) => n,
                        // ICMP replies (port unreachable): no UDP for now.
                        Err(e) if matches!(e.kind(), ErrorKind::ConnectionRefused | ErrorKind::ConnectionReset) => continue,
                        // Anything else: give up on UDP, keep the TCP session.
                        Err(_) => return std::future::pending().await,
                    };
                    let incoming = voice.lock().unwrap().incoming(Instant::now(), &buf[..n]);
                    match incoming {
                        Incoming::Audio(plain) => {
                            let frame = frames::make_frame(UDP_TUNNEL, &plain);
                            if ws_write.lock().await.send(Message::Binary(frame.into())).await.is_err() {
                                return Ok(());
                            }
                        }
                        Incoming::Resync(request) => up_write.lock().await.mumble(request).await?,
                        Incoming::Pong | Incoming::Dropped => {}
                    }
                }
            }
        }
    };
    // Whichever side ends first ends the connection.
    tokio::select! {
        r = to_server => r,
        r = to_client => r,
        r = udp_loop => r,
    }
}

/// Runs `work` while it reads the client: what the client sends meanwhile
/// is kept for the server. None when the client leaves first.
async fn while_reading<T>(
    ws: &mut tokio_tungstenite::WebSocketStream<TcpStream>,
    work: impl std::future::Future<Output = T>,
) -> Option<(T, Vec<Message>)> {
    tokio::pin!(work);
    let mut early = Vec::new();
    loop {
        tokio::select! {
            done = &mut work => return Some((done, early)),
            msg = ws.next() => match msg {
                Some(Ok(Message::Close(_)) | Err(_)) | None => return None,
                Some(Ok(msg)) => early.push(msg),
            },
        }
    }
}

async fn udp_socket(peer: SocketAddr) -> std::io::Result<UdpSocket> {
    let local = if peer.is_ipv4() { "0.0.0.0:0" } else { "[::]:0" };
    let socket = UdpSocket::bind(local).await?;
    socket.connect(peer).await?;
    Ok(socket)
}

async fn send_ping(udp: &Option<UdpSocket>, voice: &Mutex<UdpVoice>) {
    let Some(socket) = udp else { return };
    let ping = voice.lock().unwrap().ping();
    if let Some(ping) = ping {
        let _ = socket.send(&ping).await;
    }
}

const HANDSHAKE_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(10);

/// Closes with a reason and waits (briefly) for the client's close reply.
/// The client may already have sent data (the Mumble handshake): dropping
/// the socket with unread data makes the OS send a TCP reset, and browsers
/// then lose the close frame (code 1006 instead of our reason).
async fn close_with(ws: &mut tokio_tungstenite::WebSocketStream<TcpStream>, code: CloseCode, reason: String) {
    if ws.close(Some(CloseFrame { code, reason: reason.into() })).await.is_err() {
        return;
    }
    let _ = tokio::time::timeout(std::time::Duration::from_secs(2), async {
        while let Some(Ok(_)) = ws.next().await {}
    })
    .await;
}

/// What to refuse a server for: nothing when the web PKI trusts its
/// certificate for the name (renewals need no new pin), else trust on
/// first use with the pins.
fn refusal(ca_valid: bool, check: Check, fp: &str) -> Option<String> {
    match (ca_valid, check) {
        (true, _) | (false, Check::Trusted) => None,
        (false, Check::Unknown) => Some(format!("certificate-unknown {fp}")),
        (false, Check::Changed) => Some(format!("certificate-changed {fp}")),
    }
}

/// Whether the web PKI (the usual certificate authorities) trusts the
/// certificate for the name, for example one from Let's Encrypt.
fn ca_valid(
    end_entity: &rustls::pki_types::CertificateDer<'_>,
    intermediates: &[rustls::pki_types::CertificateDer<'_>],
    name: &rustls::pki_types::ServerName<'_>,
    now: rustls::pki_types::UnixTime,
) -> bool {
    use rustls::client::danger::ServerCertVerifier;
    static VERIFIER: std::sync::OnceLock<Option<Arc<rustls::client::WebPkiServerVerifier>>> = std::sync::OnceLock::new();
    let verifier = VERIFIER.get_or_init(|| {
        let roots = rustls::RootCertStore { roots: webpki_roots::TLS_SERVER_ROOTS.to_vec() };
        rustls::client::WebPkiServerVerifier::builder_with_provider(Arc::new(roots), Arc::new(rustls::crypto::ring::default_provider()))
            .build()
            .ok()
    });
    verifier.as_ref().is_some_and(|v| v.verify_server_cert(end_entity, intermediates, name, &[], now).is_ok())
}

type TlsUpstream = (tokio_rustls::client::TlsStream<TcpStream>, String, bool, SocketAddr);

/// Connects with TLS, TCP and handshake within `limit`. Returns the server
/// certificate's fingerprint, whether the web PKI trusts it, and the server
/// address (for UDP).
async fn connect_upstream(
    host: &str,
    port: u16,
    limit: std::time::Duration,
) -> Result<TlsUpstream, Box<dyn std::error::Error + Send + Sync>> {
    tokio::time::timeout(limit, async {
        let tcp = TcpStream::connect((host, port)).await?;
        tcp.set_nodelay(true)?;
        let peer = tcp.peer_addr()?;
        let name = rustls::pki_types::ServerName::try_from(host.to_string())?;
        let seen = Arc::new(Mutex::new(None));
        let tls = tokio_rustls::TlsConnector::from(Arc::new(tls_config(seen.clone())));
        let stream = tls.connect(name, tcp).await?;
        let (fp, trusted) = seen.lock().unwrap().take().ok_or("no server certificate")?;
        Ok((stream, fp, trusted, peer))
    })
    .await?
}

/// Mumble servers usually have self-signed certificates, so the CA check
/// does not decide alone: the verifier records the certificate's
/// fingerprint and whether the web PKI trusts it, and the bridge decides
/// (see refusal).
fn tls_config(seen: Arc<Mutex<Option<(String, bool)>>>) -> rustls::ClientConfig {
    let provider = Arc::new(rustls::crypto::ring::default_provider());
    rustls::ClientConfig::builder_with_provider(provider.clone())
        .with_safe_default_protocol_versions()
        .expect("TLS versions")
        .dangerous()
        .with_custom_certificate_verifier(Arc::new(Recording { provider, seen }))
        .with_no_client_auth()
}

#[derive(Debug)]
struct Recording {
    provider: Arc<rustls::crypto::CryptoProvider>,
    seen: Arc<Mutex<Option<(String, bool)>>>,
}

impl rustls::client::danger::ServerCertVerifier for Recording {
    fn verify_server_cert(
        &self,
        end_entity: &rustls::pki_types::CertificateDer<'_>,
        intermediates: &[rustls::pki_types::CertificateDer<'_>],
        server_name: &rustls::pki_types::ServerName<'_>,
        _ocsp: &[u8],
        now: rustls::pki_types::UnixTime,
    ) -> Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        let trusted = ca_valid(end_entity, intermediates, server_name, now);
        *self.seen.lock().unwrap() = Some((pins::fingerprint(end_entity), trusted));
        Ok(rustls::client::danger::ServerCertVerified::assertion())
    }

    // Signatures are still checked: the connection is encrypted to whoever
    // holds the certificate's key.
    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(message, cert, dss, &self.provider.signature_verification_algorithms)
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(message, cert, dss, &self.provider.signature_verification_algorithms)
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        self.provider.signature_verification_algorithms.supported_schemes()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn certificates_valid_for_the_name_need_no_pin() {
        assert_eq!(refusal(true, Check::Unknown, "ab"), None);
        assert_eq!(refusal(true, Check::Changed, "ab"), None);
        assert_eq!(refusal(false, Check::Trusted, "ab"), None);
        assert_eq!(refusal(false, Check::Unknown, "ab"), Some("certificate-unknown ab".into()));
        assert_eq!(refusal(false, Check::Changed, "ab"), Some("certificate-changed ab".into()));
    }

    // Needs the network: cargo test -- --ignored
    #[tokio::test]
    #[ignore]
    async fn a_lets_encrypt_certificate_is_ca_valid() {
        let (_, _, trusted, _) = connect_upstream("chat.kallisti.chat", 443, HANDSHAKE_TIMEOUT).await.unwrap();
        assert!(trusted);
        let (_, _, trusted, _) = connect_upstream("chat.kallisti.chat", 64738, HANDSHAKE_TIMEOUT).await.unwrap();
        assert!(trusted); // the Mumble port uses the web certificate too
    }

    #[tokio::test]
    async fn a_stalled_tls_handshake_times_out() {
        // The server accepts TCP but never answers TLS.
        let stalled = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = stalled.local_addr().unwrap().port();
        let limit = std::time::Duration::from_millis(200);
        let result = tokio::time::timeout(std::time::Duration::from_secs(5), connect_upstream("127.0.0.1", port, limit)).await;
        assert!(matches!(result, Ok(Err(_))));
        drop(stalled);
    }

    #[test]
    fn self_signed_is_not_ca_valid() {
        let cert = rcgen::generate_simple_self_signed(vec!["chat.example".into()]).unwrap();
        let der = rustls::pki_types::CertificateDer::from(cert.cert.der().to_vec());
        let name = rustls::pki_types::ServerName::try_from("chat.example").unwrap();
        assert!(!ca_valid(&der, &[], &name, rustls::pki_types::UnixTime::now()));
    }

    /// A Kallisti server's WebSocket on localhost: it checks the client's
    /// Origin and subprotocols, answers "kallisti", and hands the socket over.
    async fn fake_kallisti() -> (u16, tokio::sync::oneshot::Receiver<(String, String, tokio_tungstenite::WebSocketStream<TcpStream>)>) {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        let (tx, rx) = tokio::sync::oneshot::channel();
        tokio::spawn(async move {
            let (tcp, _) = listener.accept().await.unwrap();
            let mut seen = (String::new(), String::new());
            let ws = tokio_tungstenite::accept_hdr_async(tcp, |req: &Request, mut resp: Response| {
                let header = |name| req.headers().get(name).and_then(|v| v.to_str().ok()).unwrap_or("").to_string();
                seen = (header("Origin"), header("Sec-WebSocket-Protocol"));
                resp.headers_mut().insert(SEC_WEBSOCKET_PROTOCOL, HeaderValue::from_static("kallisti"));
                Ok(resp)
            })
            .await
            .unwrap();
            let _ = tx.send((seen.0, seen.1, ws));
        });
        (port, rx)
    }

    fn crypt_setup() -> Vec<u8> {
        let mut body = Vec::new();
        for (tag, byte) in [(0x0a, 1u8), (0x12, 2), (0x1a, 3)] {
            body.extend_from_slice(&[tag, 16]);
            body.extend_from_slice(&[byte; 16]);
        }
        frames::make_frame(CRYPT_SETUP, &body)
    }

    #[tokio::test]
    async fn bridges_a_kallisti_server_with_udp_voice() {
        let (server_port, server) = fake_kallisti().await;
        let udp = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let udp_port = udp.local_addr().unwrap().port();
        let bridge = start(vec!["http://app.local".into()], Arc::new(PinStore::in_memory())).await.unwrap();
        let target = format!("ws://127.0.0.1:{server_port}/ws");
        let url = format!(
            "ws://127.0.0.1:{}/?secret={}&kallisti={}&udp={udp_port}",
            bridge.port,
            bridge.secret,
            url::form_urlencoded::byte_serialize(target.as_bytes()).collect::<String>()
        );
        let mut request = url.into_client_request().unwrap();
        request.headers_mut().insert("Origin", HeaderValue::from_static("http://app.local"));
        request.headers_mut().insert(SEC_WEBSOCKET_PROTOCOL, HeaderValue::from_static("kallisti, kallisti.session.tok"));
        let (mut client, response) = tokio_tungstenite::connect_async(request).await.unwrap();
        assert_eq!(response.headers().get(SEC_WEBSOCKET_PROTOCOL).unwrap(), "kallisti");

        let (origin, protocols, mut server) = server.await.unwrap();
        assert_eq!(origin, "http://app.local");
        assert_eq!(protocols, "kallisti, kallisti.session.tok");

        // Kallisti's own API passes both ways.
        client.send(Message::Text("{\"type\":\"hello\"}".into())).await.unwrap();
        assert_eq!(server.next().await.unwrap().unwrap(), Message::Text("{\"type\":\"hello\"}".into()));
        server.send(Message::Text("{\"type\":\"ready\"}".into())).await.unwrap();
        assert_eq!(client.next().await.unwrap().unwrap(), Message::Text("{\"type\":\"ready\"}".into()));

        // The Mumble stream too, in whole frames; the UDP keys make the
        // bridge ping the server's Mumble port.
        let text_frame = frames::make_frame(11, &[1, 2, 3]);
        client.send(Message::Binary(text_frame.clone().into())).await.unwrap();
        assert_eq!(server.next().await.unwrap().unwrap(), Message::Binary(text_frame.into()));
        let setup = crypt_setup();
        server.send(Message::Binary(setup.clone().into())).await.unwrap();
        assert_eq!(client.next().await.unwrap().unwrap(), Message::Binary(setup.into()));
        let mut buf = [0u8; 64];
        let n = tokio::time::timeout(std::time::Duration::from_secs(3), udp.recv(&mut buf)).await.expect("no UDP ping").unwrap();
        assert!(n > 4); // an encrypted ping
    }

    #[test]
    fn needs_the_right_secret() {
        assert!(parse_target("host=a.example", "s3cret").is_err());
        assert!(parse_target("secret=wrong&host=a.example", "s3cret").is_err());
    }

    #[test]
    fn reads_host_and_port() {
        let mumble = |host: &str, port| Ok(Target::Mumble { host: host.into(), port });
        assert_eq!(parse_target("secret=s3cret&host=a.example&port=1234", "s3cret"), mumble("a.example", 1234));
        assert_eq!(parse_target("secret=s3cret&host=a.example", "s3cret"), mumble("a.example", 64738));
    }

    #[test]
    fn reads_a_kallisti_server() {
        let target = parse_target("secret=s3cret&kallisti=wss%3A%2F%2Fchat.example%2Fws&udp=64738", "s3cret").unwrap();
        assert_eq!(target, Target::Kallisti { url: "wss://chat.example/ws".into(), udp: Some(64738) });
        let target = parse_target("secret=s3cret&kallisti=ws%3A%2F%2Flocalhost%3A8080%2Fws", "s3cret").unwrap();
        assert_eq!(target, Target::Kallisti { url: "ws://localhost:8080/ws".into(), udp: None });
    }

    #[test]
    fn refuses_bad_targets() {
        assert!(parse_target("secret=s3cret", "s3cret").is_err());
        assert!(parse_target("secret=s3cret&host=a.example&port=99999", "s3cret").is_err());
        assert!(parse_target("secret=s3cret&host=a.example&port=x", "s3cret").is_err());
        assert!(parse_target("secret=s3cret&kallisti=https%3A%2F%2Fchat.example%2Fws", "s3cret").is_err());
        assert!(parse_target("secret=s3cret&kallisti=wss%3A%2F%2F", "s3cret").is_err());
        assert!(parse_target("secret=s3cret&kallisti=wss%3A%2F%2Fchat.example%2Fws&udp=0", "s3cret").is_err());
    }
}
