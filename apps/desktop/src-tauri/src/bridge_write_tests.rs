use super::*;
use tokio_tungstenite::{tungstenite::protocol::Role, WebSocketStream};

async fn pair(capacity: usize) -> (KallistiSocket, KallistiSocket) {
    let (a, b) = tokio::io::duplex(capacity);
    (
        WebSocketStream::from_raw_socket(Box::new(a) as Box<dyn Io>, Role::Client, None).await,
        WebSocketStream::from_raw_socket(Box::new(b) as Box<dyn Io>, Role::Server, None).await,
    )
}

#[tokio::test(start_paused = true)]
async fn stalled_upstream_does_not_retain_relay_after_local_close() {
    for text in [false, true] {
        let (mut client, local) = pair(1024 * 1024).await;
        let (upstream, server) = pair(64).await;
        let (write, read) = upstream.split();
        let relay = tokio::spawn(relay_with_udp(
            local,
            vec![],
            (UpRead::Ws(read), UpWrite::Ws(write), false),
            None::<UdpSocket>,
        ));
        let msg = if text {
            Message::Text("x".repeat(256 * 1024).into())
        } else {
            Message::Binary(frames::make_frame(11, &vec![42; 256 * 1024]).into())
        };
        client.send(msg).await.unwrap();
        // The fake server remains open but never reads. Its 64-byte pipe fills.
        tokio::task::yield_now().await;
        if text {
            drop(client); // Abrupt local EOF also cannot retain the relay.
        } else {
            client.close(None).await.unwrap();
        }
        let result = tokio::time::timeout(std::time::Duration::from_secs(15), relay).await;
        let error = result
            .expect("stalled write retained relay after local close")
            .unwrap()
            .unwrap_err();
        assert_eq!(error.kind(), ErrorKind::TimedOut);
        drop(server);
    }
}

#[tokio::test(start_paused = true)]
async fn large_frames_remain_serialized_in_both_directions_and_close_reason_survives() {
    let (client, local) = pair(64).await;
    let (upstream, server) = pair(64).await;
    let (write, read) = upstream.split();
    let relay = tokio::spawn(relay_with_udp(
        local,
        vec![],
        (UpRead::Ws(read), UpWrite::Ws(write), false),
        None::<UdpSocket>,
    ));
    let from_client = frames::make_frame(11, &vec![17; 256 * 1024]);
    let from_server = frames::make_frame(12, &vec![23; 256 * 1024]);
    let (mut cw, mut cr) = client.split();
    let (mut sw, mut sr) = server.split();
    let exchange = async {
        let send_client = async {
            // Split one Mumble frame across WebSocket messages.
            for piece in from_client.chunks(4096) {
                cw.send(Message::Binary(piece.to_vec().into()))
                    .await
                    .unwrap();
            }
        };
        let send_server = sw.send(Message::Binary(from_server.clone().into()));
        let read_server = async {
            assert_eq!(
                sr.next().await.unwrap().unwrap(),
                Message::Binary(from_client.clone().into())
            );
        };
        let read_client = async {
            assert_eq!(
                cr.next().await.unwrap().unwrap(),
                Message::Binary(from_server.clone().into())
            );
        };
        let (_, sent, _, _) = tokio::join!(send_client, send_server, read_server, read_client);
        sent.unwrap();
    };
    tokio::time::timeout(std::time::Duration::from_secs(5), exchange)
        .await
        .unwrap();
    let reason = CloseFrame {
        code: CloseCode::Policy,
        reason: "logged out".into(),
    };
    let (sent, received) = tokio::join!(sw.send(Message::Close(Some(reason.clone()))), cr.next());
    sent.unwrap();
    assert_eq!(received.unwrap().unwrap(), Message::Close(Some(reason)));
    relay.await.unwrap().unwrap();
}

#[tokio::test(start_paused = true)]
async fn stalled_upstream_close_is_bounded_too() {
    let (upstream, _server) = pair(1).await;
    let (write, _read) = upstream.split();
    let mut write = UpWrite::Ws(write);
    let result = tokio::time::timeout(std::time::Duration::from_secs(11), write.close()).await;
    assert_eq!(
        result
            .expect("close write has no bound")
            .unwrap_err()
            .kind(),
        ErrorKind::TimedOut
    );
}
