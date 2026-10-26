package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func (l *integrationLifecycle) toHeadless(context.Context, model.Member, model.LeaseCause, bool) error {
	return runnerNotBuilt()
}
func (l *integrationLifecycle) stopMember(context.Context, model.Member, model.StopReason, model.LeaseCause) error {
	return runnerNotBuilt()
}
