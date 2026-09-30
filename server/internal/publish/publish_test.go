package publish

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/remycarr/versio/client/release"
)

var host = Target{runtime.GOOS, runtime.GOARCH}

func self(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestWriteLayoutAndSignature(t *testing.T) {
	dir := t.TempDir()
	pub, priv := newKey(t)
	bins := []Binary{{Target{"windows", "amd64"}, self(t)}, {Target{"linux", "arm64"}, self(t)}}

	m, err := Write(dir, "0.2.0", bins, priv)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ManifestPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(ManifestPath(dir) + release.SignatureSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if err := release.Verify(pub, data, sig); err != nil {
		t.Fatalf("manifest signature: %v", err)
	}
	parsed, err := release.ParseManifest(data)
	if err != nil || parsed.Version != "0.2.0" || len(parsed.Artifacts) != 2 {
		t.Fatalf("manifest = %+v, err = %v", parsed, err)
	}

	a := m.Artifacts["windows/amd64"]
	if a.URL != "../releases/0.2.0/versio_0.2.0_windows_amd64.zip" {
		t.Errorf("url = %q", a.URL)
	}
	archive := filepath.Join(dir, "versio", "releases", "0.2.0", "versio_0.2.0_windows_amd64.zip")
	info, err := os.Stat(archive)
	if err != nil || info.Size() != a.Size {
		t.Fatalf("archive stat = %v, %v; manifest size %d", info, err, a.Size)
	}

	// No temporary files are left in the tree.
	filepath.Walk(dir, func(p string, _ os.FileInfo, _ error) error {
		if strings.Contains(filepath.Base(p), ".tmp-") {
			t.Errorf("leftover temporary file %s", p)
		}
		return nil
	})
}

func TestWriteIsDeterministic(t *testing.T) {
	_, priv := newKey(t)
	bins := []Binary{{host, self(t)}}
	var archives [2][]byte
	for i := range archives {
		dir := t.TempDir()
		if _, err := Write(dir, "0.2.0", bins, priv); err != nil {
			t.Fatal(err)
		}
		name := release.ArchiveName("0.2.0", host.GOOS, host.GOARCH)
		data, err := os.ReadFile(filepath.Join(dir, "versio", "releases", "0.2.0", name))
		if err != nil {
			t.Fatal(err)
		}
		archives[i] = data
	}
	if !bytes.Equal(archives[0], archives[1]) {
		t.Fatal("identical inputs produced different archives")
	}
}

func TestWriteRefusesOverwriteAndDowngrade(t *testing.T) {
	dir := t.TempDir()
	_, priv := newKey(t)
	bins := []Binary{{host, self(t)}}
	if _, err := Write(dir, "0.2.0", bins, priv); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"0.2.0", "0.1.0", "0.2.0-rc.1"} {
		if _, err := Write(dir, v, bins, priv); err == nil {
			t.Errorf("publishing %s over 0.2.0 succeeded", v)
		}
	}
	if _, err := Write(dir, "0.3.0", bins, priv); err != nil {
		t.Fatalf("publishing a newer version: %v", err)
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	_, priv := newKey(t)
	bin := Binary{host, self(t)}
	cases := map[string]struct {
		version string
		bins    []Binary
	}{
		"not semver":       {"dev", []Binary{bin}},
		"no binaries":      {"0.2.0", nil},
		"duplicate target": {"0.2.0", []Binary{bin, bin}},
		"missing binary":   {"0.2.0", []Binary{{host, filepath.Join(t.TempDir(), "nope")}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := Write(dir, c.version, c.bins, priv); err == nil {
				t.Fatal("expected error")
			}
			if _, err := os.Stat(ManifestPath(dir)); !os.IsNotExist(err) {
				t.Errorf("manifest written despite error: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "versio", "releases", c.version)); !os.IsNotExist(err) {
				t.Errorf("release directory written despite error: %v", err)
			}
		})
	}
}

// TestArchivesMatchProtocol checks each archive the way a client will: its
// hash and size match the signed manifest, and it holds exactly the payload
// binary, named for its OS, at the archive root.
func TestArchivesMatchProtocol(t *testing.T) {
	dir := t.TempDir()
	_, priv := newKey(t)
	want, err := os.ReadFile(self(t))
	if err != nil {
		t.Fatal(err)
	}
	bins := []Binary{{Target{"windows", "amd64"}, self(t)}, {Target{"darwin", "arm64"}, self(t)}}
	m, err := Write(dir, "0.2.0", bins, priv)
	if err != nil {
		t.Fatal(err)
	}

	for key, a := range m.Artifacts {
		// URLs are relative to the manifest, as the client resolves them.
		path := filepath.Join(filepath.Dir(ManifestPath(dir)), filepath.FromSlash(a.URL))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != a.SHA256 || int64(len(data)) != a.Size {
			t.Fatalf("%s: archive does not match manifest", key)
		}

		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		goos, _, _ := strings.Cut(key, "/")
		if len(zr.File) != 1 || zr.File[0].Name != release.BinaryName(goos) || !zr.File[0].Mode().IsRegular() {
			t.Fatalf("%s: unexpected archive entries %v", key, zr.File)
		}
		rc, err := zr.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: archived binary differs from input (err %v)", key, err)
		}
	}
}
