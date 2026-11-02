package names

import "testing"

func TestKey(t *testing.T) {
	cases := map[string]string{
		"  Bob ": "bob",
		"Émile":  "émile",
		"Émile": "émile", // decomposed é becomes the composed form
		"ÉMILE":  "émile",
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValid(t *testing.T) {
	for _, ok := range []string{"alice", "Émile", "bob_2", "x-y.z", "[clan]name", "x", "λ"} {
		if msg := Check(ok); msg != "" {
			t.Errorf("Check(%q) = %q, want ok", ok, msg)
		}
	}
	// Murmur's default username rule has no spaces, emoji or most punctuation.
	for _, bad := range []string{"", " ", "two words", "😀😀", "semi;colon", "superuser", "bad\nname", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "a@b", "alice@chat.example.org"} {
		if msg := Check(bad); msg == "" {
			t.Errorf("Check(%q) accepted", bad)
		}
	}
}

// Accounts from other servers: their name there and their home's host.
func TestRemote(t *testing.T) {
	for _, ok := range []string{"alice@chat.example.org", "Émile_2@social.example", "x.y-z@127.0.0.1:18799", "x@social.example", "lain@xn--bcher-kva.example"} {
		if msg := CheckRemote(ok); msg != "" {
			t.Errorf("CheckRemote(%q) = %q, want ok", ok, msg)
		}
	}
	for _, bad := range []string{"alice", "@alice@x.org", "alice@", "a b@x.org", "alice@x org", "alice@b@x.org", "alice@x.org/path", "[x]@x.org", "@x.org", "superuser@x.org."} {
		if msg := CheckRemote(bad); msg == "" {
			t.Errorf("CheckRemote(%q) accepted", bad)
		}
	}
	if user, host := Split("alice@chat.example.org"); user != "alice" || host != "chat.example.org" {
		t.Errorf("Split: %q %q", user, host)
	}
	if user, host := Split("alice"); user != "alice" || host != "" {
		t.Errorf("Split local: %q %q", user, host)
	}
}
