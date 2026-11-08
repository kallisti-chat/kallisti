use super::*;
use tokio::sync::mpsc;
use tokio_tungstenite::{tungstenite::protocol::Role, WebSocketStream};

struct FakeUdp {
    incoming: tokio::sync::Mutex<mpsc::UnboundedReceiver<std::io::Result<Vec<u8>>>>,
    sent: mpsc::UnboundedSender<Vec<u8>>,
}

impl Datagram for FakeUdp {
    async fn recv(&self, buf: &mut [u8]) -> std::io::Result<usize> {
        let packet = self.incoming.lock().await.recv().await.unwrap()?;
        buf[..packet.len()].copy_from_slice(&packet);
        Ok(packet.len())
    }
    async fn send(&self, packet: &[u8]) -> std::io::Result<usize> {
        self.sent.send(packet.to_vec()).unwrap();
        Ok(packet.len())
    }
}

async fn pair() -> (KallistiSocket, KallistiSocket) {
    let (a, b) = tokio::io::duplex(1024 * 1024);
    (
        WebSocketStream::from_raw_socket(Box::new(a) as Box<dyn Io>, Role::Client, None).await,
        WebSocketStream::from_raw_socket(Box::new(b) as Box<dyn Io>, Role::Server, None).await,
    )
}

async fn receive_failure(kind: ErrorKind, before_tick: bool) {
    let (mut client, local) = pair().await;
    let (upstream, mut server) = pair().await;
    let (write, read) = upstream.split();
    let (inject, incoming) = mpsc::unbounded_channel();
    let (sent, mut packets) = mpsc::unbounded_channel();
    let udp = FakeUdp {
        incoming: tokio::sync::Mutex::new(incoming),
        sent,
    };
    let relay = tokio::spawn(relay_with_udp(
        local,
        vec![],
        (UpRead::Ws(read), UpWrite::Ws(write), true),
        Some(udp),
    ));
    let mut body = Vec::new();
    for (tag, byte) in [(0x0a, 1u8), (0x12, 2), (0x1a, 3)] {
        body.extend_from_slice(&[tag, 16]);
        body.extend_from_slice(&[byte; 16]);
    }
    let setup = frames::make_frame(CRYPT_SETUP, &body);
    server
        .send(Message::Binary(setup.clone().into()))
        .await
        .unwrap();
    assert_eq!(
        client.next().await.unwrap().unwrap(),
        Message::Binary(setup.into())
    );
    let mut crypt = crate::crypt::CryptState::new([1; 16], [3; 16], [2; 16]);
    let ping = crypt.decrypt(&packets.recv().await.unwrap()).unwrap();
    inject.send(Ok(crypt.encrypt(&ping))).unwrap();
    // The announce proves that a pong made UDP active. No local speech.
    loop {
        let plain = crypt.decrypt(&packets.recv().await.unwrap()).unwrap();
        if plain.first() == Some(&0) {
            break;
        }
    }
    // Let a ping tick record the usable state before injecting the error.
    if !before_tick {
        tokio::time::advance(udp::PING_INTERVAL).await;
        tokio::task::yield_now().await;
    }
    inject.send(Err(std::io::Error::from(kind))).unwrap();
    tokio::task::yield_now().await;
    let limit = if kind == ErrorKind::PermissionDenied {
        1
    } else {
        20
    };
    let fallback = tokio::time::timeout(std::time::Duration::from_secs(limit), server.next()).await;
    assert_eq!(
        fallback
            .expect("UDP receive error stopped TCP fallback")
            .unwrap()
            .unwrap(),
        Message::Binary(frames::make_frame(UDP_TUNNEL, &[1, 8, 0]).into())
    );
    assert!(!relay.is_finished(), "UDP failure must not close TCP");
    server
        .send(Message::Text("still connected".into()))
        .await
        .unwrap();
    assert_eq!(
        client.next().await.unwrap().unwrap(),
        Message::Text("still connected".into())
    );
    let speech = frames::make_frame(UDP_TUNNEL, &[0, 42]);
    client
        .send(Message::Binary(speech.clone().into()))
        .await
        .unwrap();
    assert_eq!(
        server.next().await.unwrap().unwrap(),
        Message::Binary(speech.into())
    );
    client.send(Message::Text("reply".into())).await.unwrap();
    assert_eq!(
        server.next().await.unwrap().unwrap(),
        Message::Text("reply".into())
    );
    if kind != ErrorKind::PermissionDenied {
        // Expected ICMP errors must leave receive/recovery working, not
        // take the fatal-error path and abandon the socket.
        inject.send(Ok(crypt.encrypt(&[1, 8, 2]))).unwrap();
        tokio::time::timeout(std::time::Duration::from_secs(1), async {
            loop {
                let plain = crypt.decrypt(&packets.recv().await.unwrap()).unwrap();
                if plain.first() == Some(&0) {
                    break;
                }
            }
        })
        .await
        .expect("ICMP error disabled UDP recovery");
        inject.send(Ok(crypt.encrypt(&[0, 7, 9]))).unwrap();
        let audio = tokio::time::timeout(std::time::Duration::from_secs(1), client.next())
            .await
            .unwrap();
        assert_eq!(
            audio.unwrap().unwrap(),
            Message::Binary(frames::make_frame(UDP_TUNNEL, &[0, 7, 9]).into())
        );
    }
    relay.abort();
}

#[tokio::test(start_paused = true)]
async fn fatal_receive_error_falls_back_without_speech() {
    receive_failure(ErrorKind::PermissionDenied, false).await;
    receive_failure(ErrorKind::PermissionDenied, true).await;
}

#[tokio::test(start_paused = true)]
async fn expected_icmp_errors_still_allow_fallback() {
    for kind in [ErrorKind::ConnectionRefused, ErrorKind::ConnectionReset] {
        receive_failure(kind, false).await;
    }
}
