package mumbled

import (
	"errors"
	"fmt"
)

// QualityLimits defines server-wide quality and bandwidth caps for voice and streaming.
type QualityLimits struct {
	MaxBitrateVoice int `json:"maxBitrateVoice"` // in bps for speech (e.g. 40000..558000)
	MaxBitrateMusic int `json:"maxBitrateMusic"` // in bps for music mode (e.g. 64000..558000)
	BytesPerSecond  int `json:"bytesPerSecond"`  // upload bytes/s per stream (e.g. 1048576)
	MaxSide         int `json:"maxSide"`         // max stream resolution dimension (e.g. 720..4096)
	MaxFps          int `json:"maxFps"`          // max stream framerate (e.g. 15..60)
}

const (
	DefaultMaxBitrateVoice = 128_000
	DefaultMaxBitrateMusic = 320_000
	DefaultStreamBytesPerSecond = 1 << 20 // 1 MiB/s (~8.38 Mbit/s)
	DefaultStreamMaxSide = 4096
	DefaultStreamMaxFps = 60

	MinVoiceBitrate = 16_000
	MaxVoiceBitrate = 558_000
	MinStreamBytesPerSecond = 100_000
	MaxStreamBytesPerSecond = 10_000_000 // ~80 Mbit/s
	MinStreamSide = 480
	MaxStreamSide = 4096
	MinStreamFps = 15
	MaxStreamFps = 120
)

func DefaultQualityLimits() QualityLimits {
	return QualityLimits{
		MaxBitrateVoice: DefaultMaxBitrateVoice,
		MaxBitrateMusic: DefaultMaxBitrateMusic,
		BytesPerSecond:  DefaultStreamBytesPerSecond,
		MaxSide:         DefaultStreamMaxSide,
		MaxFps:          DefaultStreamMaxFps,
	}
}

func sanitizeQualityLimits(q QualityLimits) QualityLimits {
	d := DefaultQualityLimits()
	if q.MaxBitrateVoice < MinVoiceBitrate || q.MaxBitrateVoice > MaxVoiceBitrate {
		q.MaxBitrateVoice = d.MaxBitrateVoice
	}
	if q.MaxBitrateMusic < MinVoiceBitrate || q.MaxBitrateMusic > MaxVoiceBitrate {
		q.MaxBitrateMusic = d.MaxBitrateMusic
	}
	if q.BytesPerSecond < MinStreamBytesPerSecond || q.BytesPerSecond > MaxStreamBytesPerSecond {
		q.BytesPerSecond = d.BytesPerSecond
	}
	if q.MaxSide < MinStreamSide || q.MaxSide > MaxStreamSide {
		q.MaxSide = d.MaxSide
	}
	if q.MaxFps < MinStreamFps || q.MaxFps > MaxStreamFps {
		q.MaxFps = d.MaxFps
	}
	return q
}

func validateQualityLimits(q QualityLimits) error {
	if q.MaxBitrateVoice < MinVoiceBitrate || q.MaxBitrateVoice > MaxVoiceBitrate {
		return fmt.Errorf("voice bitrate must be between %d and %d bps", MinVoiceBitrate, MaxVoiceBitrate)
	}
	if q.MaxBitrateMusic < MinVoiceBitrate || q.MaxBitrateMusic > MaxVoiceBitrate {
		return fmt.Errorf("music bitrate must be between %d and %d bps", MinVoiceBitrate, MaxVoiceBitrate)
	}
	if q.BytesPerSecond < MinStreamBytesPerSecond || q.BytesPerSecond > MaxStreamBytesPerSecond {
		return fmt.Errorf("stream bandwidth must be between %d and %d bytes/s", MinStreamBytesPerSecond, MaxStreamBytesPerSecond)
	}
	if q.MaxSide < MinStreamSide || q.MaxSide > MaxStreamSide {
		return fmt.Errorf("stream resolution must be between %d and %d", MinStreamSide, MaxStreamSide)
	}
	if q.MaxFps < MinStreamFps || q.MaxFps > MaxStreamFps {
		return fmt.Errorf("stream framerate must be between %d and %d fps", MinStreamFps, MaxStreamFps)
	}
	return nil
}

// QualityLimits returns the current quality limits.
func (s *Server) QualityLimits() QualityLimits {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.QualityLimits
}

// SetQualityLimits updates the server's quality limits (admins only).
func (s *Server) SetQualityLimits(actor uint32, q QualityLimits) (QualityLimits, error) {
	if err := validateQualityLimits(q); err != nil {
		return QualityLimits{}, err
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	u := s.state.Users[actor]
	admin := u != nil && s.isAdmin(u)
	s.mu.Unlock()
	if !admin {
		return QualityLimits{}, errors.New("only admins change this setting")
	}

	if s.cfg.DB != nil {
		if err := s.cfg.DB.SetQualityLimits(q); err != nil {
			return QualityLimits{}, err
		}
	}
	s.mu.Lock()
	s.state.QualityLimits = q
	s.mu.Unlock()

	if s.cfg.OnQualityLimitsChanged != nil {
		s.cfg.OnQualityLimitsChanged(q)
	}
	return q, nil
}
