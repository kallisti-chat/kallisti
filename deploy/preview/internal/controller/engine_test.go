package controller

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRuntime struct {
	ready     bool
	prune     bool
	err       error
	published []string
	calls     []string
}

func (f *fakeRuntime) Ensure(_ context.Context, _ int, r Revision) (bool, error) {
	f.calls = append(f.calls, "ensure")
	return f.ready, f.err
}
func (f *fakeRuntime) Publish(_ context.Context, _ int, _ int, r *Revision, _ bool) error {
	f.published = append(f.published, r.Evidence.SHA)
	return f.err
}
func (f *fakeRuntime) Prune(context.Context, int, []string) (bool, error) {
	f.calls = append(f.calls, "prune")
	return f.prune, f.err
}
func (f *fakeRuntime) Withdraw(context.Context, int, int) error {
	f.calls = append(f.calls, "withdraw")
	return f.err
}
func evidenceFor(n int, sha string) Evidence {
	return Evidence{Repository: Repository, PR: n, SHA: strings.Repeat(sha, 40), RunID: 100, Attempt: 1, BuildRunID: 200, BuildAttempt: 1, BuildWorkflowID: 98, BuildArtifactID: 2, Image: ImageRepository, Digest: "sha256:" + strings.Repeat("a", 64)}
}
func fixture(t *testing.T) (*Engine, *fakeRuntime, map[int]PR, map[int]Evidence, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &fakeRuntime{ready: true, prune: true}
	e := &Engine{State: &State{}, Runtime: f, Now: func() time.Time { return now }, Save: func(context.Context, *State) error { return nil }}
	ev := evidenceFor(1, "a")
	return e, f, map[int]PR{1: {Number: 1, SHA: ev.SHA, SameRepo: true}}, map[int]Evidence{1: ev}, &now
}
func tick(t *testing.T, e *Engine, p map[int]PR, v map[int]Evidence) {
	t.Helper()
	if err := e.Tick(context.Background(), p, v); err != nil {
		t.Fatal(err)
	}
}
func TestCandidateFailureKeepsWorkingAndDoesNotRetry(t *testing.T) {
	e, f, p, v, now := fixture(t)
	tick(t, e, p, v)
	tick(t, e, p, v)
	old := e.State.Slots[0].Active
	next := evidenceFor(1, "b")
	v[1] = next
	p[1] = PR{Number: 1, SHA: next.SHA, SameRepo: true}
	f.ready = false
	tick(t, e, p, v)
	*now = now.Add(11 * time.Minute)
	tick(t, e, p, v)
	if e.State.Slots[0].Active != old || e.State.Slots[0].Candidate != nil {
		t.Fatal("failed candidate replaced working revision")
	}
	tick(t, e, p, v)
	if e.State.Slots[0].Candidate != nil {
		t.Fatal("failed candidate retried")
	}
	for _, sha := range f.published {
		if sha != old.Evidence.SHA {
			t.Fatal("unready SHA published")
		}
	}
}
func TestManualEvictionDoesNotThrash(t *testing.T) {
	e, _, p, v, now := fixture(t)
	for n := 1; n <= 3; n++ {
		ev := evidenceFor(n, "a")
		p[n] = PR{Number: n, SHA: ev.SHA, SameRepo: true}
		v[n] = ev
		tick(t, e, p, v)
		tick(t, e, p, v)
		*now = now.Add(time.Hour)
	}
	ev := evidenceFor(4, "b")
	ev.ApprovalID = 42
	ev.Approver = "cortexobscura"
	p[4] = PR{Number: 4, SHA: ev.SHA}
	v[4] = ev
	tick(t, e, p, v)
	if !e.State.Slots[0].Removing {
		t.Fatal("oldest auto not selected")
	}
	for range 8 {
		tick(t, e, p, v)
	}
	for _, s := range e.State.Slots {
		if s.PR == 1 || s.Removing {
			t.Fatalf("eviction thrash: %+v", s)
		}
	}
	if e.State.Records[1].Suppressed != v[1].Ticket() {
		t.Fatal("eviction suppression lost")
	}
}
func TestCleanupRetriesAndBlocksReuse(t *testing.T) {
	e, f, p, v, _ := fixture(t)
	tick(t, e, p, v)
	tick(t, e, p, v)
	delete(p, 1)
	f.prune = false
	tick(t, e, p, v)
	if e.State.Slots[0].PR != 1 || !e.State.Slots[0].Removing {
		t.Fatal("slot freed before cleanup")
	}
	f.err = errors.New("API unavailable")
	if e.Tick(context.Background(), p, v) == nil {
		t.Fatal("cleanup failure swallowed")
	}
	f.err = nil
	f.prune = true
	tick(t, e, p, v)
	if e.State.Slots[0].PR != 0 {
		t.Fatal("cleanup did not retry")
	}
}
func TestExpiredPreviewStaysExpired(t *testing.T) {
	e, _, p, v, now := fixture(t)
	tick(t, e, p, v)
	tick(t, e, p, v)
	*now = now.Add(72 * time.Hour)
	for range 4 {
		tick(t, e, p, v)
	}
	if e.State.Slots[0].PR != 0 {
		t.Fatal("expired preview readmitted")
	}
}
func TestStaleCandidateNeverPublished(t *testing.T) {
	e, f, p, v, _ := fixture(t)
	tick(t, e, p, v)
	p[1] = PR{Number: 1, SHA: strings.Repeat("b", 40), SameRepo: true}
	tick(t, e, p, v)
	if len(f.published) != 0 {
		t.Fatal("obsolete SHA published")
	}
}
func TestSaveFailureBeforeRuntimeMutation(t *testing.T) {
	e, f, p, v, _ := fixture(t)
	e.Save = func(context.Context, *State) error { return errors.New("conflict") }
	if e.Tick(context.Background(), p, v) == nil {
		t.Fatal("save failure hidden")
	}
	if len(f.calls) > 0 || len(f.published) > 0 {
		t.Fatal("runtime mutated before intent saved")
	}
}
func TestApprovalBindingSurvivesRestart(t *testing.T) {
	e, _, p, v, _ := fixture(t)
	ev := v[1]
	ev.ApprovalID = 7
	ev.Approver = "lambadalambda"
	v[1] = ev
	p[1] = PR{Number: 1, SHA: ev.SHA}
	tick(t, e, p, v)
	tick(t, e, p, v)
	data, _ := json.Marshal(e.State)
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	e.State = &state
	ev.SHA = strings.Repeat("b", 40)
	v[1] = ev
	p[1] = PR{Number: 1, SHA: ev.SHA}
	tick(t, e, p, v)
	if e.State.Slots[0].Candidate != nil {
		t.Fatal("old approval rebound to new SHA")
	}
}
func TestEvidenceValidation(t *testing.T) {
	p := PR{Number: 1, SHA: strings.Repeat("a", 40), SameRepo: true}
	good := evidenceFor(1, "a")
	tests := []func(*Evidence){func(e *Evidence) { e.Repository = "attacker/repo" }, func(e *Evidence) { e.PR = 2 }, func(e *Evidence) { e.SHA = strings.Repeat("b", 40) }, func(e *Evidence) { e.Digest = "latest" }, func(e *Evidence) { e.Image = "evil/image" }, func(e *Evidence) { e.Attempt = 0 }}
	for _, mutate := range tests {
		bad := good
		mutate(&bad)
		if bad.Validate(p) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	p.SameRepo = false
	if good.Validate(p) == nil {
		t.Fatal("fork automatically approved")
	}
}

func TestNewApprovalRenewsWorkingRevisionOnce(t *testing.T) {
	e, f, p, v, now := fixture(t)
	tick(t, e, p, v)
	tick(t, e, p, v)
	active := e.State.Slots[0].Active
	*now = now.Add(time.Hour)
	ev := v[1]
	ev.ApprovalID = 10
	ev.Approver = "cortexobscura"
	ev.Digest = "sha256:" + strings.Repeat("b", 64)
	v[1] = ev
	tick(t, e, p, v)
	slot := e.State.Slots[0]
	if slot.Candidate != nil || slot.Active != active || !slot.Approved || slot.Expires != now.Add(72*time.Hour) || slot.Active.Evidence.Digest == ev.Digest {
		t.Fatal("approval did not renew the existing immutable deployment")
	}
	expires := slot.Expires
	*now = now.Add(time.Hour)
	f.calls = nil
	tick(t, e, p, v)
	if e.State.Slots[0].Expires != expires {
		t.Fatal("same approval extended TTL repeatedly")
	}
	for _, call := range f.calls {
		if call == "ensure" {
			t.Fatal("renewal created candidate")
		}
	}
}
func TestFreshApprovalAfterExpiryCanReadmit(t *testing.T) {
	e, _, p, v, now := fixture(t)
	tick(t, e, p, v)
	tick(t, e, p, v)
	*now = now.Add(72 * time.Hour)
	ev := v[1]
	ev.ApprovalID = 10
	ev.Approver = "cortexobscura"
	v[1] = ev
	tick(t, e, p, v)
	if e.State.Slots[0].Candidate == nil {
		t.Fatal("fresh approval suppressed with expired ticket")
	}
}
func TestCorruptStateHasNoSideEffects(t *testing.T) {
	for _, mutate := range []func(*State){
		func(s *State) { s.Slots[0].Active.Evidence.SHA = "short" },
		func(s *State) { s.Slots[1] = s.Slots[0] },
		func(s *State) { s.Slots[0].Expires = time.Time{} },
		func(s *State) { s.Records[2] = nil },
	} {
		e, f, p, v, _ := fixture(t)
		tick(t, e, p, v)
		tick(t, e, p, v)
		mutate(e.State)
		f.calls = nil
		f.published = nil
		if e.Tick(context.Background(), p, v) == nil {
			t.Fatal("corrupt intent accepted")
		}
		if len(f.calls) > 0 || len(f.published) > 0 {
			t.Fatal("corrupt intent caused runtime effects")
		}
	}
	if (Evidence{SHA: "x"}).Revision() == "" {
		t.Fatal("empty revision")
	}
}

func TestRevisionPrefixCollisionKeepsWorking(t *testing.T) {
	e, _, p, v, _ := fixture(t)
	tick(t, e, p, v)
	tick(t, e, p, v)
	ev := v[1]
	ev.SHA = ev.SHA[:12] + strings.Repeat("b", 28)
	v[1] = ev
	p[1] = PR{Number: 1, SHA: ev.SHA, SameRepo: true}
	tick(t, e, p, v)
	if e.State.Slots[0].Candidate != nil || !strings.Contains(e.State.Records[1].Status, "collision") {
		t.Fatal("colliding revision admitted")
	}
}
