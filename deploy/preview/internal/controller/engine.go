package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Runtime interface {
	Ensure(context.Context, int, Revision) (bool, error)
	Publish(context.Context, int, int, *Revision, bool) error
	Prune(context.Context, int, []string) (bool, error)
	Withdraw(context.Context, int, int) error
}
type Engine struct {
	State   *State
	Runtime Runtime
	Save    func(context.Context, *State) error
	Now     func() time.Time
}

func (e *Engine) save(ctx context.Context) error { return e.Save(ctx, e.State) }

// tick persists intent before side effects. every runtime operation is idempotent.
func (e *Engine) Tick(ctx context.Context, prs map[int]PR, evidence map[int]Evidence) error {
	now := e.Now()
	s := e.State
	if err := s.validate(); err != nil {
		return err
	}
	if s.ApprovalBindings == nil {
		s.ApprovalBindings = map[int64]string{}
	}
	for n, p := range prs {
		r := s.record(n)
		if r.Status == "" {
			if p.SameRepo {
				r.Status = "waiting for successful CI and trusted image"
			} else {
				r.Status = "waiting for maintainer /preview <current SHA> approval and successful CI"
			}
		}
	}
	for i := range s.Slots {
		v := &s.Slots[i]
		if v.PR == 0 {
			continue
		}
		r := s.record(v.PR)
		if _, open := prs[v.PR]; !open || !now.Before(v.Expires) {
			if !v.Removing {
				v.Removing = true
				r.Status = "expired or closed; removing"
				if c := v.Candidate; c != nil {
					r.Suppressed = c.Evidence.Ticket()
				} else if v.Active != nil {
					r.Suppressed = v.Active.Evidence.Ticket()
				}
				if err := e.save(ctx); err != nil {
					return err
				}
			}
		}
		if v.Removing {
			if err := e.Runtime.Withdraw(ctx, i, v.PR); err != nil {
				return err
			}
			done, err := e.Runtime.Prune(ctx, i, nil)
			if err != nil {
				return err
			}
			if !done {
				continue
			}
			*v = Slot{}
			if err = e.save(ctx); err != nil {
				return err
			}
			continue
		}
		// refresh endpoints for replacement pods, including removing stale addresses.
		if v.Active != nil {
			if err := e.Runtime.Publish(ctx, i, v.PR, v.Active, false); err != nil {
				return err
			}
		}
		if v.Candidate == nil {
			if p, ok := prs[v.PR]; ok && v.Active != nil && p.SHA != v.Active.Evidence.SHA && !strings.HasPrefix(v.Failed, p.SHA+":") {
				r.Status = "waiting for successful CI and trusted image for " + p.SHA + "; last working revision retained"
			}
			continue
		}
		c := v.Candidate
		// a head update or withdrawn approval must never publish an obsolete candidate.
		p, open := prs[v.PR]
		ev, valid := evidence[v.PR]
		obsolete := !open || p.SHA != c.Evidence.SHA || !valid || ev.Ticket() != c.Evidence.Ticket() || ev.Validate(p) != nil
		failed := !now.Before(c.Started.Add(10 * time.Minute))
		if obsolete || failed {
			keep := []string{}
			if v.Active != nil {
				keep = append(keep, v.Active.Evidence.Revision())
			}
			done, err := e.Runtime.Prune(ctx, i, keep)
			if err != nil {
				return err
			}
			if !done {
				continue
			}
			v.Failed = c.Evidence.Ticket()
			v.Candidate = nil
			r.Status = "candidate failed or superseded; last working revision retained"
			if err = e.save(ctx); err != nil {
				return err
			}
			continue
		}
		ready, err := e.Runtime.Ensure(ctx, i, *c)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		// the candidate intent is already durable. keep the old selection until
		// publication succeeds; a crash can safely repeat this switch.
		if err = e.Runtime.Publish(ctx, i, v.PR, c, true); errors.Is(err, ErrNotReady) {
			continue
		} else if err != nil {
			return err
		}
		v.Active = c
		v.Candidate = nil
		v.Approved = c.Evidence.ApprovalID > 0
		r.Status = "ready"
		if err = e.save(ctx); err != nil {
			return err
		}

	}
	// complete old revision gc before admitting any candidate anywhere.
	for i, v := range s.Slots {
		if v.Removing || v.Candidate != nil {
			return nil
		}
		if v.PR != 0 {
			keep := []string{}
			if v.Active != nil {
				keep = append(keep, v.Active.Evidence.Revision())
			}
			done, err := e.Runtime.Prune(ctx, i, keep)
			if err != nil {
				return err
			}
			if !done {
				return nil
			}
		}
	}
	for _, ev := range eligible(prs, evidence) {
		r := s.record(ev.PR)
		if ev.ApprovalID > 0 {
			binding := fmt.Sprintf("%d:%s", ev.PR, ev.SHA)
			if previous, ok := s.ApprovalBindings[ev.ApprovalID]; ok && previous != binding {
				r.Status = "approval already bound to another SHA; post a new /preview <current SHA>"
				continue
			}
			s.ApprovalBindings[ev.ApprovalID] = binding
		}
		if r.Suppressed == ev.Ticket() {
			continue
		}
		target := -1
		existing := false
		for i, v := range s.Slots {
			if v.PR == ev.PR {
				target = i
				existing = true
				break
			}
		}
		if existing {
			v := &s.Slots[target]
			if v.Active != nil && v.Active.Evidence.SHA == ev.SHA {
				// a fresh explicit approval renews the existing deployment; its
				// deployed digest remains unchanged even if ci rebuilt the sha.
				if ev.ApprovalID > v.Active.Evidence.ApprovalID {
					v.Active.Evidence.ApprovalID = ev.ApprovalID
					v.Active.Evidence.Approver = ev.Approver
					v.Approved = true
					v.Admitted = now
					v.Expires = now.Add(72 * time.Hour)
					r.Status = "ready"
				}
				continue
			}
			if v.Active != nil && v.Active.Evidence.Revision() == ev.Revision() {
				r.Status = "revision name collision; maintainer intervention required"
				continue
			}
			if v.Failed == ev.Ticket() {
				continue
			}
		} else {
			for i, v := range s.Slots {
				if v.PR == 0 {
					target = i
					break
				}
			}
			if target < 0 && ev.ApprovalID > 0 {
				// manual approval evicts oldest automatic first, then oldest approved.
				for i, v := range s.Slots {
					if target < 0 || (!v.Approved && s.Slots[target].Approved) || (v.Approved == s.Slots[target].Approved && v.Admitted.Before(s.Slots[target].Admitted)) {
						target = i
					}
				}
				v := &s.Slots[target]
				old := s.record(v.PR)
				if v.Active != nil {
					old.Suppressed = v.Active.Evidence.Ticket()
				}
				if desired, ok := evidence[v.PR]; ok {
					old.Suppressed = desired.Ticket()
				}
				old.Status = "evicted; new commit or approval required"
				v.Removing = true
				return e.save(ctx)
			}
			if target < 0 {
				r.Status = "queued; all three slots occupied"
				continue
			}
			s.Slots[target] = Slot{PR: ev.PR, Approved: ev.ApprovalID > 0, Admitted: now, Expires: now.Add(72 * time.Hour)}
		}
		v := &s.Slots[target]
		v.Candidate = &Revision{Evidence: ev, Started: now}
		r.Status = fmt.Sprintf("starting %s", ev.SHA)
		return e.save(ctx)
	}
	return e.save(ctx)
}
