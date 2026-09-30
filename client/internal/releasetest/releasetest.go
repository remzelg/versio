// Package releasetest provides a fake static release host for tests.
package releasetest

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/remycarr/versio/client/release"
)

// ManifestPath is where Host serves the signed manifest.
const ManifestPath = "/stable/" + release.ManifestName

// Host serves release files over HTTP and records requested paths.
type Host struct {
	PublicKey ed25519.PublicKey
	srv       *httptest.Server
	priv      ed25519.PrivateKey

	mu       sync.Mutex
	files    map[string][]byte
	requests []string
}

// NewHost starts a host that is shut down when the test ends.
func NewHost(t testing.TB) *Host {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &Host{PublicKey: pub, priv: priv, files: map[string][]byte{}}
	h.srv = httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *Host) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.requests = append(h.requests, r.URL.Path)
	data, ok := h.files[r.URL.Path]
	h.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Write(data)
}

// ManifestURL is the absolute URL of the served manifest.
func (h *Host) ManifestURL() string { return h.srv.URL + ManifestPath }

// Client returns an HTTP client for the host.
func (h *Host) Client() *http.Client { return h.srv.Client() }

// Publish serves a signed manifest for version whose only artifact, for the
// running platform, is archive.
func (h *Host) Publish(t testing.TB, version string, archive []byte) {
	t.Helper()
	sum := sha256.Sum256(archive)
	m := release.Manifest{
		Schema:  release.SchemaVersion,
		Version: version,
		Artifacts: map[string]release.Artifact{
			release.PlatformKey(runtime.GOOS, runtime.GOARCH): {
				URL:    "../releases/" + version + "/" + release.ArchiveName(version, runtime.GOOS, runtime.GOARCH),
				SHA256: hex.EncodeToString(sum[:]),
				Size:   int64(len(archive)),
			},
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	h.Set(ManifestPath, data)
	h.Set(ManifestPath+release.SignatureSuffix, Sign(h.priv, data))
	h.Set(ArchivePath(version), archive)
}

// Sign produces a signature file in the format release.Verify accepts. The
// real signer belongs to the release tooling; this one serves test fixtures.
func Sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
}

// ArchivePath is where Publish serves version's archive.
func ArchivePath(version string) string {
	return "/releases/" + version + "/" + release.ArchiveName(version, runtime.GOOS, runtime.GOARCH)
}

// Get returns the file served at path.
func (h *Host) Get(path string) []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.files[path]
}

// Set serves data at path.
func (h *Host) Set(path string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.files[path] = data
}

// Requests reports how many requests were made for paths with the suffix.
func (h *Host) Requests(suffix string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, p := range h.requests {
		if strings.HasSuffix(p, suffix) {
			n++
		}
	}
	return n
}

// Zip builds a ZIP archive from name/content pairs.
func Zip(t testing.TB, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Self returns the running test binary. Tests make it act as a versio
// payload from their TestMain.
func Self(t testing.TB) []byte {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// SelfZip returns a release archive whose payload is the test binary.
func SelfZip(t testing.TB) []byte {
	t.Helper()
	return Zip(t, map[string][]byte{release.BinaryName(runtime.GOOS): Self(t)})
}
