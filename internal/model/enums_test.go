package model

import (
	"reflect"
	"testing"
)

type protocolEnum[T any] interface {
	~string
	Values() []T
	Valid() bool
}

func checkEnum[T protocolEnum[T]](t *testing.T) []string {
	t.Helper()
	var zero T
	values := zero.Values()
	result := make([]string, len(values))
	for i, v := range values {
		if !v.Valid() {
			t.Errorf("defined value %q is invalid", v)
		}
		result[i] = string(v)
	}
	if zero.Valid() || T("not_a_protocol_value").Valid() {
		t.Error("undefined enum value is valid")
	}
	if len(values) > 0 {
		values[0] = T("changed")
		if reflect.DeepEqual(values, zero.Values()) {
			t.Error("Values exposes shared mutable storage")
		}
	}
	return result
}

func TestEnumSchemaParity(t *testing.T) {
	checks := map[string]func(*testing.T) []string{
		"Harness":         checkEnum[Harness],
		"Profile":         checkEnum[Profile],
		"Mode":            checkEnum[Mode],
		"Driver":          checkEnum[Driver],
		"Pattern":         checkEnum[Pattern],
		"Role":            checkEnum[Role],
		"GridCapability":  checkEnum[GridCapability],
		"MemberKind":      checkEnum[MemberKind],
		"MemberOrigin":    checkEnum[MemberOrigin],
		"MemberState":     checkEnum[MemberState],
		"StopReason":      checkEnum[StopReason],
		"RemovedReason":   checkEnum[RemovedReason],
		"Activity":        checkEnum[Activity],
		"SessionSource":   checkEnum[SessionSource],
		"TeamState":       checkEnum[TeamState],
		"RunnerState":     checkEnum[RunnerState],
		"MessageKind":     checkEnum[MessageKind],
		"DeliveryState":   checkEnum[DeliveryState],
		"DeliveryReason":  checkEnum[DeliveryReason],
		"DeliveryVia":     checkEnum[DeliveryVia],
		"TurnStatus":      checkEnum[TurnStatus],
		"RequestKind":     checkEnum[RequestKind],
		"RequestStatus":   checkEnum[RequestStatus],
		"LeaseCause":      checkEnum[LeaseCause],
		"LaunchEndReason": checkEnum[LaunchEndReason],
		"BudgetKind":      checkEnum[BudgetKind],
		"BudgetCause":     checkEnum[BudgetCause],
		"PolicyAction":    checkEnum[PolicyAction],
		"SpawnFolder":     checkEnum[SpawnFolder],
		"SpawnCap":        checkEnum[SpawnCap],
		"InboxDirection":  checkEnum[InboxDirection],
		"ResolveAction":   checkEnum[ResolveAction],
		"WarningCode":     checkEnum[WarningCode],
		"HostOS":          checkEnum[HostOS],
		"HostArch":        checkEnum[HostArch],
		"HarnessIssue":    checkEnum[HarnessIssue],
		"AuthFailure":     checkEnum[AuthFailure],
		"Face":            checkEnum[Face],
		"EventType":       checkEnum[EventType],
		"ErrorCode":       checkEnum[ErrorCode],
	}
	schema := readSchema(t, "common")
	for name, def := range schema.Defs {
		if len(def.Enum) == 0 {
			continue
		}
		check, ok := checks[name]
		if !ok {
			t.Errorf("schema enum %s has no Go type", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			if got := check(t); !reflect.DeepEqual(got, def.Enum) {
				t.Errorf("Values() = %q, schema = %q", got, def.Enum)
			}
		})
		delete(checks, name)
	}
	for name := range checks {
		t.Errorf("Go enum %s has no schema enum", name)
	}
}
