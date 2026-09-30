package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/remycarr/versio/client/internal/active"
	"github.com/remycarr/versio/client/internal/releasetest"
	"github.com/remycarr/versio/client/release"
)

// fakeVersionEnv makes the test binary act as a versio payload that reports
// the variable's value for "--version".
const fakeVersionEnv = "VERSIO_FAKE_PAYLOAD_VERSION"

func TestMain(m *testing.M) {
	if v, ok := os.LookupEnv(fakeVersionEnv); ok && len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(v)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func config(h *releasetest.Host) Config {
	return Config{ManifestURL: h.ManifestURL(), PublicKey: h.PublicKey, Client: h.Client()}
}

// newInstall creates an install root whose active release is version.
func newInstall(t *testing.T, version string) (string, active.State) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "releases", version), 0o755); err != nil {
		t.Fatal(err)
	}
	s := active.State{ActiveVersion: version}
	if err := active.Save(root, s); err != nil {
		t.Fatal(err)
	}
	return root, s
}

func assertState(t *testing.T, root string, want active.State) {
	t.Helper()
	got, err := active.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("state = %+v, want %+v", got, want)
	}
}

// assertFails runs an update that must fail with wantErr and leave the
// install untouched.
func assertFails(t *testing.T, cfg Config, wantErr string) {
	t.Helper()
	root, cur := newInstall(t, "dev")
	res, err := Run(context.Background(), cfg, root, cur)
	if err == nil {
		t.Fatalf("expected error, got %+v", res)
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("error %q does not mention %q", err, wantErr)
	}
	assertState(t, root, cur)
	if _, err := os.Stat(filepath.Join(root, "releases", "0.2.0")); !os.IsNotExist(err) {
		t.Errorf("release directory should not exist, stat err = %v", err)
	}
}

func TestRunInstallsNewerRelease(t *testing.T) {
	t.Setenv(fakeVersionEnv, "0.2.0")
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	root, cur := newInstall(t, "dev")

	res, err := Run(context.Background(), config(h), root, cur)
	if err != nil {
		t.Fatal(err)
	}
	want := active.State{ActiveVersion: "0.2.0", PreviousVersion: "dev"}
	if !res.Updated || res.State != want {
		t.Fatalf("result = %+v", res)
	}
	assertState(t, root, want)

	bin := filepath.Join(root, "releases", "0.2.0", release.BinaryName(runtime.GOOS))
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "staging")); len(entries) != 0 {
		t.Errorf("staging not cleaned up: %v", entries)
	}
}

func TestRunSkipsWhenNotNewer(t *testing.T) {
	for _, current := range []string{"0.2.0", "0.3.0"} {
		t.Run(current, func(t *testing.T) {
			h := releasetest.NewHost(t)
			h.Publish(t, "0.2.0", releasetest.SelfZip(t))
			root, cur := newInstall(t, current)

			res, err := Run(context.Background(), config(h), root, cur)
			if err != nil {
				t.Fatal(err)
			}
			if res.Updated || res.State != cur {
				t.Fatalf("result = %+v", res)
			}
			if n := h.Requests(".zip"); n != 0 {
				t.Errorf("downloaded %d artifacts, want 0", n)
			}
		})
	}
}

func TestRunRejectsBadSignature(t *testing.T) {
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	sigPath := releasetest.ManifestPath + release.SignatureSuffix
	h.Set(sigPath, releasetest.Sign(otherPriv, h.Get(releasetest.ManifestPath)))

	assertFails(t, config(h), "signature")
	if n := h.Requests(".zip"); n != 0 {
		t.Errorf("downloaded %d artifacts before verifying the manifest", n)
	}
}

func TestRunRejectsTamperedManifest(t *testing.T) {
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	m := h.Get(releasetest.ManifestPath)
	h.Set(releasetest.ManifestPath, bytes.Replace(m, []byte("0.2.0"), []byte("0.2.1"), 1))
	assertFails(t, config(h), "signature")
}

func TestRunRejectsHashMismatch(t *testing.T) {
	t.Setenv(fakeVersionEnv, "0.2.0")
	h := releasetest.NewHost(t)
	archive := releasetest.SelfZip(t)
	h.Publish(t, "0.2.0", archive)
	tampered := bytes.Clone(archive)
	tampered[len(tampered)/2] ^= 0xff
	h.Set(releasetest.ArchivePath("0.2.0"), tampered)
	assertFails(t, config(h), "sha256")
}

