package model

import "encoding/json"

// Record arrays and objects are non-nullable even when empty. Normalize only
// local copies so marshaling never mutates a shared record.

func (m Member) MarshalJSON() ([]byte, error) {
	type plain Member
	if m.Sessions == nil {
		m.Sessions = []MemberSession{}
	}
	return json.Marshal(plain(m))
}

func (m Message) MarshalJSON() ([]byte, error) {
	type plain Message
	if m.Deliveries == nil {
		m.Deliveries = []Delivery{}
	}
	return json.Marshal(plain(m))
}

func (t Turn) MarshalJSON() ([]byte, error) {
	type plain Turn
	if t.Consumed == nil {
		t.Consumed = []string{}
	}
	if t.AutoReplies == nil {
		t.AutoReplies = []string{}
	}
	return json.Marshal(plain(t))
}

func (r Request) MarshalJSON() ([]byte, error) {
	type plain Request
	if r.Args == nil {
		r.Args = map[string]any{}
	}
	return json.Marshal(plain(r))
}

func (s TeamStatus) MarshalJSON() ([]byte, error) {
	type plain TeamStatus
	if s.Rounds == nil {
		s.Rounds = []Rounds{}
	}
	return json.Marshal(plain(s))
}
