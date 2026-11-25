package mumbled

import "testing"

func TestRandomImageNamesDefaultsAndStoredOverride(t *testing.T) {
	for _, initial := range []bool{false, true} {
		for _, saved := range []bool{false, true} {
			db := testDB(t)
			s, err := New(Config{DB: db, RandomImageNames: initial})
			if err != nil {
				t.Fatal(err)
			}
			if s.RandomImageNames() != initial {
				t.Fatalf("unsaved setting: got %v, want %v", s.RandomImageNames(), initial)
			}
			s.Close()
			if err := db.setSetting("upload_random_image_names", saved); err != nil {
				t.Fatal(err)
			}
			s, err = New(Config{DB: db, RandomImageNames: initial})
			if err != nil {
				t.Fatal(err)
			}
			if s.RandomImageNames() != saved {
				t.Fatalf("saved setting: got %v, want %v", s.RandomImageNames(), saved)
			}
			s.Close()
		}
	}
}

func TestRandomImageNamesAuthorizationAndPersistence(t *testing.T) {
	db := testDB(t)
	setAdminPassword(db, "admin")
	s, addr := startServerAndListen(t, "127.0.0.1", Config{DB: db}, nil)
	admin := superUser(t, addr)
	alice, _ := registered(t, addr, "alice")
	for _, actor := range []uint32{alice.session, ^uint32(0)} {
		if _, err := s.SetRandomImageNames(actor, true); err == nil {
			t.Fatal("non-admin changed image filename policy")
		}
	}
	if s.RandomImageNames() {
		t.Fatal("unauthorized update changed policy")
	}
	for _, enabled := range []bool{true, false} {
		if got, err := s.SetRandomImageNames(admin.session, enabled); err != nil || got != enabled {
			t.Fatalf("save %v: got %v, err %v", enabled, got, err)
		}
		if s.RandomImageNames() != enabled {
			t.Fatal("live policy did not update")
		}
		restarted, err := New(Config{DB: db, RandomImageNames: !enabled})
		if err != nil {
			t.Fatal(err)
		}
		if restarted.RandomImageNames() != enabled {
			t.Fatal("saved policy did not survive restart")
		}
		restarted.Close()
	}
	if _, err := db.db.Exec(`CREATE TRIGGER fail_image_policy BEFORE INSERT ON mumble_settings BEGIN SELECT RAISE(ABORT, 'test save failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetRandomImageNames(admin.session, true); err == nil {
		t.Fatal("expected save failure")
	}
	if s.RandomImageNames() {
		t.Fatal("failed save changed live policy")
	}
	var stored bool
	if ok, err := db.setting("upload_random_image_names", &stored); err != nil || !ok || stored {
		t.Fatalf("failed save changed stored policy: %v, %v, %v", ok, stored, err)
	}
}
