// Package active reads and writes the install root's active-release state.
//
// The state lives in <root>/current.json on every platform:
//
//	{"active_version": "0.2.0", "previous_version": "dev"}
//
// The file only ever names versions, never paths. A release directory is
// always derived as <root>/releases/<version> from a validated version, so a
// tampered state file cannot point the launcher at an arbitrary executable.
package active

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// StateFile is the name of the state file within the install root.
const StateFile = "current.json"

// maxVersionLen bounds version identifiers; real ones are far shorter.
const maxVersionLen = 64

// State is the on-disk format of current.json.
type State struct {
	ActiveVersion   string `json:"active_version"`
	PreviousVersion string `json:"previous_version,omitempty"`
}

// Load reads and validates <root>/current.json.
func Load(root string) (State, error) {
	file := filepath.Join(root, StateFile)
	data, err := os.ReadFile(file)
	if err != nil {
		return State{}, fmt.Errorf("read active release state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("malformed %s: %w", file, err)
	}
	if err := ValidateVersion(s.ActiveVersion); err != nil {
		return State{}, fmt.Errorf("%s: active_version: %w", file, err)
	}
	if s.PreviousVersion != "" {
		if err := ValidateVersion(s.PreviousVersion); err != nil {
			return State{}, fmt.Errorf("%s: previous_version: %w", file, err)
		}
	}
	return s, nil
}

// Save atomically replaces <root>/current.json with s. A reader sees either
// the old state or the new one, never a partially written file.
func Save(root string, s State) error {
	if err := ValidateVersion(s.ActiveVersion); err != nil {
		return fmt.Errorf("active_version: %w", err)
	}
	if s.PreviousVersion != "" {
		if err := ValidateVersion(s.PreviousVersion); err != nil {
			return fmt.Errorf("previous_version: %w", err)
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(root, StateFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("save active release state: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("save active release state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("save active release state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save active release state: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(root, StateFile)); err != nil {
		return fmt.Errorf("save active release state: %w", err)
	}
	return nil
}

// ReleaseDir returns <root>/releases/<version> after checking that version is
// a safe identifier and that the directory exists as a real directory (not a
// symlink that could lead outside releases/).
func ReleaseDir(root, version string) (string, error) {
	if err := ValidateVersion(version); err != nil {
		return "", err
	}
	dir := filepath.Join(root, "releases", version)
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("release %s: %w", version, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("release %s: %s is not a directory", version, dir)
	}
	return dir, nil
}

// ValidateVersion reports whether v is usable as a single path component:
// non-empty, bounded, and limited to [0-9A-Za-z.+-_], excluding "." and "..".
// This admits "dev" and semantic versions such as "1.2.3-rc.1+build.5".
func ValidateVersion(v string) error {
	if v == "" {
		return errors.New("version is empty")
	}
	if len(v) > maxVersionLen {
		return fmt.Errorf("version %.20q... is longer than %d bytes", v, maxVersionLen)
	}
	if v == "." || v == ".." {
		return fmt.Errorf("invalid version %q", v)
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '.', r == '-', r == '+', r == '_':
		default:
			return fmt.Errorf("invalid version %q: unexpected character %q", v, r)
		}
	}
	return nil
}
