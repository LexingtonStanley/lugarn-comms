package broker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type failingStepTeardown struct{}

func (failingStepTeardown) Close(context.Context, store.Team, []model.Member) error {
	return teardownFailures{
		{"kill runner tree pid 20304", model.NewError(model.ErrorInternal, "cannot terminate process")},
		{"remove bridge example-dir", &os.PathError{Op: "remove", Path: "example-dir", Err: errors.New("private stderr sentinel")}},
	}
}
func TestStopTeamPreservesTeardownStepAndTarget(t *testing.T) {
	b, _ := setup(t)
	b.opts.Teardown = failingStepTeardown{}
	out, e := b.StopTeam(context.Background(), wire.StopTeamRequest{Team: "t"})
	failure := model.AsError(e)
	if failure == nil || failure.Code != model.ErrorInternal {
		t.Fatal(failure)
	}
	raw, err := json.Marshal(failure)
	must(t, err)
	cause := string(raw)
	if strings.Contains(cause, "private stderr sentinel") {
		t.Fatal("teardown payload leaked")
	}
	for _, target := range []string{"kill runner tree pid 20304", "remove bridge example-dir"} {
		if !strings.Contains(cause, target) {
			t.Fatal("lost teardown context at app boundary", failure)
		}
	}
	if out.Team.State != model.TeamStateStopped {
		t.Fatal("cleanup stopped on an external failure", out)
	}
}
