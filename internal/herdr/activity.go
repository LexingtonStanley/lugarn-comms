package herdr

import "github.com/LexingtonStanley/lugarn-comms/internal/model"

// ActivityFromHerdr maps Herdr's agent_status to the protocol's Activity.
// Herdr 0.9.5 reports exactly the protocol's five values. Anything else,
// including an empty status or a value a later Herdr adds, is unknown, which
// the runner never treats as ready.
func ActivityFromHerdr(status string) model.Activity {
	switch model.Activity(status) {
	case model.ActivityIdle, model.ActivityWorking, model.ActivityBlocked, model.ActivityDone:
		return model.Activity(status)
	default:
		return model.ActivityUnknown
	}
}
