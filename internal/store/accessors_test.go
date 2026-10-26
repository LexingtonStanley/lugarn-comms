package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func id(n int) string { return fmt.Sprintf("%026d", n) }

func ptr[T any](v T) *T { return &v }

func agentParams(memberID, name string) MemberParams {
	return MemberParams{ID: memberID, TeamID: "team", Kind: model.MemberKindAgent, Name: name,
		Role: ptr(model.RoleWorker), Origin: model.MemberOriginSpec, Harness: ptr(model.HarnessClaude),
		Profile: ptr(model.ProfileWorkspace), Mode: ptr(model.ModeHeadless), Folder: ptr("/repo"),
		Brief: ptr("brief"), CreatedAt: ptr(testTime()), UpdatedAt: ptr(testTime())}
}

func humanParams() MemberParams {
	return MemberParams{ID: id(1), TeamID: "team", Kind: model.MemberKindHuman, Name: "human",
		Origin: model.MemberOriginImplicit, State: model.MemberStateRunning, CreatedAt: ptr(testTime()), UpdatedAt: ptr(testTime())}
}

func makeFixture(t *testing.T, s *Store) {
	t.Helper()
	writeTest(t, s, func(tx *Tx) error {
		if err := tx.CreateTeam(testTeam()); err != nil {
			return err
		}
		if err := tx.InsertMember(humanParams()); err != nil {
			return err
		}
		return tx.InsertMember(agentParams(id(2), "worker"))
	})
}

func TestTeamAndMemberAccessors(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	readTest(t, s, func(tx *Tx) error {
		byID, err := tx.GetTeam("team")
		if err != nil {
			return err
		}
		byName, err := tx.GetTeamByName("team-one")
		if err != nil {
			return err
		}
		teams, err := tx.ListTeams()
		if err != nil {
			return err
		}
		if byID.ID != byName.ID || len(teams) != 1 || byID.HopBudget != 16 {
			t.Fatalf("team reads = %#v, %d", byID, len(teams))
		}
		member, err := tx.GetMember(id(2))
		if err != nil {
			return err
		}
		live, err := tx.GetLiveMember("team", "worker")
		if err != nil {
			return err
		}
		members, err := tx.ListMembers("team", false)
		if err != nil {
			return err
		}
		if member.Name != live.Name || len(members) != 2 || member.Workdir == nil || *member.Workdir != "/repo" {
			t.Fatalf("member reads = %#v, %d", member, len(members))
		}
		stored, err := tx.GetStoredMember(id(2))
		if err != nil {
			return err
		}
		if stored.Brief == nil || *stored.Brief != "brief" {
			t.Fatalf("stored member = %#v", stored)
		}
		return nil
	})

	writeTest(t, s, func(tx *Tx) error {
		if err := tx.UpdateTeam("team", TeamPatch{Name: ptr("team-two"), HopBudget: ptr(int64(20))}, testTime()); err != nil {
			return err
		}
		if revision, err := tx.BumpTeamRevision("team", 1, testTime()); err != nil || revision != 2 {
			t.Fatalf("revision = %d, %v", revision, err)
		}
		if _, err := tx.BumpTeamRevision("team", 1, testTime()); err == nil {
			t.Fatal("stale revision accepted")
		} else {
			requireCode(t, err, model.ErrorRevisionConflict)
		}
		if run, err := tx.BumpTeamRun("team", testTime()); err != nil || run != 1 {
			t.Fatalf("run = %d, %v", run, err)
		}
		pid := int64(42)
		if err := tx.Heartbeat("team", model.Runner{State: model.RunnerStateUp, PID: &pid, StartedAt: ptr(testTime()), HeartbeatAt: ptr(testTime())}, testTime()); err != nil {
			return err
		}
		at := model.Time{Time: testTime().Add(time.Hour)}
		if changed, err := tx.TouchAppContact(ptr("team"), at); err != nil || !changed {
			t.Fatalf("first contact = %v, %v", changed, err)
		}
		if changed, err := tx.TouchAppContact(ptr("team"), model.Time{Time: at.Add(30 * time.Second)}); err != nil || changed {
			t.Fatalf("early contact = %v, %v", changed, err)
		}
		if changed, err := tx.TouchAppContact(ptr("team"), model.Time{Time: at.Add(61 * time.Second)}); err != nil || !changed {
			t.Fatalf("later contact = %v, %v", changed, err)
		}
		if err := tx.UpdateMember(id(2), MemberPatch{Task: ptr(ptr("task")), HerdrAgent: ptr(ptr("team-two-worker"))}, testTime()); err != nil {
			return err
		}
		if err := tx.SetMemberState(id(2), model.MemberStateRunning, nil, nil, nil, testTime()); err != nil {
			return err
		}
		if err := tx.SetMemberActivity(id(2), ptr(model.ActivityWorking), testTime()); err != nil {
			return err
		}
		if err := tx.SetMemberSession(id(2), ptr("session-current"), testTime()); err != nil {
			return err
		}
		removed := agentParams(id(90), "removed")
		if err := tx.InsertMember(removed); err != nil {
			return err
		}
		if err := tx.RemoveMember(id(90), model.RemovedReasonRemovedFromSpec, testTime()); err != nil {
			return err
		}
		duplicate := agentParams(id(91), "worker")
		if err := tx.InsertMember(duplicate); err == nil {
			t.Fatal("duplicate live name accepted")
		} else {
			requireCode(t, err, model.ErrorNameTaken)
		}
		return nil
	})
	readTest(t, s, func(tx *Tx) error {
		team, err := tx.GetTeam("team")
		if err != nil {
			return err
		}
		if team.Name != "team-two" || team.Run != 1 || team.Runner.State != model.RunnerStateUp || team.LastAppContactAt == nil {
			t.Fatalf("updated team = %#v", team)
		}
		member, err := tx.GetMember(id(2))
		if err != nil {
			return err
		}
		if member.State != model.MemberStateRunning || member.Activity == nil || *member.Activity != model.ActivityWorking || member.Session == nil {
			t.Fatalf("updated member = %#v", member)
		}
		live, err := tx.ListMembers("team", false)
		if err != nil {
			return err
		}
		all, err := tx.ListMembers("team", true)
		if err != nil {
			return err
		}
		if len(live) != 2 || len(all) != 3 {
			t.Fatalf("member counts = %d, %d", len(live), len(all))
		}
		return nil
	})
}

