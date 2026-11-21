package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func validateSidebarPosition(position string) error {
	if position != "left" && position != "right" {
		return fmt.Errorf("invalid sidebarPosition %q: expected left or right", position)
	}
	return nil
}

// SaveSidebarPosition updates the sidebar position in the frontend preferences.
func SaveSidebarPosition(agentDir, position string) error {
	if err := validateSidebarPosition(position); err != nil {
		return err
	}
	return savePreference(agentDir, "sidebarPosition", position)
}

// SaveResumeLastSession updates the startup preference for restoring the last session.
func SaveResumeLastSession(agentDir string, enabled bool) error {
	return savePreference(agentDir, "resumeLastSession", enabled)
}

// SaveFrontendPreferences commits only the supplied fields in one transaction.
func SaveFrontendPreferences(agentDir string, sidebar *string, resume *bool, themeName string) error {
	fields := make(map[string]any, 3)
	if themeName != "" {
		if _, err := LoadTheme(agentDir, themeName); err != nil {
			return err
		}
		fields["theme"] = CanonicalThemeName(themeName)
	}
	if sidebar != nil {
		if err := validateSidebarPosition(*sidebar); err != nil {
			return err
		}
		fields["sidebarPosition"] = *sidebar
	}
	if resume != nil {
		fields["resumeLastSession"] = *resume
	}
	return savePreferences(agentDir, fields)
}

func savePreference(agentDir, key string, value any) error {
	return savePreferences(agentDir, map[string]any{key: value})
}

func savePreferences(agentDir string, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	if err = os.MkdirAll(agentDir, 0700); err != nil {
		return err
	}
	path := filepath.Join(agentDir, "goui.json")
	release, err := lockFrontend(path + ".lock")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	prefs := map[string]json.RawMessage{}
	if err := readJSON(path, &prefs); err != nil {
		return err
	}
	if prefs == nil {
		prefs = map[string]json.RawMessage{}
	}
	for key, value := range fields {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		prefs[key] = raw
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(agentDir, ".goui-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

const frontendLockWait = 2 * time.Second

func retryFrontendLock(path string, try func() error, busy func(error) bool) error {
	deadline := time.Now().Add(frontendLockWait)
	for {
		err := try()
		if err == nil {
			return nil
		}
		remaining := time.Until(deadline)
		if !busy(err) || remaining <= 0 {
			return fmt.Errorf("acquiring preferences lock %s: %w", path, err)
		}
		time.Sleep(min(20*time.Millisecond, remaining))
	}
}
