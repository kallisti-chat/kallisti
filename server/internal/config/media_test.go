package config

import (
	"slices"
	"testing"
)

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

// Translation models: our languages by default, a list, all, or none.
func TestTranslationLanguages(t *testing.T) {
	for _, c := range []struct {
		set, langs, off, dir string
		want                 []string
	}{
		{want: []string{"de", "es", "fr", "ja", "zh", "zh-Hant"}},
		{langs: "zh, ko", want: []string{"zh", "ko"}},
		{langs: "*", want: []string{"*"}},
		{off: "0", want: nil},
	} {
		got, err := Parse(env(map[string]string{"ALLOWED_ORIGINS": "https://chat.example", "DB_PATH": "/data/k.db", "TRANSLATION_LANGUAGES": c.langs, "TRANSLATION": c.off}))
		if err != nil || !slices.Equal(got.TranslationLanguages, c.want) || got.TranslationDir != "/data/translation-models" {
			t.Fatalf("%+v: %v %q %v", c, got.TranslationLanguages, got.TranslationDir, err)
		}
	}
}

// The larger translation model: offered unless TRANSLATION_LLM=0.
func TestTranslationLLM(t *testing.T) {
	for v, want := range map[string]bool{"": false, "1": false, "0": true} {
		got, err := Parse(env(map[string]string{"ALLOWED_ORIGINS": "https://chat.example", "TRANSLATION_LLM": v}))
		if err != nil || got.NoTranslationLLM != want {
			t.Fatalf("%q: %v %v", v, got.NoTranslationLLM, err)
		}
	}
}
