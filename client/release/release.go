// Package release is the public contract for versio releases: the manifest
// format, artifact naming, version rules, and signature verification. The
// updater enforces it; release tooling must produce files that satisfy it.
//
// A channel on the static host looks like:
//
//	<channel>/manifest.json      the Manifest, as JSON
//	<channel>/manifest.json.sig  base64 Ed25519 signature of manifest.json's
//	                             exact bytes, followed by an optional newline
//
// Only verification lives here. Signing belongs to the release tooling, which
// holds the private key.
package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/remycarr/versio/client/internal/semver"
)

// SchemaVersion is the manifest format version this code reads and writes.
const SchemaVersion = 1

const (
	ManifestName    = "manifest.json"
	SignatureSuffix = ".sig"
)

// Manifest describes the latest release on a channel.
type Manifest struct {
	Schema  int    `json:"schema"`
	Version string `json:"version"`
	// Artifacts maps a PlatformKey such as "linux/amd64" to its archive.
	Artifacts map[string]Artifact `json:"artifacts"`
}

// Artifact is one downloadable archive. The archive is a ZIP whose only entry
// is BinaryName(goos) at the archive root.
type Artifact struct {
	// URL is absolute or relative to the manifest's own URL.
	URL    string `json:"url"`
	SHA256 string `json:"sha256"` // lowercase hex
	Size   int64  `json:"size"`   // bytes
}

// MaxVersionLen bounds release version strings. Clients use the version as a
// directory name, so it must stay short.
const MaxVersionLen = 64

// ValidateVersion checks that v is usable as a release version: a semantic
// version of at most MaxVersionLen bytes. Semver's character set is already
// safe as a single path component.
func ValidateVersion(v string) error {
	if len(v) > MaxVersionLen {
		return fmt.Errorf("version is longer than %d bytes", MaxVersionLen)
	}
	_, err := semver.Parse(v)
	return err
}

// CompareVersions orders two release versions, returning -1, 0, or +1 as a
// is lower than, equal to, or higher than b.
func CompareVersions(a, b string) (int, error) {
	av, err := semver.Parse(a)
	if err != nil {
		return 0, err
	}
	bv, err := semver.Parse(b)
	if err != nil {
		return 0, err
	}
	return semver.Compare(av, bv), nil
}

// PlatformKey returns the manifest key for a platform, e.g. "darwin/arm64".
func PlatformKey(goos, goarch string) string {
	return goos + "/" + goarch
}

// BinaryName returns the payload executable's file name on goos.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "versio.exe"
	}
	return "versio"
}

// ArchiveName returns the conventional archive file name for a build.
func ArchiveName(version, goos, goarch string) string {
	return fmt.Sprintf("versio_%s_%s_%s.zip", version, goos, goarch)
}

// ParseManifest decodes and validates a manifest. Call it only on bytes whose
// signature has already been verified.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("malformed manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate checks that m is well formed.
func (m Manifest) Validate() error {
	if m.Schema != SchemaVersion {
		return fmt.Errorf("manifest schema %d is not supported (want %d)", m.Schema, SchemaVersion)
	}
	if err := ValidateVersion(m.Version); err != nil {
		return fmt.Errorf("manifest version: %w", err)
	}
	if len(m.Artifacts) == 0 {
		return errors.New("manifest lists no artifacts")
	}
	for key, a := range m.Artifacts {
		if err := a.validate(); err != nil {
			return fmt.Errorf("manifest artifact %s: %w", key, err)
		}
	}
	return nil
}

func (a Artifact) validate() error {
	if a.URL == "" {
		return errors.New("url is empty")
	}
	if a.Size <= 0 {
		return fmt.Errorf("size %d is not positive", a.Size)
	}
	if len(a.SHA256) != 64 || strings.ToLower(a.SHA256) != a.SHA256 {
		return fmt.Errorf("sha256 %q is not 64 lowercase hex digits", a.SHA256)
	}
	if _, err := hex.DecodeString(a.SHA256); err != nil {
		return fmt.Errorf("sha256 %q: %w", a.SHA256, err)
	}
	return nil
}

// Verify checks a detached signature file against data.
func Verify(pub ed25519.PublicKey, data, sigFile []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigFile)))
	if err != nil {
		return fmt.Errorf("malformed signature: %w", err)
	}
	if !ed25519.Verify(pub, data, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}

// ParsePublicKey decodes a base64 Ed25519 public key.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("malformed public key: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key is %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}
