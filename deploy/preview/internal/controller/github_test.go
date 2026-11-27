package controller

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func archive(t *testing.T, e Evidence) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(w).Encode(e); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func githubFixture(t *testing.T, mutate func(*Evidence), extraCheck string, omitAssociations ...bool) (*GitHub, map[int]PR) {
	t.Helper()
	ev := evidenceFor(31, "a")
	if mutate != nil {
		mutate(&ev)
	}
	data := archive(t, ev)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, repoPath(""))
		out := any(nil)
		switch {
		case p == "/actions/workflows/99":
			out = map[string]any{"id": 99, "path": Workflow, "state": "active"}
		case p == "/actions/workflows/99/runs":
			out = map[string]any{"workflow_runs": []any{map[string]any{"id": 100, "workflow_id": 99, "run_attempt": 1, "event": "pull_request_target", "conclusion": "success", "path": Workflow, "head_sha": strings.Repeat("b", 40), "repository": map[string]string{"full_name": Repository}}}}
		case p == "/actions/runs/100/artifacts":
			out = map[string]any{"artifacts": []any{map[string]any{"id": 1, "name": "preview-metadata-1", "size_in_bytes": len(data)}}}
		case p == "/actions/artifacts/1/zip":
			_, _ = w.Write(data)
			return
		case strings.HasPrefix(p, "/compare/"):
			out = map[string]string{"status": "ahead"}
		case p == "/actions/workflows/test.yml" || p == "/actions/workflows/voice.yml" || p == "/actions/workflows/helm.yml":
			out = map[string]any{"id": 10, "path": ".github/workflows/" + strings.TrimPrefix(p, "/actions/workflows/"), "state": "active"}
		case p == "/actions/workflows/10/runs":
			out = map[string]any{"workflow_runs": []any{map[string]any{"pull_requests": []any{map[string]any{"number": 31, "head": map[string]string{"sha": strings.Repeat("a", 40)}, "base": map[string]string{"ref": "main"}}}, "workflow_id": 10, "head_sha": strings.Repeat("a", 40), "event": "pull_request", "conclusion": "success", "repository": map[string]string{"full_name": Repository}}}}
			if len(omitAssociations) > 0 && omitAssociations[0] {
				for _, run := range out.(map[string]any)["workflow_runs"].([]any) {
					run.(map[string]any)["pull_requests"] = []any{}
				}
			}
		case strings.HasSuffix(p, "/check-runs"):
			out = map[string]any{"check_runs": []any{map[string]string{"status": "completed", "conclusion": extraCheck}}}
		case strings.HasSuffix(p, "/status"):
			out = map[string]any{"state": "pending", "total_count": 0}
		default:
			t.Errorf("unexpected API request %s", p)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	g := NewGitHub("test", 99, "preview-bot")
	g.Base = server.URL
	g.Client = server.Client()
	return g, map[int]PR{31: {Number: 31, SHA: strings.Repeat("a", 40), SameRepo: true}}
}
func TestTrustedArtifactHandoff(t *testing.T) {
	g, p := githubFixture(t, nil, "success")
	ev, err := g.Evidence(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 1 || ev[31].RunID != 100 {
		t.Fatalf("valid provenance rejected: %+v", ev)
	}
}
func TestRejectArtifactReplayAndFailedExtraCheck(t *testing.T) {
	for name, mutate := range map[string]func(*Evidence){"wrong-run": func(e *Evidence) { e.RunID = 101 }, "wrong-attempt": func(e *Evidence) { e.Attempt = 2 }, "wrong-repository": func(e *Evidence) { e.Repository = "attacker/kallisti" }, "wrong-pr": func(e *Evidence) { e.PR = 32 }, "wrong-sha": func(e *Evidence) { e.SHA = strings.Repeat("c", 40) }} {
		t.Run(name, func(t *testing.T) {
			g, p := githubFixture(t, mutate, "success")
			ev, err := g.Evidence(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			if len(ev) != 0 {
				t.Fatal("untrusted metadata accepted")
			}
		})
	}
	g, p := githubFixture(t, nil, "failure")
	ev, err := g.Evidence(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Fatal("failed additional check ignored")
	}
}
func TestCommentRecoveryUpdatesOneOwnedComment(t *testing.T) {
	posts, patches := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = fmt.Fprintf(w, `[{"id":1,"body":%q,"user":{"login":"attacker"}},{"id":2,"body":%q,"user":{"login":"preview-bot"}}]`, marker, marker)
		case "POST":
			posts++
			_, _ = fmt.Fprint(w, `{"id":3}`)
		case "PATCH":
			patches++
			if !strings.HasSuffix(r.URL.Path, "/2") {
				t.Error("edited foreign comment")
			}
			_, _ = fmt.Fprint(w, `{"id":2}`)
		}
	}))
	defer s.Close()
	g := NewGitHub("", 99, "preview-bot")
	g.Base = s.URL
	g.Client = s.Client()
	r := &Record{}
	for range 2 {
		if err := g.Comment(context.Background(), 31, r, "ready"); err != nil {
			t.Fatal(err)
		}
	}
	if posts != 0 || patches != 1 {
		t.Fatalf("posts %d patches %d", posts, patches)
	}
}
func TestEditedApprovalRejected(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"id":7,"body":"/preview","created_at":"2026-01-01","updated_at":"2026-01-02","issue_url":%q,"user":{"login":"cortexobscura"}}`, "https://api.github.com"+repoPath("/issues/31"))
	}))
	defer s.Close()
	g := NewGitHub("", 99, "")
	g.Base = s.URL
	g.Client = s.Client()
	ev := evidenceFor(31, "a")
	ev.ApprovalID = 7
	ev.Approver = "cortexobscura"
	if g.approval(context.Background(), ev) == nil {
		t.Fatal("edited approval accepted")
	}
}

func TestArtifactRejectsTrailingJSON(t *testing.T) {
	for _, suffix := range []string{`{}`, `garbage`} {
		t.Run(suffix, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			w, err := z.Create("metadata.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.NewEncoder(w).Encode(evidenceFor(31, "a")); err != nil {
				t.Fatal(err)
			}
			if _, err = fmt.Fprint(w, suffix); err != nil {
				t.Fatal(err)
			}
			if err = z.Close(); err != nil {
				t.Fatal(err)
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(b.Bytes()) }))
			defer s.Close()
			g := NewGitHub("", 99, "")
			g.Base, g.Client = s.URL, s.Client()
			if _, err = g.artifact(context.Background(), 1); err == nil {
				t.Fatal("trailing metadata accepted")
			}
		})
	}
}

func TestGreenRejectsOtherPullRequest(t *testing.T) {
	g, prs := githubFixture(t, nil, "success")
	p := prs[31]
	p.Number = 32
	green, err := g.Green(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if green {
		t.Fatal("another PR's CI accepted")
	}
}

func TestGreenAllowsGitHubOmittedForkAssociations(t *testing.T) {
	g, prs := githubFixture(t, nil, "success", true)
	p := prs[31]
	p.SameRepo = false
	green, err := g.Green(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !green {
		t.Fatal("GitHub omitted fork associations rejected despite exact SHA and workflow identity")
	}
}
