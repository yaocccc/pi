package extension

import "context"

// ReleaseStatus is a point-in-time observation, not a session lease. The host
// must stop admitting operations and recheck the originating client/idle state
// before closing it. Known=false allows the host's native Pi state fallback.
type ReleaseStatus struct {
	Known   bool
	Idle    bool
	Reasons []string
}

// Pending observes frontend extension requests already admitted by this tab.
// Capability-less sessions remain usable without implementing this hook.
func (s Sessions) Pending() bool {
	for _, session := range s {
		if activity, ok := session.(interface{ Pending() bool }); ok && activity.Pending() {
			return true
		}
	}
	return false
}

func InspectorPending(inspector Inspector) bool {
	activity, ok := inspector.(interface{ Pending() bool })
	return ok && activity.Pending()
}

func (r *Runtime) CheckRelease(ctx context.Context, client Client) (ReleaseStatus, error) {
	result := ReleaseStatus{Idle: true}
	for _, capability := range r.capabilities {
		if capability.CheckRelease == nil {
			continue
		}
		status, err := capability.CheckRelease(ctx, client)
		if err != nil {
			return status, err
		}
		if !status.Known {
			continue
		}
		result.Known = true
		result.Idle = result.Idle && status.Idle
		result.Reasons = append(result.Reasons, status.Reasons...)
	}
	return result, nil
}
