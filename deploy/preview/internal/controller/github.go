package controller

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const marker = "<!-- kallisti-preview-controller -->"

type GitHub struct {
	Client     *http.Client
	TokenFile  string
	Token      string
	Base       string
	WorkflowID int64
	BotLogin   string
}

func (g *GitHub) request(ctx context.Context, method, path string, body any, out any) error {
	var data io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		data = bytes.NewReader(b)
	}
	base := g.Base
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, data)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	res, err := g.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("github %s %s: %d", method, path, res.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}
func repoPath(path string) string { return "/repos/" + Repository + path }
func (g *GitHub) Pulls(ctx context.Context) (map[int]PR, error) {
	out := map[int]PR{}
	for page := 1; page <= 100; page++ {
		var batch []struct {
			Number int
			Base   struct{ Ref string }
			Head   struct {
				SHA  string
				Ref  string
				Repo struct {
					FullName string `json:"full_name"`
				}
			}
		}
		if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/pulls?state=open&per_page=100&page=%d", page)), nil, &batch); err != nil {
			return nil, err
		}
		for _, p := range batch {
			if p.Base.Ref == "main" && shaPattern.MatchString(p.Head.SHA) {
				out[p.Number] = PR{Number: p.Number, SHA: p.Head.SHA, SameRepo: p.Head.Repo.FullName == Repository, HeadRepository: p.Head.Repo.FullName, HeadRef: p.Head.Ref}
			}
		}
		if len(batch) < 100 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("pull pagination limit exceeded")
}

type workflowRun struct {
	ID             int64  `json:"id"`
	WorkflowID     int64  `json:"workflow_id"`
	RunAttempt     int    `json:"run_attempt"`
	Event          string `json:"event"`
	Conclusion     string `json:"conclusion"`
	Path           string `json:"path"`
	HeadSHA        string `json:"head_sha"`
	HeadBranch     string `json:"head_branch"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
	PullRequests []struct {
		Number int `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_requests"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (g *GitHub) Evidence(ctx context.Context, prs map[int]PR) (map[int]Evidence, error) {
	out := map[int]Evidence{}
	// only runs from the configured, installed default-branch workflow are trusted.
	var workflow struct {
		ID    int64
		Path  string
		State string
	}
	if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/actions/workflows/%d", g.WorkflowID)), nil, &workflow); err != nil {
		return nil, err
	}
	if workflow.ID != g.WorkflowID || workflow.Path != Workflow || workflow.State != "active" {
		return nil, fmt.Errorf("unexpected preview workflow")
	}
	for page := 1; page <= 10; page++ {
		var batch struct {
			Runs []workflowRun `json:"workflow_runs"`
		}
		if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/actions/workflows/%d/runs?status=success&created=%s&per_page=100&page=%d", g.WorkflowID, url.QueryEscape(">="+time.Now().UTC().Add(-96*time.Hour).Format(time.RFC3339)), page)), nil, &batch); err != nil {
			return nil, err
		}
		for _, run := range batch.Runs {
			if run.WorkflowID != g.WorkflowID || run.Path != Workflow || run.Repository.FullName != Repository || run.Conclusion != "success" || (run.Event != "workflow_run" && run.Event != "issue_comment") || !shaPattern.MatchString(run.HeadSHA) {
				continue
			}
			var artifacts struct {
				Artifacts []struct {
					ID      int64
					Name    string
					Expired bool
					Size    int64 `json:"size_in_bytes"`
				}
			}
			if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/actions/runs/%d/artifacts?per_page=100", run.ID)), nil, &artifacts); err != nil {
				return nil, err
			}
			for _, a := range artifacts.Artifacts {
				if a.ID <= 0 || a.Name != fmt.Sprintf("preview-metadata-%d", run.RunAttempt) || a.Expired || a.Size <= 0 || a.Size > 65536 {
					continue
				}
				ev, err := g.artifact(ctx, a.ID)
				if err != nil {
					return nil, err
				}
				p, open := prs[ev.PR]
				if !open || ev.Validate(p) != nil || ev.RunID != run.ID || ev.Attempt != run.RunAttempt {
					continue
				}
				if current, ok := out[ev.PR]; ok && current.ApprovalID >= ev.ApprovalID {
					continue
				}
				// publishers must originate from trusted main history.
				var comparison struct{ Status string }
				if err = g.request(ctx, "GET", repoPath("/compare/"+run.HeadSHA+"...main"), nil, &comparison); err != nil {
					return nil, err
				}
				if comparison.Status != "ahead" && comparison.Status != "identical" {
					continue
				}
				if err = g.sourceBuild(ctx, p, ev); err != nil {
					continue
				}
				if run.Event == "issue_comment" && ev.ApprovalID == 0 {
					continue
				}
				if ev.ApprovalID > 0 {
					if err = g.approval(ctx, ev); err != nil {
						continue
					}
				}
				green, err := g.Green(ctx, p)
				if err != nil {
					return nil, err
				}
				if green {
					out[ev.PR] = ev
				}
			}
		}
		if len(batch.Runs) < 100 {
			break
		}
	}
	return out, nil
}

