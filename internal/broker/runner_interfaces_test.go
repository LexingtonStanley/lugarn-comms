package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"testing"
)

func TestOutcomeStatus(t *testing.T) {
	for _, tt := range []struct {
		o       TurnOutcome
		timeout bool
		want    model.TurnStatus
	}{
		{TurnOutcome{Acknowledged: true, ExitCode: ptr(int64(0))}, false, model.TurnStatusSucceeded},
		{TurnOutcome{ExitCode: ptr(int64(0))}, false, model.TurnStatusFailed},
		{TurnOutcome{Acknowledged: true, ExitCode: ptr(int64(1))}, false, model.TurnStatusFailed},
		{TurnOutcome{Cancelled: true}, false, model.TurnStatusCancelled},
		{TurnOutcome{Cancelled: true}, true, model.TurnStatusTimedOut},
	} {
		if got := OutcomeStatus(tt.o, tt.timeout); got != tt.want {
			t.Fatalf("%+v: %s want %s", tt, got, tt.want)
		}
	}
}
func TestDefaultRunnerDrivers(t *testing.T) {
	d := NotBuiltRunnerDrivers{}
	ctx := context.Background()
	_, err := d.Start(ctx, TurnLaunch{}, "prompt")
	code(t, err, model.ErrorInternal)
	if model.AsError(err).Details["reason"] != "not_built" {
		t.Fatal(err)
	}
	code(t, d.Execute(ctx, model.Request{}), model.ErrorInternal)
	code(t, d.Open(ctx, model.Member{}, model.LeaseCauseStart), model.ErrorInternal)
}
