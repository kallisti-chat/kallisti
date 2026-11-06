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

// Without signed links: only with the experiment.
func TestPrivateRoomMediaWithoutSignedLinks(t *testing.T) {
	for _, c := range []struct {
		flag, unsigned string
		on, invalid    bool
	}{
		{"1", "", false, false}, {"1", "1", true, false}, {"", "1", false, true}, {"0", "1", false, true},
	} {
		got, err := Parse(env(map[string]string{"ALLOWED_ORIGINS": "https://chat.example", "EXPERIMENTAL_PRIVATE_ROOM_MEDIA": c.flag, "PRIVATE_ROOM_MEDIA_NO_SIGNED_LINKS": c.unsigned}))
		if (err != nil) != c.invalid || got.PrivateRoomMediaNoSignedLinks != c.on {
			t.Fatalf("%+v: on %v err %v", c, got.PrivateRoomMediaNoSignedLinks, err)
		}
	}
}
