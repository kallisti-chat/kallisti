package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"kallisti.chat/server/internal/auth"
	"kallisti.chat/server/internal/store"
)

func TestNosniffResponses(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	accounts := &auth.Service{Store: db, Now: time.Now, AllowedOrigins: []string{"https://app.example"}}
	mux := http.NewServeMux()
	for _, path := range auth.Paths {
		mux.Handle(path, accounts)
	}
	mux.HandleFunc("/media", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "clip.mp4", time.Time{}, strings.NewReader("0123456789"))
	})
	mux.HandleFunc("/oauth/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusFound)
	})

	for _, tc := range []struct {
		name, method, path string
		status             int
	}{
		{"server info", "GET", "/api/server", http.StatusOK},
		{"unauthenticated", "GET", "/api/me", http.StatusUnauthorized},
		{"invalid content type", "POST", "/api/login", http.StatusUnsupportedMediaType},
		{"preflight", "OPTIONS", "/api/login", http.StatusNoContent},
		{"auth redirect", "GET", "/oauth/redirect", http.StatusFound},
		{"mux redirect", "GET", "/api/qr", http.StatusTemporaryRedirect},
		{"not found", "GET", "/missing", http.StatusNotFound},
		{"media range", "GET", "/media", http.StatusPartialContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("Origin", "https://app.example")
			if tc.path == "/media" {
				r.Header.Set("Range", "bytes=2-5")
			}
			before, after := httptest.NewRecorder(), httptest.NewRecorder()
			mux.ServeHTTP(before, r)
			nosniff(mux).ServeHTTP(after, r)
			if after.Code != tc.status || after.Code != before.Code || after.Body.String() != before.Body.String() {
				t.Fatalf("response changed: before %d %q, after %d %q; want status %d", before.Code, before.Body.String(), after.Code, after.Body.String(), tc.status)
			}
			// Inspect the headers as sent, not later changes to the writer.
			got := after.Result().Header
			if values := got.Values("X-Content-Type-Options"); !reflect.DeepEqual(values, []string{"nosniff"}) {
				t.Fatalf("nosniff header: %v", values)
			}
			want := before.Result().Header
			want.Set("X-Content-Type-Options", "nosniff")
			if !reflect.DeepEqual(got, want) {
				t.Errorf("other headers changed: got %v, want %v", got, want)
			}
		})
	}
}

func TestNosniffWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	srv := httptest.NewServer(nosniff(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("streaming writer lost Flusher")
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept WebSocket: %v", err)
			return
		}
		defer conn.CloseNow()
		kind, data, err := conn.Read(ctx)
		if err == nil {
			err = conn.Write(ctx, kind, data)
		}
		if err != nil {
			t.Errorf("echo WebSocket: %v", err)
		}
	})))
	defer srv.Close()
	conn, resp, err := websocket.Dial(ctx, srv.URL+"/ws", nil)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("upgrade: %d %v", resp.StatusCode, resp.Header)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	kind, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.MessageText || string(data) != "hello" {
		t.Fatalf("echo: %v %q", kind, data)
	}
}
