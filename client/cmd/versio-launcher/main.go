package main

import (
	"fmt"
	"os"

	"github.com/remycarr/versio/client/internal/launcher"
	"github.com/remycarr/versio/client/internal/update"
	"github.com/remycarr/versio/client/release"
)

// Injected at build time:
//
//	-ldflags "-X main.manifestURL=https://... -X main.publicKey=<base64>"
//
// Leaving either empty builds a launcher that never updates.
var (
	manifestURL = ""
	publicKey   = ""
)

// Environment overrides. The public key deliberately has no override: it is
// the root of trust and must come from the build.
const (
	envManifestURL = "VERSIO_MANIFEST_URL" // point at another channel or host
	envNoUpdate    = "VERSIO_NO_UPDATE"    // "1" disables update checks
	envDebug       = "VERSIO_DEBUG"        // "1" reports update failures
)

func main() {
	os.Exit(launcher.Run(options(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func options() launcher.Options {
	opts := launcher.Options{Debug: os.Getenv(envDebug) == "1"}
	if os.Getenv(envNoUpdate) == "1" || publicKey == "" {
		return opts
	}
	key, err := release.ParsePublicKey(publicKey)
	if err != nil {
		// A bad embedded key is a build bug; say so rather than silently
		// never updating.
		fmt.Fprintf(os.Stderr, "versio-launcher: updates disabled: embedded %v\n", err)
		return opts
	}
	url := manifestURL
	if v := os.Getenv(envManifestURL); v != "" {
		url = v
	}
	opts.Update = update.Config{ManifestURL: url, PublicKey: key}
	return opts
}
