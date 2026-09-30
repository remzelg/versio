package update

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/remycarr/versio/client/internal/active"
	"github.com/remycarr/versio/client/release"
)

const (
	maxBinaryBytes    = 512 << 20 // 512 MiB uncompressed
	validationTimeout = 10 * time.Second
)

// install makes <root>/releases/<m.Version> exist and hold a validated
// payload. It never touches current.json.
func install(ctx context.Context, cfg Config, root string, m release.Manifest) error {
	bin := release.BinaryName(cfg.GOOS)

	// A previous launch may have installed this release and then failed to
	// activate it, or a concurrent launch may have just installed it.
	if dir, err := active.ReleaseDir(root, m.Version); err == nil {
		if err := validate(ctx, filepath.Join(dir, bin), m.Version); err != nil {
			// Discard it so the next launch downloads a fresh copy.
			os.RemoveAll(dir)
			return err
		}
		return nil
	}

	key := release.PlatformKey(cfg.GOOS, cfg.GOARCH)
	a, ok := m.Artifacts[key]
	if !ok {
		return fmt.Errorf("no artifact for %s", key)
	}
	artifactURL, err := resolveURL(cfg.ManifestURL, a.URL)
	if err != nil {
		return err
	}

	// Each attempt gets a private directory, so concurrent launches never
	// share partially written files.
	staging := filepath.Join(root, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(staging, m.Version+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, "artifact.zip")
	if err := download(ctx, cfg.Client, artifactURL, a, archive); err != nil {
		return err
	}
	candidate := filepath.Join(tmp, "release")
	if err := os.Mkdir(candidate, 0o755); err != nil {
		return err
	}
	if err := extract(archive, bin, filepath.Join(candidate, bin)); err != nil {
		return err
	}
	if err := validate(ctx, filepath.Join(candidate, bin), m.Version); err != nil {
		return err
	}

	// staging/ and releases/ share a parent, so this rename stays on one
	// filesystem and the release appears all at once.
	if err := os.MkdirAll(filepath.Join(root, "releases"), 0o755); err != nil {
		return err
	}
	if err := os.Rename(candidate, filepath.Join(root, "releases", m.Version)); err != nil {
		// Losing a race to an identical install is fine.
		if _, statErr := active.ReleaseDir(root, m.Version); statErr == nil {
			return nil
		}
		return fmt.Errorf("move release into place: %w", err)
	}
	return nil
}

// extract writes the archive's single entry, which must be named name, to dst.
// The archive's hash was already verified against the signed manifest; the
// structural checks here guard against release-tooling mistakes.
func extract(archive, name, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()

	if len(zr.File) != 1 || zr.File[0].Name != name || !zr.File[0].Mode().IsRegular() {
		return fmt.Errorf("archive must contain exactly one file, %q", name)
	}
	f := zr.File[0]
	if f.UncompressedSize64 > maxBinaryBytes {
		return fmt.Errorf("archived %s is %d bytes, exceeds limit %d", name, f.UncompressedSize64, maxBinaryBytes)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}
	defer rc.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	// The zip reader checks the entry's CRC-32 when it reaches EOF.
	if _, err := io.Copy(out, io.LimitReader(rc, maxBinaryBytes)); err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}

// validate runs "<bin> --version" and requires it to print want. This proves
// the payload executes on this machine before it is activated.
func validate(ctx context.Context, bin, want string) error {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return fmt.Errorf("validate candidate %s: %w", bin, err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		return fmt.Errorf("validate candidate %s: reports version %q, want %q", bin, got, want)
	}
	return nil
}
