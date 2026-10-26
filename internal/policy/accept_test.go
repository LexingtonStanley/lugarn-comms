package policy

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// acceptTeam: human, lead (orchestrator) and its spawned worker api, a pi
// stage, and grid lead → api message+wake, api → lead message.
func acceptTeam() (members []model.Member, g Grid) {
	lead := agent("L", "lead", model.RoleOrchestrator, model.HarnessClaude, mode(model.ModePane))
	members = []model.Member{
		humanMember("H"),
		lead,
		agent("A", "api", model.RoleWorker, model.HarnessCodex, spawnedBy(lead), state(model.MemberStateStopped)),
		agent("P", "stage", model.RoleStage, model.HarnessPi, state(model.MemberStateStarting)),
		agent("S", "sleeper", model.RoleCustom, model.HarnessClaude, state(model.MemberStateStopped)),
		agent("G", "gone", model.RoleCustom, model.HarnessClaude, removed()),
	}
	return members, NewGrid(SpawnEdges("lead", "api"))
}

func input(members []model.Member, g Grid, from, to string, kind model.MessageKind, text string) MessageInput {
	in := MessageInput{ID: "01JB0000000000000000000000", Kind: kind, Live: members, Text: text, Grid: g,
		Budget: BudgetState{Hops: model.Hops{Allowance: 16, Base: 16}, ReviewRounds: 3}}
	for i := range members {
		if members[i].Name == from {
			in.From = members[i]
		}
		if members[i].Name == to {
			in.To = &members[i]
		}
	}
	if to == AllName {
		in.All = true
	}
	return in
}

func TestResolveRecipient(t *testing.T) {
	members, _ := acceptTeam()
	to, all, err := ResolveRecipient(members, "api")
	requireNoError(t, err)
	equal(t, "api", [2]any{to.ID, all}, [2]any{"A", false})
	to, all, err = ResolveRecipient(members, "human")
	requireNoError(t, err)
	equal(t, "human", to.ID, "H")
	to, all, err = ResolveRecipient(members, "all")
	equal(t, "all", [3]any{to == nil, all, err == nil}, [3]any{true, true, true})
	for _, name := range []string{"nobody", "gone", "API", ""} {
		_, _, err = ResolveRecipient(members, name)
		requireCode(t, err, model.ErrorUnknownMember)
	}
}

func TestAcceptRefusals(t *testing.T) {
	members, g := acceptTeam()
	big := strings.Repeat("x", 65537)
	cases := []struct {
		name string
		edit func(*MessageInput)
		code model.ErrorCode
		msg  string
	}{
		{"reply to a message not received", func(in *MessageInput) { in.Kind, in.ReplyTo = rep, ptr("01JBX") },
			model.ErrorNotFound, "01JBX isn't a message you received"},
		{"send with an unknown reply_to", func(in *MessageInput) { in.ReplyTo = ptr("01JBX") },
			model.ErrorNotFound, "01JBX isn't a message you received"},
		{"the person's reply to an unknown message", func(in *MessageInput) {
			in.From, in.ReplyTo = members[0], ptr("01JBX")
		}, model.ErrorNotFound, "no message 01JBX in this team"},
		{"unknown kind", func(in *MessageInput) { in.Kind = "note" }, model.ErrorInvalidRequest, ""},
		{"reply without reply_to", func(in *MessageInput) { in.Kind = rep }, model.ErrorInvalidRequest, ""},
		{"auto-reply without reply_to", func(in *MessageInput) { in.Kind = auto }, model.ErrorInvalidRequest, ""},
		{"task with reply_to", func(in *MessageInput) { in.Kind, in.ReplyTo, in.ReplyFound = task, ptr("01JBX"), true },
			model.ErrorInvalidRequest, ""},
		{"to self", func(in *MessageInput) { in.To = &members[1] }, model.ErrorInvalidRequest, "to: you can't message yourself"},
		{"the person to self", func(in *MessageInput) { in.From, in.To = members[0], &members[0] }, model.ErrorInvalidRequest, ""},
		{"an agent to all", func(in *MessageInput) { in.To, in.All = nil, true }, model.ErrorInvalidRequest,
			"to: only the person can send to all"},
		{"no recipient", func(in *MessageInput) { in.To = nil }, model.ErrorInvalidRequest, ""},
		{"empty text", func(in *MessageInput) { in.Text = "" }, model.ErrorInvalidRequest, ""},
		{"invalid UTF-8", func(in *MessageInput) { in.Text = "ok \xff" }, model.ErrorInvalidRequest, ""},
		{"over 65,536 code points", func(in *MessageInput) { in.Text = big }, model.ErrorInvalidRequest,
			"text is capped at 65536 characters"},
		{"a task over 65,536", func(in *MessageInput) { in.Kind, in.Text = task, big }, model.ErrorInvalidRequest, ""},
		{"a send to human over 8,000", func(in *MessageInput) { in.To, in.Text = &members[0], strings.Repeat("é", 8001) },
			model.ErrorInvalidRequest, "messages to human are capped at 8000 characters: shorten it and send again"},
		{"a reply to human over 8,000", func(in *MessageInput) {
			in.To, in.Kind, in.ReplyTo, in.ReplyFound, in.Text = &members[0], rep, ptr("01JBX"), true, strings.Repeat("é", 8001)
		}, model.ErrorInvalidRequest, ""},
		{"a broadcast nobody can receive", func(in *MessageInput) {
			in.From, in.To, in.All = members[0], nil, true
			in.Live = []model.Member{members[0], members[4], members[5]}
		}, model.ErrorInvalidRequest, "all: no member is starting or running to receive it"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := input(members, g, "lead", "api", msg, "hello")
			c.edit(&in)
			_, err := Accept(in)
			requireCode(t, err, c.code)
			if c.msg != "" {
				equal(t, "message", err.Message, c.msg)
			}
		})
	}
}