// sourceBuild verifies the untrusted artifact's GitHub provenance independently of the publisher metadata.
func (g *GitHub) sourceBuild(ctx context.Context, p PR, e Evidence) error {
	var run workflowRun
	if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/actions/runs/%d", e.BuildRunID)), nil, &run); err != nil {
		return err
	}
	if run.ID != e.BuildRunID || run.RunAttempt != e.BuildAttempt || run.WorkflowID != e.BuildWorkflowID || run.Event != "pull_request" || run.Conclusion != "success" || run.Path != BuildWorkflow || run.Repository.FullName != Repository || run.HeadSHA != p.SHA || p.HeadRepository == "" || p.HeadRef == "" || run.HeadRepository.FullName != p.HeadRepository || run.HeadBranch != p.HeadRef {
		return fmt.Errorf("source build identity mismatch")
	}
	for _, association := range run.PullRequests {
		if association.Number != p.Number || association.Head.SHA != p.SHA || association.Base.Ref != "main" {
			return fmt.Errorf("source build PR association mismatch")
		}
	}
	var workflow struct {
		ID          int64
		Path, State string
	}
	if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/actions/workflows/%d", e.BuildWorkflowID)), nil, &workflow); err != nil {
		return err
	}
	if workflow.ID != e.BuildWorkflowID || workflow.Path != BuildWorkflow || workflow.State != "active" {
		return fmt.Errorf("unexpected source build workflow")
	}
	var artifact struct {
		ID          int64
		Name        string
		Expired     bool
		WorkflowRun struct {
			ID      int64
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_run"`
	}
	if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/actions/artifacts/%d", e.BuildArtifactID)), nil, &artifact); err != nil {
		return err
	}
	if artifact.ID != e.BuildArtifactID || artifact.Name != fmt.Sprintf("preview-image-%d", e.BuildAttempt) || artifact.Expired || artifact.WorkflowRun.ID != e.BuildRunID || artifact.WorkflowRun.HeadSHA != p.SHA {
		return fmt.Errorf("source build artifact mismatch")
	}
	return nil
}

