package startup

import "pi-goui/internal/core/session"

// SessionFile gives explicit paths priority and selects the most recently
// modified native session in the current project only when requested.
func SessionFile(agentDir, cwd, explicit string, resume bool) (string, error) {
	if explicit != "" || !resume {
		return explicit, nil
	}
	sessions, err := session.List(agentDir, cwd)
	if err != nil || len(sessions) == 0 {
		return "", err
	}
	return sessions[0].Path, nil
}