func TestAcceptTextCaps(t *testing.T) {
	members, g := acceptTeam()
	// Code points, not bytes: é is 2 bytes, 😀 is 4.
	for _, unit := range []string{"a", "é", "😀"} {
		for _, c := range []struct {
			to    string
			kind  model.MessageKind
			n     int
			ok    bool
			limit int
		}{
			{"human", msg, 8000, true, 8000},
			{"human", msg, 8001, false, 8000},
			{"human", rep, 8000, true, 8000},
			{"human", auto, 8000, true, 8000},
			{"human", auto, 8001, true, 8000},
			{"api", msg, 65536, true, 65536},
			{"api", msg, 65537, false, 65536},
			{"api", task, 65536, true, 65536},
			{"api", auto, 65537, true, 65536},
			{"api", auto, 200000, true, 65536},
		} {
			in := input(members, g, "lead", c.to, c.kind, strings.Repeat(unit, c.n))
			if c.kind != msg && c.kind != task {
				in.ReplyTo, in.ReplyFound = ptr("01JBX"), true
			}
			plan, err := Accept(in)
			if !c.ok {
				requireCode(t, err, model.ErrorInvalidRequest)
				equal(t, "details", err.Details, map[string]any{"field": "text", "limit": c.limit, "length": c.n})
				continue
			}
			requireNoError(t, err)
			got := utf8.RuneCountInString(plan.Text)
			if got > c.limit {
				t.Fatalf("%s %s of %d %q: stored %d code points", c.to, c.kind, c.n, unit, got)
			}
			if c.n <= c.limit && plan.Text != in.Text {
				t.Fatalf("%s %s of %d %q was changed", c.to, c.kind, c.n, unit)
			}
			if c.n > c.limit && !strings.HasSuffix(plan.Text, " more characters in the transcript]") {
				t.Fatalf("%s %s of %d %q not cut", c.to, c.kind, c.n, unit)
			}
		}
	}
}

func TestCutText(t *testing.T) {
	equal(t, "within", CutText("short", 8000), "short")
	text := strings.Repeat("é", 11240)
	got := CutText(text, 8000)
	marker := "[… cut by Lugarn: 3298 more characters in the transcript]"
	equal(t, "golden", got, strings.Repeat("é", 7942)+"\n"+marker)
	equal(t, "length", utf8.RuneCountInString(got), 8000)

	// Across every count of digits, the kept prefix is the longest whose
	// marker still fits, and the marker counts what was left out.
	for _, total := range []int{8001, 8002, 8009, 8010, 8050, 8054, 8055, 8056, 8099, 8100, 8101, 8999, 9000, 9055, 9056, 17999, 18000, 18060, 108000, 1008000} {
		in := strings.Repeat("😀", total)
		out := CutText(in, 8000)
		body, mark, ok := strings.Cut(out, "\n")
		if !ok {
			t.Fatalf("%d: no marker line", total)
		}
		kept := utf8.RuneCountInString(body)
		if mark != cutMarker(total-kept) || !strings.HasPrefix(in, body) {
			t.Fatalf("%d: marker %q for %d kept", total, mark, kept)
		}
		if n := utf8.RuneCountInString(out); n > 8000 || kept+1+1+utf8.RuneCountInString(cutMarker(total-kept-1)) <= 8000 {
			t.Fatalf("%d: %d code points, %d kept: not the longest fit", total, n, kept)
		}
	}
}

