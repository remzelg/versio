// Package update installs newer releases on launch.
//
// Run performs one update attempt:
//
//  1. Fetch manifest.json and manifest.json.sig from the channel URL.
//  2. Verify the signature against the embedded public key; nothing in the
//     manifest is trusted before this succeeds.
//  3. Stop unless the manifest's version is newer than the active one.
//  4. Pick the artifact for this GOOS/GOARCH and download it into a private
//     directory under <root>/staging, checking its size and SHA-256.
//  5. Extract the payload and run "<payload> --version" to confirm it starts
//     and reports the promised version.
//  6. Rename the staged release into <root>/releases/<version>.
//  7. Atomically rewrite current.json, remembering the previous version.
//  8. Prune releases that are neither active nor previous.
//
// Any failure leaves current.json untouched, so the caller keeps running the
// release that was already active.
package update

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/remycarr/versio/client/internal/active"
	"github.com/remycarr/versio/client/internal/semver"
)

// Config holds everything an update attempt needs besides the install root.
type Config struct {
	// ManifestURL is the absolute URL of the channel's manifest.json.
	ManifestURL string
	// PublicKey verifies manifest signatures.
	PublicKey ed25519.PublicKey
	// Client performs HTTP requests; nil means http.DefaultClient. Callers
	// bound the whole attempt with the context passed to Run.
	Client *http.Client
	// GOOS and GOARCH select the artifact; empty means the running platform.
	GOOS, GOARCH string
}

// Result reports the outcome of a successful Run.
type Result struct {
	State   active.State // the state now in effect
	Updated bool         // whether a new release was activated
}

// Run attempts to update the install at root, whose current state is cur.
// On error the install is unchanged and cur remains in effect.
func Run(ctx context.Context, cfg Config, root string, cur active.State) (Result, error) {
	cfg = cfg.withDefaults()
	if cfg.ManifestURL == "" || len(cfg.PublicKey) != ed25519.PublicKeySize {
		return Result{}, errors.New("updates are not configured")
	}

	m, err := fetchManifest(ctx, cfg)
	if err != nil {
		return Result{}, err
	}
	if !isNewer(m.Version, cur.ActiveVersion) {
		return Result{State: cur}, nil
	}
	if err := active.ValidateVersion(m.Version); err != nil {
		return Result{}, fmt.Errorf("manifest version: %w", err)
	}

	if err := install(ctx, cfg, root, m); err != nil {
		return Result{}, fmt.Errorf("install %s: %w", m.Version, err)
	}

	next := active.State{ActiveVersion: m.Version, PreviousVersion: cur.ActiveVersion}
	if err := active.Save(root, next); err != nil {
		return Result{}, err
	}
	prune(root, next)
	return Result{State: next, Updated: true}, nil
}

func (c Config) withDefaults() Config {
	if c.Client == nil {
		c.Client = http.DefaultClient
	}
	if c.GOOS == "" {
		c.GOOS = runtime.GOOS
	}
	if c.GOARCH == "" {
		c.GOARCH = runtime.GOARCH
	}
	return c
}

// isNewer reports whether candidate should replace current. A current version
// that is not semver (such as the "dev" build) is older than any release.
func isNewer(candidate, current string) bool {
	c, err := semver.Parse(candidate)
	if err != nil {
		return false
	}
	cur, err := semver.Parse(current)
	if err != nil {
		return true
	}
	return semver.Compare(c, cur) > 0
}

// prune removes release directories other than the active and previous ones.
// It is best effort: a release still in use (e.g. a running versio.exe on
// Windows) simply fails to delete and is retried after the next update.
func prune(root string, s active.State) {
	dir := filepath.Join(root, "releases")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if name := e.Name(); name != s.ActiveVersion && name != s.PreviousVersion {
			os.RemoveAll(filepath.Join(dir, name))
		}
	}
}
