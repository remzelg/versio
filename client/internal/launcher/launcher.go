// Package launcher locates the active versio release, updates it when a newer
// signed release is available, and runs it.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/remycarr/versio/client/internal/active"
	"github.com/remycarr/versio/client/internal/update"
	"github.com/remycarr/versio/client/release"
)

// updateTimeout bounds a whole update attempt, including the download.
// Connection and response-header timeouts in newHTTPClient make an
// unreachable host fail much sooner.
const updateTimeout = 2 * time.Minute

// Options configures the launcher. The zero value disables updates.
type Options struct {
	// Update configures update-on-launch. Updates are skipped when
	// Update.ManifestURL or Update.PublicKey is unset.
	Update update.Config
	// Debug reports update failures on stderr. Otherwise they are silent,
	// so an offline user is not warned on every launch.
	Debug bool
}

// Run executes the active release with the given launcher arguments and
// returns the process exit code. Only a leading --root is consumed; every
// other argument (including --version) is forwarded unchanged.
//
// Before running the payload, Run installs and activates a newer release if
// one is available, so the same invocation already runs the new version.
// "--version" never triggers an update, keeping it offline and deterministic.
func Run(opts Options, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root, childArgs, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "versio-launcher: %v\n", err)
		return 2
	}
	if root == "" {
		if root, err = defaultRoot(); err != nil {
			fmt.Fprintf(stderr, "versio-launcher: %v\n", err)
			return 1
		}
	}
	if root, err = filepath.Abs(root); err != nil {
		fmt.Fprintf(stderr, "versio-launcher: resolve root: %v\n", err)
		return 1
	}

	state, err := active.Load(root)
	if err != nil {
		fmt.Fprintf(stderr, "versio-launcher: no usable active release in %s: %v\n", root, err)
		return 1
	}

	if wantsUpdate(opts, childArgs) {
		state = tryUpdate(opts, root, state, stderr)
	}

	bin, err := payload(root, state, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "versio-launcher: no usable active release in %s: %v\n", root, err)
		return 1
	}

	cmd := exec.Command(bin, childArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
		return 1 // terminated by a signal
	default:
		fmt.Fprintf(stderr, "versio-launcher: run %s: %v\n", bin, err)
		return 1
	}
}

func wantsUpdate(opts Options, childArgs []string) bool {
	if opts.Update.ManifestURL == "" || opts.Update.PublicKey == nil {
		return false
	}
	for _, a := range childArgs {
		if a == "--version" {
			return false
		}
	}
	return true
}

// tryUpdate runs one update attempt and returns the state to launch. Any
// failure keeps the current state.
func tryUpdate(opts Options, root string, cur active.State, stderr io.Writer) active.State {
	cfg := opts.Update
	if cfg.Client == nil {
		cfg.Client = newHTTPClient()
	}
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	res, err := update.Run(ctx, cfg, root, cur)
	if err != nil {
		if opts.Debug {
			fmt.Fprintf(stderr, "versio-launcher: update skipped: %v\n", err)
		}
		return cur
	}
	if res.Updated {
		fmt.Fprintf(stderr, "versio: updated %s -> %s\n", cur.ActiveVersion, res.State.ActiveVersion)
	}
	return res.State
}

// newHTTPClient fails fast when the release host is unreachable or slow to
// respond, while leaving the body transfer bounded only by the context.
func newHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 5 * time.Second
	t.ResponseHeaderTimeout = 10 * time.Second
	return &http.Client{Transport: t}
}

// payload returns the executable to run: the active release's, or the
// previous release's if the active one is unusable.
func payload(root string, s active.State, stderr io.Writer) (string, error) {
	bin, err := releaseBinary(root, s.ActiveVersion)
	if err == nil || s.PreviousVersion == "" {
		return bin, err
	}
	prev, prevErr := releaseBinary(root, s.PreviousVersion)
	if prevErr != nil {
		return "", err
	}
	fmt.Fprintf(stderr, "versio-launcher: active release unusable (%v); running previous release %s\n", err, s.PreviousVersion)
	return prev, nil
}

func releaseBinary(root, version string) (string, error) {
	dir, err := active.ReleaseDir(root, version)
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, release.BinaryName(runtime.GOOS))
	if err := checkExecutable(bin); err != nil {
		return "", err
	}
	return bin, nil
}

// parseArgs consumes a leading "--root <path>" or "--root=<path>". A
// dedicated flag.FlagSet is deliberately not used: it would reject the
// child's --version as an unknown launcher flag.
func parseArgs(args []string) (root string, rest []string, err error) {
	if len(args) == 0 {
		return "", nil, nil
	}
	switch a := args[0]; {
	case a == "--root":
		if len(args) < 2 {
			return "", nil, errors.New("--root requires a path")
		}
		root, rest = args[1], args[2:]
	case strings.HasPrefix(a, "--root="):
		root, rest = strings.TrimPrefix(a, "--root="), args[1:]
	default:
		return "", args, nil
	}
	if root == "" {
		return "", nil, errors.New("--root requires a non-empty path")
	}
	return root, rest, nil
}

func defaultRoot() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is not set; use --root")
		}
		return filepath.Join(base, "versio"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory (use --root): %w", err)
	}
	return filepath.Join(home, ".versio"), nil
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("release binary %s: %w", path, err)
	}
	if info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
		return fmt.Errorf("release binary %s is not an executable file", path)
	}
	return nil
}
