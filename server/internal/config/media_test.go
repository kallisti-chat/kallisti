package config

import "testing"

func TestExperimentalPrivateRoomMedia(t *testing.T) {
	for _, c := range []struct {
		flag, host       string
		enabled, invalid bool
	}{
		{"", "", false, false}, {"0", "", false, false}, {"1", "", true, false},
		{"", "murmur", false, false}, {"1", "murmur", false, true},
	} {
		got, err := Parse(env(map[string]string{"ALLOWED_ORIGINS": "https://chat.example", "EXPERIMENTAL_PRIVATE_ROOM_MEDIA": c.flag, "MUMBLE_HOST": c.host}))
		if (err != nil) != c.invalid || got.ExperimentalPrivateRoomMedia != c.enabled {
			t.Fatalf("%+v: enabled %v err %v", c, got.ExperimentalPrivateRoomMedia, err)
		}
	}
}
