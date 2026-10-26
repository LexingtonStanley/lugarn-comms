package broker

import (
	"encoding/json"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (r *Runner) prompt(tx *store.Tx, t *store.Team, m model.Member, items []store.DeliveryItem, seal string) (string, error) {
	edges, e := r.b.grid(tx, t.ID)
	if e != nil {
		return "", e
	}
	grid := policy.NewGrid(edges)
	needsBrief := m.Session == nil
	if m.Session != nil {
		s, e := tx.GetMemberSession(m.ID, *m.Session)
		if e != nil {
			return "", e
		}
		needsBrief = s.BriefedAt == nil
	}
	var brief *string
	if needsBrief {
		stored, e := tx.GetStoredMember(m.ID)
		if e != nil {
			return "", e
		}
		live, e := tx.ListMembers(t.ID, false)
		if e != nil {
			return "", e
		}
		cfg, e := tx.GetSpawnSettings(m.ID)
		if e != nil && !missing(e) {
			return "", e
		}
		rendered := policy.RenderBrief(*stored.Brief, policy.BriefFacts{Member: m, Team: t.Name, Live: live, Grid: grid, CanSpawn: cfg != nil && cfg.Enabled && m.Origin != model.MemberOriginSpawn})
		brief = &rendered
	}
	messages := make([]policy.PromptMessage, 0, len(items))
	for _, item := range items {
		message, e := tx.GetMessage(item.MessageID)
		if e != nil {
			return "", e
		}
		from, e := tx.GetMember(message.From.ID)
		if e != nil {
			return "", e
		}
		messages = append(messages, policy.PromptMessage{Seq: item.MessageSeq, ID: message.ID, From: *from, Text: message.Text})
	}
	return policy.Prompt(policy.PromptInput{Team: t.Name, To: m, Seal: seal, Brief: brief, Messages: messages, Grid: grid}), nil
}
func (r *Runner) autoReplies(tx *store.Tx, t *store.Team, w *runnerTurn, out TurnOutcome) error {
	if t.State != model.TeamStateRunning || out.Text == "" {
		return nil
	}
	events, e := tx.ReplayEvents(w.seq, &t.ID, 0)
	if e != nil {
		return e
	}
	sent := map[string]bool{}
	for _, event := range events {
		if event.Type != model.EventMessageCreated {
			continue
		}
		var data wire.MessageCreatedEventData
		if e = json.Unmarshal(event.Data, &data); e != nil {
			return e
		}
		m := data.Message
		if m.From.ID == w.member.ID && m.Turn != nil && *m.Turn == w.turn.ID && m.Kind != model.MessageKindAutoReply {
			for _, d := range m.Deliveries {
				if d.State != model.DeliveryStateDenied {
					sent[d.To.ID] = true
				}
			}
		}
	}
	// Keep sender order by first appearance, but answer its newest consumed message.
	order := []string{}
	newest := map[string]model.Message{}
	for _, item := range w.items {
		m, e := tx.GetMessage(item.MessageID)
		if e != nil {
			return e
		}
		if _, ok := newest[m.From.ID]; !ok {
			order = append(order, m.From.ID)
		}
		newest[m.From.ID] = *m
	}
	from, e := tx.GetMember(w.member.ID)
	if e != nil {
		return e
	}
	for _, id := range order {
		if sent[id] {
			continue
		}
		original := newest[id]
		to, e := tx.GetMember(id)
		if e != nil {
			return e
		}
		if to.RemovedAt != nil {
			continue
		}
		_, _, refusal, e := r.b.accept(tx, t, *from, to, false, out.Text, &original.ID, model.MessageKindAutoReply)
		if e != nil {
			return e
		}
		if refusal != nil {
			return refusal
		}
	}
	return nil
}