func TestGridCounterHoldSpawnAndWorktreeAccessors(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	writeTest(t, s, func(tx *Tx) error {
		edges := []GridEdge{{FromID: id(1), ToID: id(2), Can: []model.GridCapability{model.GridCapabilityMessage}}}
		// Human is legal at the storage layer; policy owns the implicit-grid rule.
		if err := tx.ReplaceGrid("team", edges); err != nil {
			return err
		}
		if err := tx.AddGridEdges("team", []GridEdge{{FromID: id(1), ToID: id(2), Can: []model.GridCapability{model.GridCapabilityWake}}}); err != nil {
			return err
		}
		if err := tx.PutHopCounter(HopCounter{TeamID: "team", Allowance: 16}); err != nil {
			return err
		}
		if err := tx.PutRoundCounter(RoundCounter{TeamID: "team", WriterID: id(2), ReviewerID: id(1), Allowance: 3}); err != nil {
			return err
		}
		if err := tx.PutHold(Hold{MemberID: id(2), TeamID: "team", HeldUntil: model.Time{Time: time.Now().Add(time.Hour)}}); err != nil {
			return err
		}
		harnesses := []model.Harness{model.HarnessClaude}
		if err := tx.PutSpawnSettings(SpawnSettings{MemberID: id(2), Enabled: true, RunningAtOnce: 3, PerRun: 8,
			Harnesses: &harnesses, Mode: model.ModeHeadless, Folder: model.SpawnFolderWorktree, MaxProfile: model.ProfileWorkspace, Brief: ptr("spawn brief")}); err != nil {
			return err
		}
		spawned := agentParams(id(3), "spawned")
		spawned.Origin = model.MemberOriginSpawn
		spawned.SpawnedBy = ptr(id(2))
		spawned.SpawnedRun = ptr(int64(0))
		spawned.State = model.MemberStateRunning
		if err := tx.InsertMember(spawned); err != nil {
			return err
		}
		if err := tx.InsertWorktree(WorktreeRecord{ID: id(10), TeamID: "team", MemberID: id(2), Repo: "/repo", Path: "/repo-lugarn/team-worker", Branch: "lugarn/team/worker", Base: "main"}); err != nil {
			return err
		}
		if err := tx.UpdateMember(id(2), MemberPatch{WorktreeID: ptr(ptr(id(10)))}, testTime()); err != nil {
			return err
		}
		return nil
	})
	readTest(t, s, func(tx *Tx) error {
		rows, err := tx.ListGrid("team")
		if err != nil {
			return err
		}
		edge, err := tx.GetGridEdge(id(1), id(2))
		if err != nil {
			return err
		}
		if len(rows) != 2 || edge == nil || len(edge.Can) != 2 {
			t.Fatalf("grid = %d, %#v", len(rows), edge)
		}
		hops, err := tx.GetHopCounter("team")
		if err != nil {
			return err
		}
		round, err := tx.GetRoundCounter(id(2), id(1))
		if err != nil {
			return err
		}
		rounds, err := tx.ListRoundCounters("team")
		if err != nil {
			return err
		}
		if hops.Used != 0 || round.Allowance != 3 || len(rounds) != 1 {
			t.Fatalf("counters = %#v %#v", hops, rounds)
		}
		hold, err := tx.GetHold(id(2))
		if err != nil {
			return err
		}
		if hold.ReleasedAt != nil {
			t.Fatal("hold already released")
		}
		settings, err := tx.GetSpawnSettings(id(2))
		if err != nil {
			return err
		}
		count, err := tx.GetSpawnCount(id(2))
		if err != nil {
			return err
		}
		counts, err := tx.ListSpawnCounts("team")
		if err != nil {
			return err
		}
		if settings.Harnesses == nil || count.Running != 1 || count.ThisRun != 1 || len(counts) != 1 {
			t.Fatalf("spawn = %#v %#v", settings, count)
		}
		worktree, err := tx.GetWorktree(id(10))
		if err != nil {
			return err
		}
		byPath, err := tx.GetWorktreeByPath(worktree.Path)
		if err != nil {
			return err
		}
		list, err := tx.ListWorktrees("team", false)
		if err != nil {
			return err
		}
		member, err := tx.GetMember(id(2))
		if err != nil {
			return err
		}
		if byPath.ID != worktree.ID || len(list) != 1 || member.Worktree == nil || *member.Workdir != worktree.Path || member.HeldUntil == nil {
			t.Fatalf("worktree/member = %#v %#v", worktree, member)
		}
		return nil
	})
	writeTest(t, s, func(tx *Tx) error {
		if err := tx.ReleaseHold(id(2), testTime()); err != nil {
			return err
		}
		if err := tx.RemoveWorktree(id(10), testTime()); err != nil {
			return err
		}
		if err := tx.DeleteSpawnSettings(id(2)); err != nil {
			return err
		}
		if err := tx.DeleteHopCounter("team"); err != nil {
			return err
		}
		return tx.DeleteRoundCounters("team")
	})
	readTest(t, s, func(tx *Tx) error {
		worktrees, err := tx.ListWorktrees("team", false)
		if err != nil {
			return err
		}
		all, err := tx.ListWorktrees("team", true)
		if err != nil {
			return err
		}
		if len(worktrees) != 0 || len(all) != 1 {
			t.Fatalf("worktrees after removal = %d, %d", len(worktrees), len(all))
		}
		return nil
	})
}

