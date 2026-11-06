package mumbled

import (
	"testing"
)

func TestQualityLimitsValidation(t *testing.T) {
	d := DefaultQualityLimits()
	if d.MaxBitrateVoice != DefaultMaxBitrateVoice || d.BytesPerSecond != DefaultStreamBytesPerSecond {
		t.Fatalf("unexpected defaults: %+v", d)
	}

	// Valid update
	valid := QualityLimits{
		MaxBitrateVoice: 96_000,
		MaxBitrateMusic: 256_000,
		BytesPerSecond:  2_000_000,
		MaxSide:         1920,
		MaxFps:          30,
	}
	if err := validateQualityLimits(valid); err != nil {
		t.Fatalf("unexpected error for valid limits: %v", err)
	}

	// Invalid voice bitrate
	invalidVoice := valid
	invalidVoice.MaxBitrateVoice = 5_000
	if err := validateQualityLimits(invalidVoice); err == nil {
		t.Fatalf("expected error for too low voice bitrate")
	}

	invalidFps := valid
	invalidFps.MaxFps = 120
	if err := validateQualityLimits(invalidFps); err == nil {
		t.Fatalf("expected error for too high fps")
	}
}

func TestQualityLimitsServerAndDB(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")

	initial, err := db.QualityLimits()
	if err != nil {
		t.Fatal(err)
	}
	if initial.MaxBitrateVoice != DefaultMaxBitrateVoice {
		t.Fatalf("expected default voice bitrate, got %d", initial.MaxBitrateVoice)
	}

	var changed QualityLimits
	s, addr := startServerAndListen(t, "127.0.0.1", Config{
		DB:       db,
		RootName: "Root",
		OnQualityLimitsChanged: func(q QualityLimits) {
			changed = q
		},
	}, nil)

	su := superUser(t, addr)
	alice, _ := registered(t, addr, "alice")

	newLimits := QualityLimits{
		MaxBitrateVoice: 64_000,
		MaxBitrateMusic: 192_000,
		BytesPerSecond:  1_500_000,
		MaxSide:         1080,
		MaxFps:          30,
	}

	// Non-admin cannot change limits
	if _, err := s.SetQualityLimits(alice.session, newLimits); err == nil {
		t.Fatalf("expected error for non-admin SetQualityLimits")
	}

	// Admin changes limits
	saved, err := s.SetQualityLimits(su.session, newLimits)
	if err != nil {
		t.Fatalf("admin SetQualityLimits failed: %v", err)
	}
	if saved != newLimits || s.QualityLimits() != newLimits {
		t.Fatalf("limits mismatch: saved=%+v, current=%+v", saved, s.QualityLimits())
	}
	if changed != newLimits {
		t.Fatalf("callback not invoked with new limits: %+v", changed)
	}

	// Reopen DB to verify persistence
	fromDB, err := db.QualityLimits()
	if err != nil {
		t.Fatal(err)
	}
	if fromDB != newLimits {
		t.Fatalf("persisted limits mismatch: got %+v, want %+v", fromDB, newLimits)
	}
}
