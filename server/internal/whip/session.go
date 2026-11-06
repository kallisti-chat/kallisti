package whip

import (
	"cmp"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"kallisti.chat/server/internal/stream"
)

// session is one stream from OBS: a WebRTC connection that feeds a stream
// ingest. It ends on DELETE, when the connection fails, or on a timeout.
// When the hub ends the stream (the owner left, moved, or lost Speak), it
// waits a while for the owner's voice session (again, or a new one after a
// browser refresh) and goes on there.
type session struct {
	h           *Handler
	id, account string
	pc          *webrtc.PeerConnection
	feed        *feed
	begun       time.Time
	videoSSRC   atomic.Uint32
	lastPicture atomic.Int64  // the last picture's arrival (see arrival)
	sound       atomic.Bool   // a sound track is read
	pli         chan struct{} // a keyframe is wanted
	orphaned    chan struct{} // the hub ended the stream
	connected   chan struct{}
	connectOnce sync.Once
	done        chan struct{} // closed by end
	endOnce     sync.Once
	closeOnce   sync.Once
	opened      chan struct{} // open returned (close waits for it)
}

const pliEvery = time.Second // keyframe requests while none came

func newSession(h *Handler, id, account string) *session {
	return &session{h: h, id: id, account: account, begun: time.Now(),
		pli: make(chan struct{}, 1), orphaned: make(chan struct{}, 1), connected: make(chan struct{}), done: make(chan struct{}), opened: make(chan struct{})}
}

// open answers the offer.
func (s *session) open(o offer, sdp string, voice uint32) (string, error) {
	defer close(s.opened)
	bps := s.h.BytesPerSecond
	if s.h.Limits != nil {
		bps = s.h.Limits()
	}
	s.feed = newFeed(s.h.Hub, voice, o.audio != nil, bps, stream.IngestHooks{
		Keyframe: func() {
			select {
			case s.pli <- struct{}{}:
			default:
			}
		},
		Ended: func() {
			s.feed.orphan()
			select {
			case s.orphaned <- struct{}{}:
			default:
			}
		},
	})
	s.feed.still, s.feed.warn = s.h.Preview, s.h.Warn
	api, err := s.h.api(o)
	if err != nil {
		return "", err
	}
	if s.pc, err = api.NewPeerConnection(webrtc.Configuration{}); err != nil {
		return "", err
	}
	s.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			s.connectOnce.Do(func() { close(s.connected) })
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			s.end()
		}
	})
	s.pc.OnTrack(func(t *webrtc.TrackRemote, r *webrtc.RTPReceiver) {
		switch strings.ToLower(t.Codec().MimeType) { // the first track of each kind only
		case "video/h264":
			if s.videoSSRC.CompareAndSwap(0, uint32(t.SSRC())) {
				go s.readVideo(t)
				go readReports(r, &s.feed.srPicture)
			}
		case "audio/opus":
			if !s.sound.Swap(true) {
				go s.readSound(t)
				go readReports(r, &s.feed.srSound)
			}
		}
	})
	if err := s.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		return "", err
	}
	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	gathered := webrtc.GatheringCompletePromise(s.pc)
	if err := s.pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	select { // host candidates only: at once
	case <-gathered:
	case <-time.After(gatherTimeout):
		return "", errors.New("ICE gathering timed out")
	}
	return s.pc.LocalDescription().SDP, nil
}

// end asks the session to close (never blocks: hooks call it).
func (s *session) end() { s.endOnce.Do(func() { close(s.done) }) }

func (s *session) close() {
	s.closeOnce.Do(func() {
		s.end()
		<-s.opened
		if s.feed != nil {
			s.feed.stop()
		}
		if s.pc != nil {
			s.pc.Close()
		}
		s.h.take(s.id)
	})
}

func (s *session) run() {
	defer s.close()
	select {
	case <-s.connected:
	case <-s.done:
		return
	case <-time.After(cmp.Or(s.h.ConnectTimeout, connectTimeout)):
		slog.Info("stream from OBS: no connection", "account", s.account)
		return
	}
	noKeyBy := time.Now().Add(cmp.Or(s.h.KeyTimeout, keyTimeout))
	var rejoinBy time.Time
	// rejoin gives the stream the owner's voice session, once it may stream
	// there (after a refresh the new tab is in text only for a moment).
	rejoin := func(now time.Time) bool {
		voice, err := s.h.voiceFor(s.account)
		ok := err == nil
		if ok {
			s.feed.rebind(voice)
			noKeyBy = now.Add(cmp.Or(s.h.KeyTimeout, keyTimeout))
			s.keyframe()
		}
		return ok
	}
	tick := time.NewTicker(pliEvery)
	defer tick.Stop()
	s.keyframe()
	for {
		select {
		case <-s.done:
			slog.Info("stream from OBS ended", "account", s.account)
			return
		case <-s.pli:
			s.keyframe()
		case <-s.orphaned:
			rejoinBy = time.Now().Add(cmp.Or(s.h.RejoinTimeout, rejoinTimeout))
			rejoin(time.Now())
		case now := <-tick.C:
			if s.feed.waiting() {
				if !rejoin(now) && now.After(rejoinBy) {
					slog.Info("stream from OBS: the owner left voice", "account", s.account)
					return
				}
				continue
			}
			if s.feed.started() {
				if time.Duration(s.arrival()-s.lastPicture.Load())*time.Microsecond > cmp.Or(s.h.MediaTimeout, mediaTimeout) {
					slog.Info("stream from OBS: no pictures", "account", s.account)
					return
				}
				continue
			}
			if now.After(noKeyBy) {
				slog.Info("stream from OBS: no keyframe", "account", s.account)
				return
			}
			s.keyframe()
		}
	}
}

// keyframe asks the encoder for one (PLI).
func (s *session) keyframe() {
	if ssrc := s.videoSSRC.Load(); ssrc != 0 {
		s.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
	}
}

// arrival: microseconds since the session began.
func (s *session) arrival() int64 { return time.Since(s.begun).Microseconds() }

// readReports passes a track's sender reports on, until the track ends.
func readReports(r *webrtc.RTPReceiver, to *srClock) {
	for {
		pkts, _, err := r.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range pkts {
			if sr, ok := p.(*rtcp.SenderReport); ok {
				to.report(sr.NTPTime, sr.RTPTime)
			}
		}
	}
}

// rtpReader: a *webrtc.TrackRemote.
type rtpReader interface {
	ReadRTP() (*rtp.Packet, interceptor.Attributes, error)
}

func (s *session) readVideo(t rtpReader) {
	v := newVideoIn()
	for p, ok := s.next(t); ok; p, ok = s.next(t) {
		pics := v.push(p, s.arrival())
		s.feed.srPicture.apply(&v.clock)
		for _, pic := range pics {
			if pic.data != nil { // before the stream starts with it
				s.lastPicture.Store(s.arrival())
			}
			if err := s.feed.picture(pic); err != nil {
				slog.Info("stream from OBS refused", "account", s.account, "err", err)
				s.end()
				return
			}
		}
	}
}

func (s *session) readSound(t rtpReader) {
	for p, ok := s.next(t); ok; p, ok = s.next(t) {
		s.feed.sound(p, s.arrival())
	}
}

// next reads a track's next packet. Bad ones (an unknown payload type) are
// skipped; at the track's end the session ends (it would freeze).
func (s *session) next(t rtpReader) (*rtp.Packet, bool) {
	for {
		p, _, err := t.ReadRTP()
		if err == nil {
			return p, true
		}
		if errors.Is(err, io.EOF) {
			s.end()
			return nil, false
		}
	}
}
