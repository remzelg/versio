package active

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "releases", "0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeState(t *testing.T, root, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, StateFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadValid(t *testing.T) {
	root := newRoot(t)
	writeState(t, root, `{"active_version":"0.1.0","previous_version":"dev"}`)
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := (State{ActiveVersion: "0.1.0", PreviousVersion: "dev"}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadMissing(t *testing.T) {
	root := newRoot(t)
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), StateFile) {
		t.Fatalf("expected error naming %s, got %v", StateFile, err)
	}
}

func TestLoadRejectsBadState(t *testing.T) {
	cases := map[string]string{
		"malformed":        `{"active_version":`,
		"not object":       `[]`,
		"missing active":   `{}`,
		"traversal":        `{"active_version":"../../etc"}`,
		"separator":        `{"active_version":"a/b"}`,
		"backslash":        `{"active_version":"a\\b"}`,
		"dotdot":           `{"active_version":".."}`,
		"bad previous":     `{"active_version":"0.1.0","previous_version":"../x"}`,
		"absolute version": `{"active_version":"/tmp"}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			root := newRoot(t)
			writeState(t, root, data)
			if got, err := Load(root); err == nil {
				t.Fatalf("expected error, got %+v", got)
			}
		})
	}
}

func TestSaveRoundTrip(t *testing.T) {
	root := newRoot(t)
	want := State{ActiveVersion: "0.2.0", PreviousVersion: "0.1.0"}
	if err := Save(root, want); err != nil {
		t.Fatal(err)
	}
	// Overwrite to exercise replacing an existing file.
	want = State{ActiveVersion: "0.3.0", PreviousVersion: "0.2.0"}
	if err := Save(root, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), StateFile+".tmp-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestSaveRejectsInvalidVersion(t *testing.T) {
	root := newRoot(t)
	if err := Save(root, State{ActiveVersion: "../x"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(root, StateFile)); !os.IsNotExist(err) {
		t.Fatalf("state file should not exist, stat err = %v", err)
	}
}

func TestReleaseDir(t *testing.T) {
	root := newRoot(t)
	got, err := ReleaseDir(root, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "releases", "0.1.0"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, err := ReleaseDir(root, "9.9.9"); err == nil {
		t.Error("missing release: expected error")
	}
	if _, err := ReleaseDir(root, "../releases/0.1.0"); err == nil {
		t.Error("traversal: expected error")
	}
}

func TestReleaseDirRejectsFile(t *testing.T) {
	root := newRoot(t)
	if err := os.WriteFile(filepath.Join(root, "releases", "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReleaseDir(root, "file"); err == nil {
		t.Fatal("expected error")
	}
}

func TestReleaseDirRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := newRoot(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "releases", "evil")); err != nil {
		t.Fatal(err)
	}
	if got, err := ReleaseDir(root, "evil"); err == nil {
		t.Fatalf("expected error, got %q", got)
	}
}

func TestValidateVersion(t *testing.T) {
	for _, v := range []string{"dev", "0.1.0", "1.2.3-rc.1+build.5", "v2_beta"} {
		if err := ValidateVersion(v); err != nil {
			t.Errorf("%q: unexpected error %v", v, err)
		}
	}
	for _, v := range []string{"", ".", "..", "a/b", `a\b`, "a b", "1.0\x00", strings.Repeat("9", maxVersionLen+1)} {
		if err := ValidateVersion(v); err == nil {
			t.Errorf("%q: expected error", v)
		}
	}
}
