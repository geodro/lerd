package idle

import "time"

// Action is what the engine should do for a site at one evaluation.
type Action int

const (
	ActionNone Action = iota
	ActionSuspend
	ActionResume
)

func (a Action) String() string {
	switch a {
	case ActionSuspend:
		return "suspend"
	case ActionResume:
		return "resume"
	default:
		return "none"
	}
}

// Decide returns the action for a site given its resolved idle-suspend policy
// and current state. It is pure so the suspend/resume rules can be exhaustively
// tested without touching workers or the clock.
//
//	disabled + suspended   -> resume   (feature turned off; restore what we stopped)
//	disabled               -> none
//	no activity record yet  -> none     (startup grace; never suspend a site we've
//	                                      not yet observed)
//	idle >= timeout, up      -> suspend
//	idle <  timeout, down    -> resume   (activity returned; backstops OnActivity)
//	otherwise                -> none
func Decide(enabled bool, timeout, idleFor time.Duration, hasRecord, suspended bool) Action {
	if !enabled {
		if suspended {
			return ActionResume
		}
		return ActionNone
	}
	if !hasRecord {
		return ActionNone
	}
	if idleFor >= timeout && !suspended {
		return ActionSuspend
	}
	if idleFor < timeout && suspended {
		return ActionResume
	}
	return ActionNone
}