func TestAcceptGridDenial(t *testing.T) {
	members, g := acceptTeam()
	// api may message lead, but stage may message nobody.
	in := input(members, g, "stage", "lead", msg, "hi")
	plan, err := Accept(in)
	requireNoError(t, err)
	equal(t, "outcome", plan.Outcome, OutcomeDenied)
	equal(t, "delivery", plan.Deliveries, []DeliveryPlan{{To: model.MemberRef{ID: "L", Name: "lead"},
		State: model.DeliveryStateDenied, Reason: ptr(model.DeliveryReasonGrid)}})
	requireCode(t, plan.Err, model.ErrorDenied)
	equal(t, "error", jsonOf(t, plan.Err), map[string]any{"code": "denied", "message": "stage may not message lead",
		"details": map[string]any{"action": "message", "message_id": "01JB0000000000000000000000"}})
	equal(t, "uncounted", [3]any{plan.Hop == nil, plan.Round == nil, plan.Budget.Hops.Used}, [3]any{true, true, int64(0)})
	equal(t, "events", len(plan.Events), 1)
	e := plan.Events[0]
	equal(t, "event", [4]any{e.Type, e.Timeline, *e.Member, *e.Message},
		[4]any{model.EventPolicyDenied, true, "P", "01JB0000000000000000000000"})
	equal(t, "event data", jsonOf(t, e.Data), map[string]any{
		"action": "message", "actor": map[string]any{"id": "P", "name": "stage"},
		"target": map[string]any{"id": "L", "name": "lead"}, "message_id": "01JB0000000000000000000000",
		"error": jsonOf(t, plan.Err),
	})

	// The same as an auto-reply is skipped: nothing stored, nothing written.
	in.Kind, in.ReplyTo, in.ReplyFound = auto, ptr("01JBX"), true
	plan, err = Accept(in)
	requireNoError(t, err)
	equal(t, "skipped", [4]any{plan.Outcome, plan.Err == nil, len(plan.Deliveries), len(plan.Events)},
		[4]any{OutcomeSkipped, true, 0, 0})

	// Replies go through the grid like any message.
	in.Kind = rep
	plan, _ = Accept(in)
	equal(t, "reply denied", plan.Outcome, OutcomeDenied)

	// human's implicit row and column need no edge.
	plan, _ = Accept(input(members, g, "stage", "human", msg, "hi"))
	equal(t, "to human", plan.Outcome, OutcomeAccepted)
	plan, _ = Accept(input(members, g, "human", "stage", msg, "hi"))
	equal(t, "from human", plan.Outcome, OutcomeAccepted)
	// The spawn edges: api may answer lead, lead may message api.
	plan, _ = Accept(input(members, g, "api", "lead", msg, "done"))
	equal(t, "api → lead", plan.Outcome, OutcomeAccepted)
}

func TestAcceptToHuman(t *testing.T) {
	members, g := acceptTeam()
	for recent, notify := range []bool{true, true, true, true, true, false, false} {
		in := input(members, g, "stage", "human", msg, "status")
		in.ToHumanRecent = recent
		in.Budget.Hops = model.Hops{Used: 20, Allowance: 16, Base: 16}
		plan, err := Accept(in)
		requireNoError(t, err)
		equal(t, "delivery", plan.Deliveries, []DeliveryPlan{{To: model.MemberRef{ID: "H", Name: "human"},
			State: model.DeliveryStateDelivered, Via: ptr(model.DeliveryViaHuman), Notify: ptr(notify)}})
		equal(t, "outside the budget", [4]any{plan.Hop == nil, plan.Round == nil, len(plan.Events), plan.Budget.Hops.Used},
			[4]any{true, true, 0, int64(20)})
	}
	// Notify is null on deliveries to anyone else.
	plan, _ := Accept(input(members, g, "lead", "api", msg, "go"))
	if plan.Deliveries[0].Notify != nil || plan.Deliveries[0].Via != nil {
		t.Fatal("notify and via are null for an agent recipient")
	}
	// An auto-reply to human over the cap is cut and still delivered.
	in := input(members, g, "api", "human", auto, strings.Repeat("y", 9000))
	in.ReplyTo, in.ReplyFound = ptr("01JBX"), true
	plan, err := Accept(in)
	requireNoError(t, err)
	equal(t, "cut", [2]any{utf8.RuneCountInString(plan.Text), plan.Deliveries[0].State}, [2]any{8000, model.DeliveryStateDelivered})
}