func TestRunRejectsWrongSize(t *testing.T) {
	h := releasetest.NewHost(t)
	archive := releasetest.SelfZip(t)
	h.Publish(t, "0.2.0", archive)
	h.Set(releasetest.ArchivePath("0.2.0"), append(bytes.Clone(archive), 0))
	assertFails(t, config(h), "bytes")
}

func TestRunMissingPlatform(t *testing.T) {
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	cfg := config(h)
	cfg.GOOS, cfg.GOARCH = "plan9", "mips"
	assertFails(t, cfg, "plan9/mips")
}

func TestRunRejectsCandidateReportingWrongVersion(t *testing.T) {
	t.Setenv(fakeVersionEnv, "9.9.9")
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	assertFails(t, config(h), "reports version")
}

func TestRunRejectsMalformedArchives(t *testing.T) {
	bin := release.BinaryName(runtime.GOOS)
	self := releasetest.Self(t)
	cases := map[string][]byte{
		"not a zip":   []byte("definitely not a zip"),
		"wrong name":  releasetest.Zip(t, map[string][]byte{"other": self}),
		"nested":      releasetest.Zip(t, map[string][]byte{"dir/" + bin: self}),
		"extra entry": releasetest.Zip(t, map[string][]byte{bin: self, "README": []byte("hi")}),
	}
	for name, archive := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(fakeVersionEnv, "0.2.0")
			h := releasetest.NewHost(t)
			h.Publish(t, "0.2.0", archive)
			assertFails(t, config(h), "archive")
		})
	}
}

func TestRunManifestUnavailable(t *testing.T) {
	h := releasetest.NewHost(t)
	assertFails(t, config(h), "404")
}

func TestRunNotConfigured(t *testing.T) {
	h := releasetest.NewHost(t)
	cfg := config(h)
	cfg.PublicKey = nil
	assertFails(t, cfg, "not configured")
}

func TestRunReusesInstalledRelease(t *testing.T) {
	t.Setenv(fakeVersionEnv, "0.2.0")
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	root, cur := newInstall(t, "dev")
	// Simulate a launch that installed 0.2.0 but died before activating it.
	dir := filepath.Join(root, "releases", "0.2.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, release.BinaryName(runtime.GOOS)), releasetest.Self(t), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), config(h), root, cur)
	if err != nil || !res.Updated {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if n := h.Requests(".zip"); n != 0 {
		t.Errorf("downloaded %d artifacts, want 0", n)
	}
}

func TestRunDiscardsBrokenInstalledRelease(t *testing.T) {
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	root, cur := newInstall(t, "dev")
	dir := filepath.Join(root, "releases", "0.2.0")
	if err := os.MkdirAll(dir, 0o755); err != nil { // no binary inside
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), config(h), root, cur); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("broken release should be removed, stat err = %v", err)
	}
	assertState(t, root, cur)
}

func TestRunPrunesOldReleases(t *testing.T) {
	t.Setenv(fakeVersionEnv, "0.2.0")
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	root, _ := newInstall(t, "0.0.1")
	if err := os.MkdirAll(filepath.Join(root, "releases", "0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	cur := active.State{ActiveVersion: "0.1.0", PreviousVersion: "0.0.1"}
	if err := active.Save(root, cur); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), config(h), root, cur); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "releases"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "0.1.0,0.2.0" {
		t.Fatalf("releases = %v, want [0.1.0 0.2.0]", names)
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"0.2.0", "dev", true},
		{"0.2.0", "0.1.0", true},
		{"0.2.0", "0.2.0-rc.1", true},
		{"0.2.0", "0.2.0", false},
		{"0.1.0", "0.2.0", false},
		{"not-semver", "dev", false},
	}
	for _, c := range cases {
		if got := isNewer(c.candidate, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}

func TestResolveURL(t *testing.T) {
	base := "https://example.com/versio/stable/manifest.json"
	cases := map[string]string{
		"../releases/0.2.0/a.zip":       "https://example.com/versio/releases/0.2.0/a.zip",
		"https://cdn.example.org/a.zip": "https://cdn.example.org/a.zip",
	}
	for ref, want := range cases {
		got, err := resolveURL(base, ref)
		if err != nil || got != want {
			t.Errorf("resolveURL(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
	for _, ref := range []string{"file:///etc/passwd", "ftp://example.com/a.zip"} {
		if got, err := resolveURL(base, ref); err == nil {
			t.Errorf("resolveURL(%q) = %q, want error", ref, got)
		}
	}
}