func (g *GitHub) artifact(ctx context.Context, id int64) (Evidence, error) {
	var ev Evidence
	base := g.Base
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+repoPath(fmt.Sprintf("/actions/artifacts/%d/zip", id)), nil)
	if err != nil {
		return ev, err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	res, err := g.Client.Do(req)
	if err != nil {
		return ev, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		return ev, fmt.Errorf("artifact HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil {
		return ev, err
	}
	if len(b) > 65536 {
		return ev, fmt.Errorf("artifact too large")
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return ev, err
	}
	if len(z.File) != 1 || z.File[0].Name != "metadata.json" || z.File[0].UncompressedSize64 > 8192 {
		return ev, fmt.Errorf("unexpected metadata archive")
	}
	r, err := z.File[0].Open()
	if err != nil {
		return ev, err
	}
	defer func() { _ = r.Close() }()
	dec := json.NewDecoder(io.LimitReader(r, 8193))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&ev); err != nil {
		return ev, err
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return ev, fmt.Errorf("metadata must contain exactly one JSON object")
	}
	return ev, nil
}
func (g *GitHub) approval(ctx context.Context, e Evidence) error {
	var c struct {
		ID       int64
		Body     string
		IssueURL string `json:"issue_url"`
		Created  string `json:"created_at"`
		Updated  string `json:"updated_at"`
		User     struct{ Login string }
	}
	if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/issues/comments/%d", e.ApprovalID)), nil, &c); err != nil {
		return err
	}
	if e.Approver != "cortexobscura" && e.Approver != "lambadalambda" {
		return fmt.Errorf("untrusted approver")
	}
	if c.Created == "" || c.Created != c.Updated {
		return fmt.Errorf("edited approval requires a fresh comment")
	}
	if c.ID != e.ApprovalID || c.User.Login != e.Approver || c.IssueURL != "https://api.github.com"+repoPath(fmt.Sprintf("/issues/%d", e.PR)) {
		return fmt.Errorf("approval identity mismatch")
	}
	body := strings.TrimSpace(c.Body)
	if body != "/preview "+e.SHA {
		return fmt.Errorf("approval withdrawn")
	}
	return nil
}
func (g *GitHub) Comment(ctx context.Context, n int, r *Record, body string) error {
	body = marker + "\n" + body
	if r.CommentID == 0 {
		for page := 1; page <= 100; page++ {
			var cs []struct {
				ID   int64
				Body string
				User struct{ Login string }
			}
			if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/issues/%d/comments?per_page=100&page=%d", n, page)), nil, &cs); err != nil {
				return err
			}
			for _, c := range cs {
				if c.User.Login == g.BotLogin && strings.HasPrefix(c.Body, marker) {
					r.CommentID = c.ID
					r.CommentBody = c.Body
					break
				}
			}
			if r.CommentID != 0 || len(cs) < 100 {
				break
			}
			if page == 100 {
				return fmt.Errorf("comment pagination limit exceeded")
			}
		}
	}
	if r.CommentID != 0 && r.CommentBody == body {
		return nil
	}
	method, path := "POST", fmt.Sprintf("/issues/%d/comments", n)
	if r.CommentID != 0 {
		method = "PATCH"
		path = fmt.Sprintf("/issues/comments/%d", r.CommentID)
	}
	var response struct{ ID int64 }
	if err := g.request(ctx, method, repoPath(path), map[string]string{"body": body}, &response); err != nil {
		return err
	}
	r.CommentID = response.ID
	r.CommentBody = body
	return nil
}
func NewGitHub(token string, id int64, bot string) *GitHub {
	return &GitHub{Token: token, WorkflowID: id, BotLogin: bot, Client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return fmt.Errorf("too many redirects")
		}
		u, err := url.Parse("https://api.github.com")
		if err != nil {
			return err
		}
		if req.URL.Host != u.Host {
			req.Header.Del("Authorization")
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("insecure redirect")
		}
		return nil
	}}}
}

// green requires the original merge-ref suites as well as every reported check.
// the preview workflow independently repeats tests against the exact head image.
func (g *GitHub) Green(ctx context.Context, p PR) (bool, error) {
	for _, path := range []string{"test.yml", "voice.yml", "helm.yml"} {
		var wf struct {
			ID    int64
			Path  string
			State string
		}
		if err := g.request(ctx, "GET", repoPath("/actions/workflows/"+path), nil, &wf); err != nil {
			return false, err
		}
		if wf.ID <= 0 || wf.Path != ".github/workflows/"+path || wf.State != "active" {
			return false, nil
		}
		var batch struct {
			Runs []workflowRun `json:"workflow_runs"`
		}
		if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/actions/workflows/%d/runs?head_sha=%s&event=pull_request&per_page=100", wf.ID, p.SHA)), nil, &batch); err != nil {
			return false, err
		}
		if len(batch.Runs) == 0 {
			return false, nil
		}
		r := batch.Runs[0]
		// github can omit pull_requests for fork runs; reject conflicting links
		// when present. the trusted metadata separately binds the exact PR head.
		associated := len(r.PullRequests) == 0
		for _, ref := range r.PullRequests {
			if ref.Number == p.Number && ref.Head.SHA == p.SHA && ref.Base.Ref == "main" {
				associated = true
			}
		}
		if !associated || r.WorkflowID != wf.ID || r.HeadSHA != p.SHA || r.Event != "pull_request" || r.Conclusion != "success" || r.Repository.FullName != Repository {
			return false, nil
		}
	}
	found := false
	for page := 1; page <= 100; page++ {
		var batch struct {
			Checks []struct {
				Status     string
				Conclusion string
			} `json:"check_runs"`
		}
		if err := g.request(ctx, "GET", repoPath(fmt.Sprintf("/commits/%s/check-runs?filter=latest&per_page=100&page=%d", p.SHA, page)), nil, &batch); err != nil {
			return false, err
		}
		for _, c := range batch.Checks {
			found = true
			if c.Status != "completed" || (c.Conclusion != "success" && c.Conclusion != "skipped") {
				return false, nil
			}
		}
		if len(batch.Checks) < 100 {
			break
		}
		if page == 100 {
			return false, fmt.Errorf("check pagination limit exceeded")
		}
	}
	var status struct {
		State string
		Total int `json:"total_count"`
	}
	if err := g.request(ctx, "GET", repoPath("/commits/"+p.SHA+"/status"), nil, &status); err != nil {
		return false, err
	}
	return found && (status.Total == 0 || status.State == "success"), nil
}