func TestAcceptWake(t *testing.T) {
	members, g := acceptTeam()
	cases := []struct {
		from, to       string
		kind           model.MessageKind
		hopsUsed       int64
		wake, willWake bool
		state          model.DeliveryState
	}{
		{"human", "api", msg, 0, true, true, model.DeliveryStateQueued},      // a person's send wakes a stopped member
		{"human", "lead", msg, 0, true, false, model.DeliveryStateQueued},    // running: nothing to wake
		{"human", "stage", task, 0, true, false, model.DeliveryStateQueued},  // starting
		{"lead", "api", msg, 0, true, true, model.DeliveryStateQueued},       // wake on the edge
		{"lead", "api", task, 0, true, true, model.DeliveryStateQueued},      // a spawn task
		{"api", "lead", msg, 0, false, false, model.DeliveryStateQueued},     // message only
		{"lead", "api", msg, 16, true, false, model.DeliveryStateHeld},       // held: it won't wake yet
		{"human", "sleeper", msg, 16, true, true, model.DeliveryStateQueued}, // the person's message resets the budget
	}
	for _, c := range cases {
		in := input(members, g, c.from, c.to, c.kind, "x")
		in.Budget.Hops.Used = c.hopsUsed
		plan, err := Accept(in)
		requireNoError(t, err)
		d := plan.Deliveries[0]
		if d.Wake != c.wake || d.WillWake != c.willWake || d.State != c.state {
			t.Errorf("%s → %s %s: wake %v will_wake %v %s; want %v %v %s", c.from, c.to, c.kind, d.Wake, d.WillWake, d.State,
				c.wake, c.willWake, c.state)
		}
	}
}

func TestAcceptBroadcast(t *testing.T) {
	members, g := acceptTeam()
	in := input(members, g, "human", AllName, msg, "Pause after your current step.")
	in.Budget.Hops = model.Hops{Used: 3, Allowance: 16, Base: 16}
	in.Budget.Rounds = []model.Rounds{{Writer: model.MemberRef{ID: "L", Name: "lead"}, Reviewer: model.MemberRef{ID: "P", Name: "stage"}, Used: 1, Allowance: 3}}
	plan, err := Accept(in)
	requireNoError(t, err)
	equal(t, "broadcast", [3]any{plan.Outcome, plan.Broadcast, plan.Kind}, [3]any{OutcomeAccepted, true, msg})
	// Running and starting agents; not stopped ones, removed ones or human.
	equal(t, "deliveries", plan.Deliveries, []DeliveryPlan{
		{To: model.MemberRef{ID: "L", Name: "lead"}, State: model.DeliveryStateQueued},
		{To: model.MemberRef{ID: "P", Name: "stage"}, State: model.DeliveryStateQueued},
	})
	equal(t, "resets", causes(plan.Events), []string{"hops:reset:human", "rounds:reset:human"})
	equal(t, "uncounted", [2]bool{plan.Hop == nil, plan.Round == nil}, [2]bool{true, true})
}

func TestAcceptKinds(t *testing.T) {
	members, g := acceptTeam()
	in := input(members, g, "human", "lead", msg, "Keep the fallback.")
	in.ReplyTo, in.ReplyFound = ptr("01JBX"), true
	plan, err := Accept(in)
	requireNoError(t, err)
	equal(t, "a send with reply_to is a reply", [2]any{plan.Kind, *plan.ReplyTo}, [2]any{rep, "01JBX"})
	plan, _ = Accept(input(members, g, "human", "lead", task, "Do it."))
	equal(t, "task", [2]any{plan.Kind, plan.ReplyTo == nil}, [2]any{task, true})
	plan, _ = Accept(input(members, g, "lead", "api", msg, "Go."))
	equal(t, "message", [3]any{plan.Kind, plan.ReplyTo == nil, plan.Broadcast}, [3]any{msg, true, false})
}
