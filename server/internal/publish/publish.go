// Package publish packages built payloads into the static release tree that
// the updater consumes:
//
//	<dir>/versio/
//	├── stable/
//	│   ├── manifest.json
//	│   └── manifest.json.sig
//	└── releases/<version>/
//	    └── versio_<version>_<goos>_<goarch>.zip   (one per target)
//
// The tree can be uploaded as-is to any static file host. Manifest artifact
// URLs are relative, so the tree works under any base URL.
package publish

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/remycarr/versio/client/release"
	"github.com/remycarr/versio/server/internal/signing"
)

// Channel is the only release channel.
const Channel = "stable"

// Target is a GOOS/GOARCH pair to build for.
type Target struct{ GOOS, GOARCH string }

func (t Target) String() string { return release.PlatformKey(t.GOOS, t.GOARCH) }

// DefaultTargets are the platforms each release is built for: Intel/AMD and
// ARM builds of Windows, macOS, and Linux.
var DefaultTargets = []Target{
	{"windows", "amd64"},
	{"windows", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
}

// Binary is a payload already built for Target.
type Binary struct {
	Target
	Path string
}

// ManifestPath returns the manifest's location within the tree rooted at dir.
func ManifestPath(dir string) string {
	return filepath.Join(dir, "versio", Channel, release.ManifestName)
}

// zipTime is a fixed entry timestamp so identical binaries give identical
// archives. ZIP cannot represent dates before 1980; midday keeps the date in
// 1980 when tools display it in local time.
var zipTime = time.Date(1980, 1, 1, 12, 0, 0, 0, time.UTC)

// Write adds release version, made of bins, to the tree at dir and signs a
// new manifest pointing at it. Releases are immutable: Write refuses to
// overwrite an existing release or to publish a version that is not newer
// than the one currently in the manifest.
func Write(dir, version string, bins []Binary, priv ed25519.PrivateKey) (release.Manifest, error) {
	if err := CheckVersion(dir, version); err != nil {
		return release.Manifest{}, err
	}
	if len(bins) == 0 {
		return release.Manifest{}, errors.New("no binaries to publish")
	}

	releasesDir := filepath.Join(dir, "versio", "releases")
	final := filepath.Join(releasesDir, version)
	if _, err := os.Stat(final); err == nil {
		return release.Manifest{}, fmt.Errorf("release %s already exists at %s", version, final)
	}
	if err := os.MkdirAll(releasesDir, 0o755); err != nil {
		return release.Manifest{}, err
	}
	// Assemble next to the final location, then rename, so an interrupted
	// run never leaves a half-written release directory.
	tmp, err := os.MkdirTemp(releasesDir, "."+version+".tmp-*")
	if err != nil {
		return release.Manifest{}, err
	}
	defer os.RemoveAll(tmp)

	m := release.Manifest{
		Schema:    release.SchemaVersion,
		Version:   version,
		Artifacts: make(map[string]release.Artifact, len(bins)),
	}
	for _, b := range bins {
		key := b.Target.String()
		if _, dup := m.Artifacts[key]; dup {
			return release.Manifest{}, fmt.Errorf("duplicate target %s", key)
		}
		name := release.ArchiveName(version, b.GOOS, b.GOARCH)
		a, err := writeArchive(filepath.Join(tmp, name), b.Path, release.BinaryName(b.GOOS))
		if err != nil {
			return release.Manifest{}, fmt.Errorf("package %s: %w", key, err)
		}
		// Relative to <dir>/versio/stable/manifest.json.
		a.URL = path.Join("..", "releases", version, name)
		m.Artifacts[key] = a
	}
	if err := m.Validate(); err != nil {
		return release.Manifest{}, err
	}
	if err := os.Chmod(tmp, 0o755); err != nil { // MkdirTemp uses 0700
		return release.Manifest{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return release.Manifest{}, err
	}

	// Artifacts first, manifest last: a client never sees a manifest that
	// points at archives which do not exist yet.
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return release.Manifest{}, err
	}
	data = append(data, '\n')
	manifest := ManifestPath(dir)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		return release.Manifest{}, err
	}
	if err := writeFileAtomic(manifest+release.SignatureSuffix, signing.Sign(priv, data)); err != nil {
		return release.Manifest{}, err
	}
	if err := writeFileAtomic(manifest, data); err != nil {
		return release.Manifest{}, err
	}
	return m, nil
}

// CheckVersion requires version to be a valid, installable semver that is
// newer than the version already published in dir, if any. Write calls it;
// callers may also call it early to fail before expensive builds.
func CheckVersion(dir, version string) error {
	if err := release.ValidateVersion(version); err != nil {
		return err
	}
	data, err := os.ReadFile(ManifestPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cur, err := release.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("existing manifest: %w", err)
	}
	c, err := release.CompareVersions(version, cur.Version)
	if err != nil {
		return fmt.Errorf("existing manifest: %w", err)
	}
	if c <= 0 {
		return fmt.Errorf("version %s is not newer than published version %s", version, cur.Version)
	}
	return nil
}

// writeArchive zips the binary at src as the single entry name, returning the
// archive's hash and size.
func writeArchive(dst, src, name string) (release.Artifact, error) {
	in, err := os.Open(src)
	if err != nil {
		return release.Artifact{}, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return release.Artifact{}, err
	}
	defer out.Close()

	h := sha256.New()
	counter := &countWriter{w: io.MultiWriter(out, h)}
	zw := zip.NewWriter(counter)
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipTime}
	hdr.SetMode(0o755)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return release.Artifact{}, err
	}
	if _, err := io.Copy(w, in); err != nil {
		return release.Artifact{}, err
	}
	if err := zw.Close(); err != nil {
		return release.Artifact{}, err
	}
	if err := out.Close(); err != nil {
		return release.Artifact{}, err
	}
	return release.Artifact{SHA256: hex.EncodeToString(h.Sum(nil)), Size: counter.n}, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func writeFileAtomic(name string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil { // CreateTemp uses 0600
		return err
	}
	return os.Rename(tmp.Name(), name)
}
