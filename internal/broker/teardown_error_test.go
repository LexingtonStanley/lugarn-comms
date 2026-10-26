package broker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type failingStepTeardown struct{}

func (failingStepTeardown) Close(context.Context, store.Team, []model.Member) error {
	return errors.Join(fmt.Errorf("kill runner tree pid 20304: %w", model.NewError(model.ErrorInternal, "Access is denied.")), fmt.Errorf("remove bridge example-dir: %w", errors.New("sharing violation")))
}
func TestStopTeamPreservesTeardownStepAndTarget(t *testing.T) {
	b, _ := setup(t)
	b.opts.Teardown = failingStepTeardown{}
	out, e := b.StopTeam(context.Background(), wire.StopTeamRequest{Team: "t"})
	failure := model.AsError(e)
	if failure == nil || failure.Code != model.ErrorInternal {
		t.Fatal(failure)
	}
	cause, _ := failure.Details["cause"].(string)
	for _, target := range []string{"kill runner tree pid 20304", "remove bridge example-dir"} {
		if !strings.Contains(cause, target) {
			t.Fatal("lost teardown context at app boundary", failure)
		}
	}
	if out.Team.State != model.TeamStateStopped {
		t.Fatal("cleanup stopped on an external failure", out)
	}
}
