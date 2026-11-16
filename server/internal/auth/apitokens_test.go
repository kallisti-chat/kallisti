package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func call2(t *testing.T, srv *httptest.Server, method, path string, body any, c *http.Cookie, bearer string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example")
	if c != nil {
		req.AddCookie(c)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

// Tokens are made, listed and revoked with a session; a token reads as its
// account; ending the logins ends the tokens.
func TestAPITokens(t *testing.T) {
	a, srv := setup(t)
	resp, _ := post(t, srv, "/api/accounts", map[string]string{"name": "erin", "password": "a password"}, nil)
	cookie := sessionCookie(resp)

	code, v := call2(t, srv, "POST", "/api/tokens", map[string]any{"name": "agent", "write": true}, cookie, "")
	token, _ := v["token"].(string)
	if code != http.StatusCreated || len(token) < 40 {
		t.Fatalf("create: %d %v", code, v)
	}
	r := httptest.NewRequest("GET", "/api/agent/channels", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if acc, write, ok := a.APIAccount(r); !ok || acc.Name != "erin" || !write {
		t.Fatalf("token: %v %v %v", acc.Name, write, ok)
	}
	// A token is no session, and makes no tokens.
	if _, ok := a.AccountFor(r); ok {
		t.Fatal("a token counted as a session")
	}
	if code, _ := call2(t, srv, "POST", "/api/tokens", map[string]any{"name": "more"}, nil, token); code != http.StatusUnauthorized {
		t.Fatalf("token made a token: %d", code)
	}
	code, list := call2(t, srv, "GET", "/api/tokens", nil, cookie, "")
	if tokens, _ := list["tokens"].([]any); code != 200 || len(tokens) != 1 {
		t.Fatalf("list: %d %v", code, list)
	}
	id := list["tokens"].([]any)[0].(map[string]any)["id"]
	if code, _ := call2(t, srv, "DELETE", "/api/tokens", map[string]any{"id": id}, cookie, ""); code != http.StatusNoContent {
		t.Fatalf("revoke: %d", code)
	}
	if _, _, ok := a.APIAccount(r); ok {
		t.Fatal("revoked token works")
	}

	_, v = call2(t, srv, "POST", "/api/tokens", map[string]any{"name": "again"}, cookie, "")
	r.Header.Set("Authorization", "Bearer "+v["token"].(string))
	if _, write, ok := a.APIAccount(r); !ok || write {
		t.Fatalf("read token: %v %v", write, ok)
	}
	call2(t, srv, "POST", "/api/logout-all", map[string]any{}, cookie, "")
	if _, _, ok := a.APIAccount(r); ok {
		t.Fatal("a token outlived log out everywhere")
	}
}
