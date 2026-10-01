// Package install holds the install scripts that versio-release publishes at
// the root of the release tree:
//
//	curl -fsSL <base URL>/install.sh | sh     (macOS, Linux)
//	irm <base URL>/install.ps1 | iex          (Windows)
//
// Each script downloads the launcher for its platform from
// <base URL>/versio/launcher/, checks it against SHA256SUMS there, puts it on
// PATH, and runs it once so the launcher installs the latest release.
package install

import (
	"embed"
	"strings"
)

//go:embed install.sh install.ps1
var files embed.FS

// DefaultBaseURL is the release host written into the scripts in this
// directory. Scripts replaces it, so a published script defaults to the host
// it was published for.
const DefaultBaseURL = "http://127.0.0.1:8080"

// Scripts returns the install scripts by file name, with DefaultBaseURL
// replaced by baseURL.
func Scripts(baseURL string) (map[string][]byte, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	scripts := make(map[string][]byte, len(entries))
	for _, e := range entries {
		data, err := files.ReadFile(e.Name())
		if err != nil {
			return nil, err
		}
		scripts[e.Name()] = []byte(strings.ReplaceAll(string(data), DefaultBaseURL, baseURL))
	}
	return scripts, nil
}
