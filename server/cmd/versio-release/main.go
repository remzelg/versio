// Command versio-release creates signing keys and publishes signed releases.
//
//	versio-release keygen [-out .keys]
//	versio-release build -version 0.2.0 [-key .keys/versio.key] [-out dist] [-pkg <payload package>]
//	versio-release serve [-dir dist] [-addr 127.0.0.1:8080] [-cert file -key file]
//
// build cross-compiles the payload for each target, packages the binaries,
// and writes a signed manifest into a static tree under -out, ready to be
// served by any static file host. serve is such a host, for local use.
package main

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/remycarr/versio/client/release"
	"github.com/remycarr/versio/server/internal/publish"
	"github.com/remycarr/versio/server/internal/signing"
)

// payloadPackage is the client CLI that each release ships. It is resolved
// through this module's go.mod, so the client version being released is
// whatever go.mod requires (today: ../client, via replace).
const payloadPackage = "github.com/remycarr/versio/client/cmd/versio"

const usage = `usage:
  versio-release keygen [-out dir]
  versio-release build -version X.Y.Z [-key file] [-out dir] [-pkg path]
  versio-release serve [-dir dir] [-addr host:port] [-cert file -key file]
`

// errUsage marks errors caused by a bad command line; main follows them with
// the usage text.
var errUsage = errors.New("usage error")

func main() {
	err := run(os.Args[1:], os.Stdout)
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "versio-release: %v\n", err)
	if errors.Is(err, errUsage) {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	os.Exit(1)
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing command: %w", errUsage)
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "keygen":
		return keygen(rest, stdout)
	case "build":
		return build(rest, stdout)
	case "serve":
		return serve(rest, stdout)
	default:
		return fmt.Errorf("unknown command %q: %w", cmd, errUsage)
	}
}

// keygen writes a new Ed25519 key pair: <out>/versio.key (private, keep
// secret, never commit) and <out>/versio.pub (embedded in the launcher).
func keygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", ".keys", "directory for versio.key and versio.pub")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	privPath := filepath.Join(*out, "versio.key")
	pubPath := filepath.Join(*out, "versio.pub")
	// O_EXCL: never silently replace a key that released clients trust.
	if err := writeNew(privPath, signing.EncodeKey(priv)+"\n", 0o600); err != nil {
		return err
	}
	if err := writeNew(pubPath, signing.EncodeKey(pub)+"\n", 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (secret) and %s\n", privPath, pubPath)
	return nil
}

func writeNew(name, data string, perm os.FileMode) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func build(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	version := fs.String("version", "", "release version (semver, required)")
	keyPath := fs.String("key", filepath.Join(".keys", "versio.key"), "Ed25519 private key file")
	out := fs.String("out", "dist", "static release tree to write into")
	pkg := fs.String("pkg", payloadPackage, "Go package of the payload")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" {
		return errors.New("build: -version is required")
	}

	if err := publish.CheckVersion(*out, *version); err != nil {
		return err
	}
	keyText, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	priv, err := signing.ParsePrivateKey(string(keyText))
	if err != nil {
		return fmt.Errorf("%s: %w", *keyPath, err)
	}

	work, err := os.MkdirTemp("", "versio-release-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	var bins []publish.Binary
	for _, t := range publish.DefaultTargets {
		bin := filepath.Join(work, t.GOOS+"_"+t.GOARCH, release.BinaryName(t.GOOS))
		fmt.Fprintf(stdout, "building %s\n", t)
		if err := goBuild(*pkg, *version, t, bin); err != nil {
			return fmt.Errorf("build %s: %w", t, err)
		}
		bins = append(bins, publish.Binary{Target: t, Path: bin})
	}

	m, err := publish.Write(*out, *version, bins, priv)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "published %s with %d artifact(s); manifest at %s\n",
		m.Version, len(m.Artifacts), publish.ManifestPath(*out))
	return nil
}

// goBuild cross-compiles pkg for t with the version stamped in. CGO is off so
// cross-compilation needs no C toolchain; -trimpath keeps local paths out of
// the binary.
func goBuild(pkg, version string, t publish.Target, out string) error {
	cmd := exec.Command("go", "build",
		"-trimpath",
		"-ldflags", "-s -w -X main.version="+version,
		"-o", out,
		pkg)
	cmd.Env = append(os.Environ(), "GOOS="+t.GOOS, "GOARCH="+t.GOARCH, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}
