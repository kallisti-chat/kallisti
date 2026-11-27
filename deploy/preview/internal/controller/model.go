package controller

import (
	"fmt"
	"regexp"
	"sort"
	"time"
)

const Repository = "kallisti-chat/kallisti"
const ImageRepository = "ghcr.io/kallisti-chat/kallisti-preview"
const Workflow = ".github/workflows/preview.yml"

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type PR struct {
	Number   int    `json:"number"`
	SHA      string `json:"sha"`
	SameRepo bool   `json:"same_repo"`
}
type Evidence struct {
	Repository string `json:"repository"`
	PR         int    `json:"pr"`
	SHA        string `json:"sha"`
	RunID      int64  `json:"run_id"`
	Attempt    int    `json:"attempt"`
	Image      string `json:"image"`
	Digest     string `json:"digest"`
	ApprovalID int64  `json:"approval_id"`
	Approver   string `json:"approver"`
}

func (e Evidence) Validate(p PR) error {
	if e.Repository != Repository || e.PR <= 0 || e.PR != p.Number || e.SHA != p.SHA || !shaPattern.MatchString(e.SHA) || e.Image != ImageRepository || !digestPattern.MatchString(e.Digest) || e.RunID <= 0 || e.Attempt < 1 {
		return fmt.Errorf("invalid image provenance for PR %d", p.Number)
	}
	if e.ApprovalID < 0 || (e.ApprovalID > 0 && e.Approver != "cortexobscura" && e.Approver != "lambadalambda") || (!p.SameRepo && e.ApprovalID == 0) {
		return fmt.Errorf("fork lacks SHA-bound approval")
	}
	return nil
}
func (e Evidence) Ticket() string { return fmt.Sprintf("%s:%d", e.SHA, e.ApprovalID) }
func (e Evidence) Revision() string {
	return fmt.Sprintf("preview-%d-%s", e.PR, e.SHA[:min(12, len(e.SHA))])
}

type Revision struct {
	Evidence Evidence  `json:"evidence"`
	Started  time.Time `json:"started"`
}
type Slot struct {
	PR        int       `json:"pr"`
	Approved  bool      `json:"approved"`
	Admitted  time.Time `json:"admitted"`
	Expires   time.Time `json:"expires"`
	Active    *Revision `json:"active,omitempty"`
	Candidate *Revision `json:"candidate,omitempty"`
	Removing  bool      `json:"removing,omitempty"`
	Failed    string    `json:"failed,omitempty"`
}
type Record struct {
	Suppressed  string `json:"suppressed,omitempty"`
	Status      string `json:"status"`
	CommentID   int64  `json:"comment_id,omitempty"`
	CommentBody string `json:"comment_body,omitempty"`
}
type State struct {
	ApprovalBindings map[int64]string `json:"approval_bindings,omitempty"`
	Slots            [3]Slot          `json:"slots"`
	Records          map[int]*Record  `json:"records"`
}

func (s *State) record(n int) *Record {
	if s.Records == nil {
		s.Records = map[int]*Record{}
	}
	if s.Records[n] == nil {
		s.Records[n] = &Record{}
	}
	return s.Records[n]
}
func slotName(i int) string  { return fmt.Sprintf("preview-slot-%d", i+1) }
func hostname(pr int) string { return fmt.Sprintf("pr-%d.kallisti.fans", pr) }

// eligible puts explicit approvals first, then uses pr number for stable fifo.
func eligible(prs map[int]PR, evidence map[int]Evidence) []Evidence {
	out := []Evidence{}
	for n, p := range prs {
		if e, ok := evidence[n]; ok && e.Validate(p) == nil {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.ApprovalID > 0) != (b.ApprovalID > 0) {
			return a.ApprovalID > 0
		}
		if a.ApprovalID > 0 && a.ApprovalID != b.ApprovalID {
			return a.ApprovalID < b.ApprovalID
		}
		return a.PR < b.PR
	})
	return out
}

// validate rejects corrupt persisted intent before any runtime side effects.
func (s *State) validate() error {
	seen := map[int]bool{}
	for i, v := range s.Slots {
		if v.PR == 0 {
			if v.Active != nil || v.Candidate != nil || v.Removing {
				return fmt.Errorf("slot %d has runtime intent without a PR", i+1)
			}
			continue
		}
		if v.PR < 0 || seen[v.PR] || v.Admitted.IsZero() || !v.Expires.After(v.Admitted) {
			return fmt.Errorf("invalid slot %d", i+1)
		}
		seen[v.PR] = true
		for _, r := range []*Revision{v.Active, v.Candidate} {
			if r == nil {
				continue
			}
			if r.Started.IsZero() || r.Evidence.Validate(PR{Number: v.PR, SHA: r.Evidence.SHA, SameRepo: true}) != nil {
				return fmt.Errorf("invalid revision in slot %d", i+1)
			}
		}
		if v.Active != nil && v.Candidate != nil && v.Active.Evidence.Revision() == v.Candidate.Evidence.Revision() {
			return fmt.Errorf("colliding revisions in slot %d", i+1)
		}
	}
	for n, r := range s.Records {
		if n <= 0 || r == nil {
			return fmt.Errorf("invalid PR record")
		}
	}
	return nil
}
