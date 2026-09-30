package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/remycarr/versio/client/release"
)

const (
	maxManifestBytes  = 1 << 20   // 1 MiB
	maxSignatureBytes = 1 << 10   // 1 KiB
	maxArtifactBytes  = 512 << 20 // 512 MiB, a sanity bound on the signed size
)

// fetchManifest downloads the manifest and its detached signature, verifies
// the signature over the exact bytes received, and only then parses them.
func fetchManifest(ctx context.Context, cfg Config) (release.Manifest, error) {
	data, err := get(ctx, cfg.Client, cfg.ManifestURL, maxManifestBytes)
	if err != nil {
		return release.Manifest{}, err
	}
	sig, err := get(ctx, cfg.Client, cfg.ManifestURL+release.SignatureSuffix, maxSignatureBytes)
	if err != nil {
		return release.Manifest{}, err
	}
	if err := release.Verify(cfg.PublicKey, data, sig); err != nil {
		return release.Manifest{}, fmt.Errorf("manifest %s: %w", cfg.ManifestURL, err)
	}
	return release.ParseManifest(data)
}

// get fetches a small document, failing if it exceeds limit bytes.
func get(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	resp, err := open(ctx, client, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("GET %s: response exceeds %d bytes", rawURL, limit)
	}
	return data, nil
}

// download streams an artifact to dst, verifying its exact size and SHA-256.
// On error dst may contain partial data; the caller discards it.
func download(ctx context.Context, client *http.Client, rawURL string, a release.Artifact, dst string) error {
	if a.Size > maxArtifactBytes {
		return fmt.Errorf("artifact size %d exceeds limit %d", a.Size, maxArtifactBytes)
	}
	resp, err := open(ctx, client, rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	// Read one byte past the expected size so an oversized body is detected.
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, a.Size+1))
	if err != nil {
		return fmt.Errorf("GET %s: %w", rawURL, err)
	}
	if n != a.Size {
		return fmt.Errorf("GET %s: got %d bytes, manifest says %d", rawURL, n, a.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("GET %s: sha256 %s does not match manifest %s", rawURL, got, a.SHA256)
	}
	return f.Close()
}

func open(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "versio-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return resp, nil
}

// resolveURL resolves an artifact URL against the manifest URL, so manifests
// can use relative links and the whole tree can move between hosts.
func resolveURL(manifestURL, ref string) (string, error) {
	base, err := url.Parse(manifestURL)
	if err != nil {
		return "", fmt.Errorf("manifest url: %w", err)
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("artifact url %q: %w", ref, err)
	}
	u := base.ResolveReference(r)
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("artifact url %q: unsupported scheme %q", ref, u.Scheme)
	}
	return u.String(), nil
}
