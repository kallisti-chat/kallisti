package sidecar

// QualityLimits defines server-wide voice and stream quality limits for clients.
type QualityLimits struct {
	MaxBitrateVoice int `json:"maxBitrateVoice"` // in bps (speech)
	MaxBitrateMusic int `json:"maxBitrateMusic"` // in bps (music mode)
	BytesPerSecond  int `json:"bytesPerSecond"`  // in B/s (stream upload)
	MaxSide         int `json:"maxSide"`         // max stream resolution dimension
	MaxFps          int `json:"maxFps"`          // max stream framerate
}

func DefaultQualityLimits() QualityLimits {
	return QualityLimits{
		MaxBitrateVoice: 128_000,
		MaxBitrateMusic: 320_000,
		BytesPerSecond:  1 << 20,
		MaxSide:         4096,
		MaxFps:          60,
	}
}
