package launcher

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/remycarr/versio/client/internal/active"
	"github.com/remycarr/versio/client/internal/releasetest"
	"github.com/remycarr/versio/client/internal/update"
	"github.com/remycarr/versio/client/release"
)

// TestMain lets the test binary double as the fixture "versio" executable.
// It prints the release directory it runs from and its arguments, or, when
// VERSIO_TEST_VERSION is set, answers "--version" like a real payload.
func TestMain(m *testing.M) {
	if os.Getenv("VERSIO_TEST_HELPER") == "1" {
		if v := os.Getenv("VERSIO_TEST_VERSION"); v != "" && len(os.Args) == 2 && os.Args[1] == "--version" {
			fmt.Println(v)
			os.Exit(0)
		}
		exe, _ := os.Executable()
		fmt.Fprintf(os.Stdout, "release=%s args=%q\n", filepath.Base(filepath.Dir(exe)), os.Args[1:])
		fmt.Fprintln(os.Stderr, "child-stderr")
		code, _ := strconv.Atoi(os.Getenv("VERSIO_TEST_EXIT"))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func newRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "releases", "0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func setState(t *testing.T, root string, s active.State) {
	t.Helper()
	data := fmt.Sprintf(`{"active_version":%q,"previous_version":%q}`, s.ActiveVersion, s.PreviousVersion)
	if err := os.WriteFile(filepath.Join(root, active.StateFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setCurrent(t *testing.T, root, version string) {
	t.Helper()
	setState(t, root, active.State{ActiveVersion: version})
}

func installFixture(t *testing.T, root, version string) {
	t.Helper()
	dir := filepath.Join(root, "releases", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, release.BinaryName(runtime.GOOS))
	if err := os.WriteFile(dst, releasetest.Self(t), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runWith(t *testing.T, opts Options, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(opts, args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runWith(t, Options{}, args...)
}

func setupActive(t *testing.T) string {
	t.Helper()
	t.Setenv("VERSIO_TEST_HELPER", "1")
	root := newRoot(t)
	installFixture(t, root, "0.1.0")
	setCurrent(t, root, "0.1.0")
	return root
}

func TestRunNoChildArgs(t *testing.T) {
	root := setupActive(t)
	code, out, errs := run(t, "--root", root)
	if code != 0 || out != "release=0.1.0 args=[]\n" || errs != "child-stderr\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestRunForwardsVersionUnchanged(t *testing.T) {
	root := setupActive(t)
	code, out, errs := run(t, "--root", root, "--version")
	if code != 0 || out != "release=0.1.0 args=[\"--version\"]\n" || errs != "child-stderr\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestRunRootEqualsForm(t *testing.T) {
	root := setupActive(t)
	code, out, _ := run(t, "--root="+root, "a", "--root", "b")
	if code != 0 || out != "release=0.1.0 args=[\"a\" \"--root\" \"b\"]\n" {
		t.Fatalf("got code=%d stdout=%q", code, out)
	}
}

func TestRunPreservesExitCode(t *testing.T) {
	root := setupActive(t)
	t.Setenv("VERSIO_TEST_EXIT", "7")
	if code, _, _ := run(t, "--root", root); code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
}

func TestRunNoActiveRelease(t *testing.T) {
	root := newRoot(t)
	code, out, errs := run(t, "--root", root)
	if code == 0 || out != "" || !strings.Contains(errs, root) || !strings.Contains(errs, active.StateFile) {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestRunRejectsInvalidActiveVersion(t *testing.T) {
	root := newRoot(t)
	if err := os.Mkdir(filepath.Join(root, "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	setCurrent(t, root, "../elsewhere")
	code, out, errs := run(t, "--root", root)
	if code == 0 || out != "" || !strings.Contains(errs, "invalid version") {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestRunMissingBinary(t *testing.T) {
	root := newRoot(t)
	setCurrent(t, root, "0.1.0")
	code, out, errs := run(t, "--root", root)
	want := filepath.Join(root, "releases", "0.1.0", release.BinaryName(runtime.GOOS))
	if code == 0 || out != "" || !strings.Contains(errs, want) {
		t.Fatalf("got code=%d stdout=%q stderr=%q, want mention of %s", code, out, errs, want)
	}
}

func TestRunFallsBackToPreviousRelease(t *testing.T) {
	t.Setenv("VERSIO_TEST_HELPER", "1")
	root := newRoot(t)
	installFixture(t, root, "0.1.0")
	setState(t, root, active.State{ActiveVersion: "0.2.0", PreviousVersion: "0.1.0"}) // 0.2.0 missing
	code, out, errs := run(t, "--root", root)
	if code != 0 || out != "release=0.1.0 args=[]\n" || !strings.Contains(errs, "running previous release 0.1.0") {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
}

func TestParseArgs(t *testing.T) {
	if _, _, err := parseArgs([]string{"--root"}); err == nil {
		t.Error("expected error for --root without value")
	}
	root, rest, err := parseArgs([]string{"--version"})
	if err != nil || root != "" || len(rest) != 1 || rest[0] != "--version" {
		t.Errorf("got root=%q rest=%q err=%v", root, rest, err)
	}
}

// updateOptions returns launcher options pointing at a host publishing 0.2.0.
func updateOptions(t *testing.T) (*releasetest.Host, Options) {
	t.Helper()
	t.Setenv("VERSIO_TEST_VERSION", "0.2.0")
	h := releasetest.NewHost(t)
	h.Publish(t, "0.2.0", releasetest.SelfZip(t))
	opts := Options{Update: update.Config{ManifestURL: h.ManifestURL(), PublicKey: h.PublicKey, Client: h.Client()}}
	return h, opts
}

func TestRunUpdatesAndRunsNewReleaseImmediately(t *testing.T) {
	root := setupActive(t)
	_, opts := updateOptions(t)

	code, out, errs := runWith(t, opts, "--root", root)
	if code != 0 || out != "release=0.2.0 args=[]\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if !strings.Contains(errs, "updated 0.1.0 -> 0.2.0") {
		t.Errorf("stderr %q lacks update notice", errs)
	}
	s, err := active.Load(root)
	if err != nil || s != (active.State{ActiveVersion: "0.2.0", PreviousVersion: "0.1.0"}) {
		t.Fatalf("state = %+v, err = %v", s, err)
	}
}

func TestRunVersionFlagSkipsUpdate(t *testing.T) {
	root := setupActive(t)
	h, opts := updateOptions(t)
	t.Setenv("VERSIO_TEST_VERSION", "") // make the payload echo its release

	code, out, _ := runWith(t, opts, "--root", root, "--version")
	if code != 0 || out != "release=0.1.0 args=[\"--version\"]\n" {
		t.Fatalf("got code=%d stdout=%q", code, out)
	}
	if n := h.Requests(""); n != 0 {
		t.Fatalf("--version made %d network requests", n)
	}
}

func TestRunUpdateFailureRunsCurrentRelease(t *testing.T) {
	root := setupActive(t)
	h, opts := updateOptions(t)
	h.Set(releasetest.ManifestPath+release.SignatureSuffix, []byte("bogus"))

	code, out, errs := runWith(t, opts, "--root", root)
	if code != 0 || out != "release=0.1.0 args=[]\n" || errs != "child-stderr\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}

	opts.Debug = true
	_, _, errs = runWith(t, opts, "--root", root)
	if !strings.Contains(errs, "update skipped") {
		t.Fatalf("debug stderr %q lacks failure report", errs)
	}
}

func TestRunFirstRunInstallsLatestRelease(t *testing.T) {
	t.Setenv("VERSIO_TEST_HELPER", "1")
	root := filepath.Join(t.TempDir(), "versio") // does not exist yet
	_, opts := updateOptions(t)

	code, out, errs := runWith(t, opts, "--root", root)
	if code != 0 || out != "release=0.2.0 args=[]\n" || !strings.Contains(errs, "installed 0.2.0") {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
	s, err := active.Load(root)
	if err != nil || s != (active.State{ActiveVersion: "0.2.0"}) {
		t.Fatalf("state = %+v, err = %v", s, err)
	}
}

func TestRunFirstRunFailureExplains(t *testing.T) {
	root := t.TempDir()
	h, opts := updateOptions(t)
	h.Set(releasetest.ManifestPath+release.SignatureSuffix, []byte("bogus"))

	code, out, errs := runWith(t, opts, "--root", root)
	if code == 0 || out != "" || !strings.Contains(errs, "not installed yet") {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(root, active.StateFile)); !os.IsNotExist(err) {
		t.Fatalf("current.json after failed install: %v", err)
	}
}

func TestRunFirstRunVersionStaysOffline(t *testing.T) {
	root := t.TempDir()
	h, opts := updateOptions(t)

	code, _, errs := runWith(t, opts, "--root", root, "--version")
	if code == 0 || !strings.Contains(errs, active.StateFile) {
		t.Fatalf("got code=%d stderr=%q", code, errs)
	}
	if n := h.Requests(""); n != 0 {
		t.Fatalf("--version made %d network requests", n)
	}
}