func TestLeaseLaunchTokenSessionAndTurnAccessors(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	launchID, turnID := id(20), id(21)
	writeTest(t, s, func(tx *Tx) error {
		if err := tx.InsertLease(id(2), model.Lease{Driver: model.DriverNone}, testTime()); err != nil {
			return err
		}
		if err := tx.InsertLaunch(Launch{ID: launchID, MemberID: id(2), Generation: 1, Driver: model.ModeHeadless, StartedAt: testTime()}); err != nil {
			return err
		}
		if err := tx.CompareAndSwapLease(id(2), model.DriverNone, 0, model.Lease{Driver: model.DriverHeadless, Generation: 1, Launch: &launchID}, testTime()); err != nil {
			return err
		}
		if err := tx.CompareAndSwapLease(id(2), model.DriverNone, 0, model.Lease{Driver: model.DriverNone, Generation: 2}, testTime()); err == nil {
			t.Fatal("stale lease accepted")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		if err := tx.InsertToken(Token{LaunchID: launchID, MemberID: id(2), Hash: fmt.Sprintf("%064x", 1)}); err != nil {
			return err
		}
		if err := tx.InsertMemberSession(MemberSessionRecord{MemberID: id(2), Ordinal: 1, SessionID: "session-one", Harness: model.HarnessClaude,
			Source: model.SessionSourceAssigned, LaunchID: &launchID, Seal: "01234567"}); err != nil {
			return err
		}
		if err := tx.InsertTurn(TurnParams{MemberID: id(2), Turn: model.Turn{ID: turnID, Team: "team", Launch: launchID,
			LeaseGeneration: 1, Harness: model.HarnessClaude, Status: model.TurnStatusStarting, StartedAt: testTime(),
			Usage: &model.Usage{InputTokens: ptr(int64(12)), CostUSD: ptr(0.25)}}, UsageRaw: json.RawMessage(`{"raw":true}`)}); err != nil {
			return err
		}
		duplicate := TurnParams{MemberID: id(2), Turn: model.Turn{ID: id(22), Team: "team", Launch: launchID, LeaseGeneration: 1, Harness: model.HarnessClaude, Status: model.TurnStatusRunning}}
		if err := tx.InsertTurn(duplicate); err == nil {
			t.Fatal("second live turn accepted")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		return nil
	})
	readTest(t, s, func(tx *Tx) error {
		lease, err := tx.GetLease(id(2))
		if err != nil {
			return err
		}
		leases, err := tx.ListLeases("team")
		if err != nil {
			return err
		}
		launch, err := tx.GetLaunch(launchID)
		if err != nil {
			return err
		}
		launches, err := tx.ListLaunches("team")
		if err != nil {
			return err
		}
		if lease.Lease.Generation != 1 || len(leases) != 1 || launch.ID != launchID || len(launches) != 1 {
			t.Fatalf("lease/launch reads failed")
		}
		token, err := tx.GetTokenByHash(fmt.Sprintf("%064x", 1))
		if err != nil || token.RevokedAt != nil {
			t.Fatalf("token = %#v, %v", token, err)
		}
		session, err := tx.GetMemberSession(id(2), "session-one")
		if err != nil {
			return err
		}
		sessions, err := tx.ListMemberSessions(id(2))
		if err != nil {
			return err
		}
		if session.Seal != "01234567" || len(sessions) != 1 {
			t.Fatalf("sessions = %#v", sessions)
		}
		turn, err := tx.GetTurn(turnID)
		if err != nil {
			return err
		}
		live, err := tx.GetLiveTurn(id(2))
		if err != nil {
			return err
		}
		count, err := tx.CountLiveTurns("team")
		if err != nil {
			return err
		}
		list, err := tx.ListTurns(id(2), 10)
		if err != nil {
			return err
		}
		seal, err := tx.GetTurnSeal(turnID)
		if err != nil {
			return err
		}
		raw, err := tx.GetTurnUsageRaw(turnID)
		if err != nil || string(raw) != `{"raw":true}` {
			t.Fatalf("usage raw = %s, %v", raw, err)
		}
		if turn.Usage == nil || turn.Usage.InputTokens == nil || *turn.Usage.InputTokens != 12 || live.ID != turnID || count != 1 || len(list) != 1 || seal != nil {
			t.Fatalf("turn = %#v, count %d, seal %v", turn, count, seal)
		}
		return nil
	})
	writeTest(t, s, func(tx *Tx) error {
		if err := tx.MarkSessionBriefed(id(2), 1, testTime()); err != nil {
			return err
		}
		if err := tx.UpdateTurn(turnID, ptr(int64(2)), TurnPatch{Status: ptr(model.TurnStatusRunning)}); err == nil {
			t.Fatal("stale turn generation accepted")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		if err := tx.CompareAndSwapLease(id(2), model.DriverHeadless, 1, model.Lease{Driver: model.DriverNone, Generation: 2}, testTime()); err != nil {
			return err
		}
		status := model.TurnStatusSucceeded
		ended := testTime()
		if err := tx.UpdateTurn(turnID, ptr(int64(1)), TurnPatch{Status: &status, EndedAt: ptr(&ended)}); err == nil {
			t.Fatal("turn landed after its lease changed")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		if err := tx.UpdateTurn(turnID, nil, TurnPatch{Status: &status, EndedAt: ptr(&ended)}); err != nil {
			return err
		}
		if err := tx.RevokeToken(launchID, testTime()); err != nil {
			return err
		}
		if err := tx.EndLaunch(launchID, model.LaunchEndReasonStopped, testTime()); err != nil {
			return err
		}
		if err := tx.EndLaunch(launchID, model.LaunchEndReasonStopped, testTime()); err == nil {
			t.Fatal("launch ended twice")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		return nil
	})
	readTest(t, s, func(tx *Tx) error {
		turn, err := tx.GetTurn(turnID)
		if err != nil {
			return err
		}
		token, err := tx.GetTokenByHash(fmt.Sprintf("%064x", 1))
		if err != nil {
			return err
		}
		session, err := tx.GetMemberSession(id(2), "session-one")
		if err != nil {
			return err
		}
		if turn.Status != model.TurnStatusSucceeded || turn.EndedAt == nil || token.RevokedAt == nil || session.BriefedAt == nil {
			t.Fatalf("final runtime records invalid")
		}
		return nil
	})
}

func TestMessageDeliveryAndRequestAccessors(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	messageID := id(30)
	writeTest(t, s, func(tx *Tx) error {
		seq, err := tx.InsertMessage(MessageParams{SenderID: id(1), Run: 1, Message: model.Message{ID: messageID, Team: "team", Kind: model.MessageKindMessage, Text: "hello", CreatedAt: testTime()}})
		if err != nil || seq != 1 {
			t.Fatalf("message seq = %d, %v", seq, err)
		}
		if err := tx.InsertDelivery(DeliveryParams{MessageID: messageID, RecipientID: id(2), Delivery: model.Delivery{State: model.DeliveryStateQueued}}); err != nil {
			return err
		}
		_, err = tx.InsertMessage(MessageParams{SenderID: id(2), Run: 1, Message: model.Message{ID: id(31), Team: "team", Kind: model.MessageKindMessage, Text: "to human", CreatedAt: model.Time{Time: testTime().Add(time.Minute)}}})
		if err != nil {
			return err
		}
		deliveredAt := model.Time{Time: testTime().Add(time.Minute)}
		notify := true
		via := model.DeliveryViaHuman
		if err := tx.InsertDelivery(DeliveryParams{MessageID: id(31), RecipientID: id(1), Delivery: model.Delivery{State: model.DeliveryStateDelivered, Via: &via, Notify: &notify, DeliveredAt: &deliveredAt}}); err != nil {
			return err
		}
		launchID := id(50)
		if err := tx.InsertLaunch(Launch{ID: launchID, MemberID: id(2), Generation: 1, Driver: model.ModeHeadless}); err != nil {
			return err
		}
		if err := tx.InsertLease(id(2), model.Lease{Driver: model.DriverHeadless, Generation: 1, Launch: &launchID}, testTime()); err != nil {
			return err
		}
		if err := tx.InsertRequest(RequestParams{MemberID: ptr(id(2)), RequestedByID: id(1), Request: model.Request{ID: id(40), Team: "team", Kind: model.RequestKindOpenTerminal, Args: map[string]any{"cancel_turn": true}}}); err != nil {
			return err
		}
		if err := tx.InsertRequest(RequestParams{MemberID: ptr(id(2)), RequestedByID: id(1), Request: model.Request{ID: id(41), Team: "team", Kind: model.RequestKindStopMember}}); err == nil {
			t.Fatal("second open request accepted")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		return nil
	})
	readTest(t, s, func(tx *Tx) error {
		message, err := tx.GetMessage(messageID)
		if err != nil {
			return err
		}
		delivery, err := tx.GetDelivery(messageID, id(2))
		if err != nil {
			return err
		}
		list, err := tx.ListDeliveries(messageID)
		if err != nil {
			return err
		}
		if message.From.Name != "human" || delivery.To.Name != "worker" || len(list) != 1 {
			t.Fatalf("message reads = %#v", message)
		}
		inbox, more, err := tx.Inbox(id(2), model.InboxDirectionIn, 20, "")
		if err != nil || more || len(inbox) != 0 {
			t.Fatalf("queued inbox = %d, %v, %v", len(inbox), more, err)
		}
		out, _, err := tx.Inbox(id(2), model.InboxDirectionOut, 20, "")
		if err != nil || len(out) != 1 {
			t.Fatalf("out inbox = %d, %v", len(out), err)
		}
		count, err := tx.CountMessagesToHuman(id(2), model.Time{Time: testTime().Add(2 * time.Minute)})
		if err != nil || count != 1 {
			t.Fatalf("human count = %d, %v", count, err)
		}
		request, err := tx.GetRequest(id(40))
		if err != nil {
			return err
		}
		open, err := tx.ListOpenRequests("team")
		if err != nil {
			return err
		}
		if request.RequestedBy.Name != "human" || request.Member.Name != "worker" || request.Args["cancel_turn"] != true || len(open) != 1 {
			t.Fatalf("request = %#v", request)
		}
		return nil
	})
	writeTest(t, s, func(tx *Tx) error {
		via := model.DeliveryViaHeadless
		items, err := tx.PickUpDeliveries(id(2), 10, &via, nil, ptr(int64(1)), testTime())
		if err != nil || len(items) != 1 || items[0].Delivery.Attempts != 1 || items[0].Delivery.State != model.DeliveryStateDelivering {
			t.Fatalf("pickup = %#v, %v", items, err)
		}
		if err := tx.UpdateDelivery(messageID, id(2), model.DeliveryStateQueued, DeliveryPatch{State: model.DeliveryStateDelivered}); err == nil {
			t.Fatal("stale delivery state accepted")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		deliveredAt := testTime()
		noReason := (*model.DeliveryReason)(nil)
		if err := tx.CompareAndSwapLease(id(2), model.DriverHeadless, 1, model.Lease{Driver: model.DriverNone, Generation: 2}, testTime()); err != nil {
			return err
		}
		if err := tx.UpdateDelivery(messageID, id(2), model.DeliveryStateDelivering, DeliveryPatch{State: model.DeliveryStateDelivered, Reason: &noReason, DeliveredAt: ptr(&deliveredAt), ExpectedLeaseGeneration: ptr(int64(1))}); err == nil {
			t.Fatal("delivery landed after its lease changed")
		} else {
			requireCode(t, err, model.ErrorLeaseConflict)
		}
		if err := tx.UpdateDelivery(messageID, id(2), model.DeliveryStateDelivering, DeliveryPatch{State: model.DeliveryStateDelivered, Reason: &noReason, DeliveredAt: ptr(&deliveredAt)}); err != nil {
			return err
		}
		started := testTime()
		if err := tx.UpdateRequest(id(40), model.RequestStatusPending, RequestPatch{Status: model.RequestStatusRunning, StartedAt: ptr(&started)}); err != nil {
			return err
		}
		finished := model.Time{Time: testTime().Add(time.Minute)}
		var noError *model.Error
		if err := tx.UpdateRequest(id(40), model.RequestStatusRunning, RequestPatch{Status: model.RequestStatusSucceeded, Error: &noError, FinishedAt: ptr(&finished)}); err != nil {
			return err
		}
		return tx.InsertRequest(RequestParams{MemberID: ptr(id(2)), RequestedByID: id(1), Request: model.Request{ID: id(41), Team: "team",
			Kind: model.RequestKindStopMember, Status: model.RequestStatusFailed, FinishedAt: &finished,
			Error: model.NewError(model.ErrorLaunchFailed, "launch failed").WithDetail("cause", "missing binary")}})
	})
	readTest(t, s, func(tx *Tx) error {
		inbox, _, err := tx.Inbox(id(2), model.InboxDirectionIn, 20, "")
		if err != nil || len(inbox) != 1 {
			t.Fatalf("delivered inbox = %d, %v", len(inbox), err)
		}
		both, _, err := tx.Inbox(id(2), model.InboxDirectionBoth, 1, "")
		if err != nil || len(both) != 1 {
			t.Fatalf("both inbox = %d, %v", len(both), err)
		}
		page, more, err := tx.Inbox(id(2), model.InboxDirectionBoth, 1, both[0].ID)
		if err != nil || more || len(page) != 1 || page[0].ID != messageID {
			t.Fatalf("before page = %#v, %v, %v", page, more, err)
		}
		deliveries, err := tx.ListTeamDeliveries("team", model.DeliveryStateDelivered)
		if err != nil || len(deliveries) != 2 {
			t.Fatalf("team deliveries = %d, %v", len(deliveries), err)
		}
		count, err := tx.CountDeliveries("team", model.DeliveryStateDelivered)
		if err != nil || count != 2 {
			t.Fatalf("delivery count = %d, %v", count, err)
		}
		request, err := tx.GetRequest(id(40))
		if err != nil {
			return err
		}
		if request.Status != model.RequestStatusSucceeded || request.FinishedAt == nil {
			t.Fatalf("request = %#v", request)
		}
		failed, err := tx.GetRequest(id(41))
		if err != nil {
			return err
		}
		if failed.Error == nil || failed.Error.Details["cause"] != "missing binary" {
			t.Fatalf("failed request error = %#v", failed.Error)
		}
		return nil
	})
}

func TestEventReplayAndTimeline(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	writeTest(t, s, func(tx *Tx) error {
		for i, typ := range []model.EventType{model.EventTeamApplied, model.EventMessageCreated, model.EventMemberAdded, model.EventBudgetChanged} {
			teamID := "team"
			var team *string
			if i != 3 {
				team = &teamID
			}
			seq, err := tx.AppendEvent(Event{TeamID: team, Type: typ, Timeline: i != 2, Data: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i))})
			if err != nil || seq != int64(i+1) {
				t.Fatalf("append seq = %d, %v", seq, err)
			}
		}
		return nil
	})
	readTest(t, s, func(tx *Tx) error {
		all, err := tx.ReplayEvents(0, nil, 0)
		if err != nil {
			return err
		}
		team, err := tx.ReplayEvents(0, ptr("team"), 0)
		if err != nil {
			return err
		}
		after, err := tx.ReplayEvents(2, nil, 0)
		if err != nil {
			return err
		}
		current, err := tx.CurrentEventSeq()
		if err != nil {
			return err
		}
		if len(all) != 4 || len(team) != 3 || len(after) != 2 || after[0].Seq != 3 || current != 4 {
			t.Fatalf("replay = %d, %d, %d, %d", len(all), len(team), len(after), current)
		}
		tail, err := tx.Timeline("team", 0, 0, 2)
		if err != nil {
			return err
		}
		if len(tail.Events) != 2 || tail.Events[0].Seq != 1 || tail.Events[1].Seq != 2 || tail.HasMoreBefore || tail.HasMoreAfter {
			t.Fatalf("tail = %#v", tail)
		}
		newer, err := tx.Timeline("team", 0, 1, 2)
		if err != nil {
			return err
		}
		if len(newer.Events) != 1 || newer.Events[0].Seq != 2 || newer.HasMoreAfter || !newer.HasMoreBefore {
			t.Fatalf("newer = %#v", newer)
		}
		older, err := tx.Timeline("team", 2, 0, 2)
		if err != nil {
			return err
		}
		if len(older.Events) != 1 || older.Events[0].Seq != 1 || older.HasMoreBefore || !older.HasMoreAfter {
			t.Fatalf("older = %#v", older)
		}
		event, err := tx.GetEvent(4)
		if err != nil || event.TeamID != nil {
			t.Fatalf("host event = %#v, %v", event, err)
		}
		return nil
	})
}

func TestReadSnapshotAndContext(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Read(ctx, func(tx *Tx) error { return nil }); err == nil {
		t.Fatal("cancelled read succeeded")
	}
}
