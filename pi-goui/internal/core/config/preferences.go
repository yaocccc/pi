package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

func savePreference(agentDir, key string, value any) (err error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
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
	prefs[key] = raw
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
