package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"github.com/google/uuid"
)

func (r *Runner) memberState(tx *store.Tx, m model.Member, state model.MemberState, reason *model.StopReason) error {
	now := r.b.now()
	if state != model.MemberStateRunning {
		if e := tx.SetMemberActivity(m.ID, nil, now); e != nil {
			return e
		}
	}
	if state == model.MemberStateStopped {
		if e := tx.ReleaseHold(m.ID, now); e != nil && !missing(e) {
			return e
		}
	}
	var started, stopped **model.Time
	if state == model.MemberStateRunning {
		started = ptr(&now)
		stopped = ptr((*model.Time)(nil))
	}
	if state == model.MemberStateStopped {
		stopped = ptr(&now)
	}
	if e := tx.SetMemberState(m.ID, state, reason, started, stopped, now); e != nil {
		return e
	}
	updated, e := tx.GetMember(m.ID)
	if e != nil {
		return e
	}
	return r.b.event(tx, &m.Team, model.EventMemberStateChanged, true, &m.ID, nil, wire.MemberStateChangedEventData{Member: *updated, From: m.State, To: state, Reason: reason})
}
func (r *Runner) launchHeadless(ctx context.Context, m model.Member, cause model.LeaseCause) (*model.Member, error) {
	if r.prepareMember != nil {
		ready, e := r.prepareMember(ctx, m)
		if e != nil {
			return nil, r.launchFailed(ctx, m, e)
		}
		m = *ready
	}
	host := r.b.host(ctx)
	if !host.CanRunHeadless(*m.Harness) {
		return nil, r.launchFailed(ctx, m, invalid("harness cannot run headless"))
	}
	if issue := host.HarnessIssue(*m.Harness); issue != "" {
		return nil, r.launchFailed(ctx, m, model.NewError(model.ErrorHarnessUnavailable, "harness is unavailable").WithDetail("reason", issue))
	}
	binary, e := r.opts.Binary(*m.Harness)
	if e != nil {
		return nil, r.launchFailed(ctx, m, e)
	}
	if m.Workdir == nil {
		return nil, r.launchFailed(ctx, m, fmt.Errorf("member has no workdir"))
	}
	if stat, e := os.Stat(*m.Workdir); e != nil || !stat.IsDir() {
		if e == nil {
			e = fmt.Errorf("workdir is not a directory")
		}
		return nil, r.launchFailed(ctx, m, e)
	}
	id, e := r.b.opts.ID()
	if e != nil {
		return nil, e
	}
	token, e := model.NewToken()
	if e != nil {
		return nil, e
	}
	var starting *model.Member
	newSession := false
	e = r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning || m.Lease.Driver != model.DriverNone && !(m.Lease.Driver == model.DriverPane && (cause == model.LeaseCauseReturnHeadless || cause == model.LeaseCausePaneExit)) {
			return errRunnerGeneration
		}
		current, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if cause == model.LeaseCauseWake {
			allowed, e := r.mayWake(tx, *current)
			if e != nil {
				return e
			}
			if !allowed {
				return errRunnerGeneration
			}
		}
		now := r.b.now()
		next := model.Lease{Driver: model.DriverHeadless, Generation: m.Lease.Generation + 1, Launch: &id}
		if m.Lease.Launch != nil {
			if e = tx.EndLaunch(*m.Lease.Launch, model.LaunchEndReasonConverted, now); e != nil {
				return e
			}
			if _, e = tx.RevokeMemberTokens(m.ID, now); e != nil {
				return e
			}
		}
		if e = tx.InsertLaunch(store.Launch{ID: id, MemberID: m.ID, Generation: next.Generation, Driver: model.ModeHeadless, SessionIDStart: current.Session, StartedAt: now}); e != nil {
			return e
		}
		if e = tx.InsertToken(store.Token{LaunchID: id, MemberID: m.ID, Hash: model.HashToken(token), CreatedAt: now}); e != nil {
			return e
		}
		if e = tx.CompareAndSwapLease(m.ID, m.Lease.Driver, m.Lease.Generation, next, now); e != nil {
			return e
		}
		if current.Session == nil && *m.Harness == model.HarnessClaude {
			sid, e := uuid.NewRandom()
			if e != nil {
				return e
			}
			seal, e := model.NewSeal()
			if e != nil {
				return e
			}
			if e = r.session(tx, m, sid.String(), seal, model.SessionSourceAssigned, id, false); e != nil {
				return e
			}
			newSession = true
		}
		if m.State != model.MemberStateRunning {
			if e = r.memberState(tx, m, model.MemberStateStarting, nil); e != nil {
				return e
			}
		}
		starting, e = tx.GetMember(m.ID)
		return e
	})
	if e != nil {
		return nil, e
	}
	desc := TurnLaunch{Harness: *m.Harness, Profile: *m.Profile, Workdir: *m.Workdir, StateDir: r.b.opts.StateDir, LaunchID: id, MemberID: m.ID, Binary: binary, Session: starting.Session, NewSession: newSession}
	if e = statedir.Ensure(r.b.opts.StateDir); e == nil {
		e = statedir.WriteSecret(filepath.Join(r.b.opts.StateDir, "launch", id+".token"), []byte(token+"\n"))
	}
	if e == nil {
		e = r.opts.Files.Write(ctx, desc)
	}
	if e == nil {
		e = r.saveLaunch(desc)
	}
	if e != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return nil, r.launchFailed(cleanup, *starting, e)
	}
	var ready *model.Member
	e = r.write(ctx, starting, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return errRunnerGeneration
		}
		if e := tx.UpdateMember(m.ID, store.MemberPatch{LastDriver: ptr(ptr(model.ModeHeadless))}, r.b.now()); e != nil {
			return e
		}
		if starting.State != model.MemberStateRunning {
			if e := r.memberState(tx, *starting, model.MemberStateRunning, nil); e != nil {
				return e
			}
		}
		ready, e = tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if e = r.setActivity(tx, *ready, model.ActivityIdle); e != nil {
			return e
		}
		ready, e = tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		return r.b.event(tx, &m.Team, model.EventLeaseChanged, true, &m.ID, nil, wire.LeaseChangedEventData{Member: *ready, From: m.Lease.Driver, To: model.DriverHeadless, Generation: ready.Lease.Generation, Cause: cause})
	})
	return ready, e
}
func (r *Runner) launchFailed(ctx context.Context, m model.Member, cause error) error {
	e := r.write(ctx, &m, func(tx *store.Tx, _ *store.Team) error {
		if m.Lease != nil && m.Lease.Driver != model.DriverNone {
			now := r.b.now()
			if m.Lease.Launch != nil {
				l, e := tx.GetLaunch(*m.Lease.Launch)
				if e != nil {
					return e
				}
				if l.EndedAt == nil {
					if e = tx.EndLaunch(l.ID, model.LaunchEndReasonStopped, now); e != nil {
						return e
					}
				}
				if _, e = tx.RevokeMemberTokens(m.ID, now); e != nil {
					return e
				}
			}
			next := model.Lease{Driver: model.DriverNone, Generation: m.Lease.Generation + 1}
			if e := tx.CompareAndSwapLease(m.ID, m.Lease.Driver, m.Lease.Generation, next, now); e != nil {
				return e
			}
			current, e := tx.GetMember(m.ID)
			if e != nil {
				return e
			}
			if e = r.b.event(tx, &m.Team, model.EventLeaseChanged, true, &m.ID, nil, wire.LeaseChangedEventData{Member: *current, From: m.Lease.Driver, To: next.Driver, Generation: next.Generation, Cause: model.LeaseCauseStop}); e != nil {
				return e
			}
		}
		ds, e := tx.NextDeliveries(m.ID, 0)
		if e != nil {
			return e
		}
		for _, d := range ds {
			if _, e = r.b.changeDelivery(tx, m.Team, d.MessageID, d.Delivery, model.DeliveryStateHeld, ptr(model.DeliveryReasonDeliveryFailed)); e != nil {
				return e
			}
		}
		return r.memberState(tx, m, model.MemberStateStopped, ptr(model.StopReasonLaunchFailed))
	})
	if e != nil {
		return e
	}
	if m.Lease != nil && m.Lease.Launch != nil {
		if e = r.removeLaunchFiles(ctx, *m.Lease.Launch); e != nil {
			return e
		}
	}
	return model.NewError(model.ErrorLaunchFailed, "member launch failed").WithDetail("cause", cause.Error())
}
func (r *Runner) removeLaunchFiles(ctx context.Context, id string) error {
	if e := r.opts.Files.Delete(ctx, id); e != nil {
		return e
	}
	for _, suffix := range []string{"", ".token", ".json", ".opencode.json"} {
		if e := os.RemoveAll(filepath.Join(r.b.opts.StateDir, "launch", id+suffix)); e != nil {
			return e
		}
	}
	return nil
}
func (r *Runner) session(tx *store.Tx, m model.Member, id, seal string, source model.SessionSource, launch string, briefed bool) error {
	if id == "" {
		return nil
	}
	existing, e := tx.GetMemberSession(m.ID, id)
	if e != nil && !missing(e) {
		return e
	}
	if e != nil {
		sessions, e := tx.ListMemberSessions(m.ID)
		if e != nil {
			return e
		}
		rec := store.MemberSessionRecord{MemberID: m.ID, Ordinal: int64(len(sessions) + 1), SessionID: id, Harness: *m.Harness, Source: source, LaunchID: &launch, Seal: seal, FirstSeenAt: r.b.now()}
		if e = tx.InsertMemberSession(rec); e != nil {
			return e
		}
		existing = &rec
		if e = tx.SetMemberSession(m.ID, &id, r.b.now()); e != nil {
			return e
		}
		updated, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		s := updated.Sessions[len(updated.Sessions)-1]
		if e = r.b.event(tx, &m.Team, model.EventMemberSessionAdded, true, &m.ID, nil, wire.MemberSessionAddedEventData{Member: *updated, Session: s}); e != nil {
			return e
		}
	} else {
		if e = tx.SetMemberSession(m.ID, &id, r.b.now()); e != nil {
			return e
		}
	}
	if briefed && existing.BriefedAt == nil {
		return tx.MarkSessionBriefed(m.ID, existing.Ordinal, r.b.now())
	}
	return nil
}

// Keep effective launch settings separate from mutable member settings. A profile
// edit applies to the next launch, including across runner restarts.
func (r *Runner) launchDescriptionPath(id string) string {
	return filepath.Join(r.b.opts.StateDir, "launch", id, "runner-launch.json")
}
func (r *Runner) saveLaunch(desc TurnLaunch) error {
	raw, e := json.Marshal(desc)
	if e != nil {
		return e
	}
	path := r.launchDescriptionPath(desc.LaunchID)
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	return statedir.WriteSecret(path, raw)
}
func (r *Runner) effectiveLaunch(desc TurnLaunch) (TurnLaunch, error) {
	raw, e := os.ReadFile(r.launchDescriptionPath(desc.LaunchID))
	if os.IsNotExist(e) {
		return desc, nil
	}
	if e != nil {
		return desc, e
	}
	var saved TurnLaunch
	if e = model.DecodeStrict(raw, &saved); e != nil {
		return desc, e
	}
	if saved.LaunchID != desc.LaunchID || saved.MemberID != desc.MemberID {
		return desc, model.NewError(model.ErrorInternal, "launch description identity mismatch")
	}
	saved.Session = desc.Session
	saved.NewSession = desc.NewSession
	return saved, nil
}
