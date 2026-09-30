# First install: getting versio onto a new machine

**Status: design only. Nothing in this document is implemented.** Today the
only way to install versio is from source with `make -C client install`, which
builds the launcher for the local machine and seeds `~/.versio` with a release.
This document describes how a customer without Go or the source code would get
versio for the first time, and what would need to be built to support it.

## The problem

Updating an installed client is solved: the launcher checks a signed manifest,
downloads the release for its own platform, verifies it, and runs it. Two
properties make that work, and neither exists before the first install:

1. **The launcher knows its platform.** Go builds `GOOS`/`GOARCH` into the
   binary, so it picks the right archive automatically. Before anything is
   installed, *something else* must work out whether the machine is Windows,
   macOS, or Linux, on amd64 or arm64.
2. **The launcher holds the trust anchor.** Every update is checked against
   the Ed25519 public key embedded in the launcher. The launcher itself can't
   be checked that way, because the key isn't on the machine yet.

So first install is a bootstrap problem. The goal is to keep it as small as
possible: **deliver one binary, the launcher, and let it do everything else.**

## Why Go

versio is written in Go largely because Go makes this kind of cross-platform
delivery simple. Native code still needs one build per OS and CPU; Go doesn't
change that, since Windows, macOS, and Linux use different executable formats
and amd64 and arm64 use different instruction sets. What Go does is make those
builds cheap to produce and easy to ship:

- **Cross-compilation is built in.** Setting `GOOS` and `GOARCH` builds for
  any of the six targets from one machine, with no extra toolchains, SDKs, or
  build machines. `versio-release build` produces all six in about ten
  seconds on a laptop.
- **Nothing to install first.** A Go program is a single self-contained
  executable with the runtime compiled in. Customers don't need Go, Python, a
  JVM, or .NET, which is what lets the install scripts be "download one file
  and put it on PATH".
- **No system library dependencies.** With `CGO_ENABLED=0` the binaries are
  statically linked, so one Linux build runs on glibc and musl
  distributions alike, and nothing breaks when the OS updates its C library.
- **The standard library covers everything needed.** HTTP and TLS, SHA-256,
  Ed25519, ZIP extraction, JSON, and starting processes all come from Go's
  standard library, which behaves the same on every platform. The project has
  no third-party dependencies at all.
- **Platform differences stay small and explicit.** The OS-specific code in
  the client comes down to the `.exe` suffix (`release.BinaryName`), the
  default install root (`defaultRoot`), and skipping the executable-bit check
  on Windows. Atomic renames, file permissions, and process handling go
  through the same `os` and `os/exec` calls everywhere.
- **Each binary knows its own platform.** `runtime.GOOS` and `runtime.GOARCH`
  are fixed at compile time, so an installed launcher always requests the
  right release. Detecting the platform only has to happen once, in the
  install script, before any Go code is on the machine.

This is why first install is the *only* platform-specific step: after it, one
codebase handles every platform identically.

## Overview

```text
 customer                      static host                       machine
 ────────                      ───────────                       ───────
 runs one-line command ──────▶ install.sh / install.ps1
                               (served over HTTPS)
                                        │
                     script detects OS + CPU, picks one of 6 launchers
                                        │
                               launcher binary ────────────────▶ verify SHA-256
                                                                  place on PATH
                                                                  run `versio`
                                                                       │
                     launcher finds no current.json: first-run install │
                               manifest.json + .sig ◀──────────────────┤
                               release archive ◀───────────────────────┤
                                                                  verify signature,
                                                                  hash, --version;
                                                                  activate; run app
```

After this, the machine is indistinguishable from one installed with
`make -C client install`, and every later update follows the existing update
path.

The design has three parts:

1. The release tool publishes **launcher binaries** for all six platforms.
2. Two **install scripts**, one POSIX `sh` script for macOS and Linux and one
   PowerShell script for Windows, pick and install the right launcher.
3. The launcher gains a **first-run install**: with no `current.json`, it
   installs the latest release instead of failing.

## 1. Published launchers

`versio-release build` currently publishes payload archives only. It would
also build the launcher for the same six targets, with the production manifest
URL and public key embedded via `-ldflags` (the same way the Makefile does
for development builds).

```text
dist/
├── install.sh
├── install.ps1
└── versio/
    ├── launcher/1/
    │   ├── versio-launcher_darwin_amd64
    │   ├── versio-launcher_darwin_arm64
    │   ├── versio-launcher_linux_amd64
    │   ├── versio-launcher_linux_arm64
    │   ├── versio-launcher_windows_amd64.exe
    │   └── versio-launcher_windows_arm64.exe
    ├── stable/…                         (unchanged)
    └── releases/…                       (unchanged)
```

Design choices:

- **Bare binaries, not ZIPs.** The scripts should need only tools every
  machine has. `unzip` is missing from many minimal Linux images, and
  implementations differ, so avoiding archives for the launcher removes a
  dependency. (Payload releases stay zipped: the launcher extracts those with
  Go's standard library.)
- **A launcher version separate from the app version.** The launcher changes
  rarely and deliberately. It gets its own counter (`launcher/1/`,
  `launcher/2/`, …), and published launchers are immutable like releases.
- **Checksums baked into the scripts.** `versio-release` generates
  `install.sh` and `install.ps1` from templates, writing in the launcher
  version and the SHA-256 of each launcher binary. A script and the binaries
  it installs therefore always match, and no separate checksum file needs to
  be fetched.

## 2. Install scripts

Customers run one line, depending on their shell:

```sh
# macOS and Linux
curl -fsSL https://versio.example.com/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://versio.example.com/install.ps1 | iex
```

Two scripts cover six platforms because each script handles both CPU
architectures, and macOS and Linux share POSIX `sh`.

### What each script does

1. **Detect the platform** (see the table below). Stop with a clear message
   on anything unsupported, e.g. 32-bit Windows or Linux on RISC-V.
2. **Download the matching launcher** to a temporary file.
3. **Verify its SHA-256** against the value baked into the script, using
   `sha256sum` (Linux), `shasum -a 256` (macOS), or `Get-FileHash` (Windows).
4. **Install it** as `versio` / `versio.exe`: make it executable, then move
   it into place in one step, so a half-written file is never on PATH.
5. **Make it reachable on PATH** (see below).
6. **Run `versio` once** to trigger the first-run install, so the app is
   downloaded and verified while the user is watching, rather than on their
   first real use.
7. **Print what happened** and how to uninstall.

The scripts never use `sudo` or administrator rights. Everything is
per-user.

Two structural rules apply to both scripts:

- **Wrap the body in a function and call it on the last line.** If the
  download of the script is cut off, a piped `sh` or `iex` would otherwise run
  a truncated script. With the call on the last line, a truncated script
  defines a function and does nothing.
- **Fail loudly.** Use `set -eu` in `sh` and `$ErrorActionPreference = 'Stop'`
  in PowerShell. Never leave a partial install.

Environment overrides, mainly for testing:

| Variable | Default | Purpose |
|---|---|---|
| `VERSIO_BASE_URL` | `https://versio.example.com` | Where launchers are downloaded from (local demo: `http://127.0.0.1:8080`) |
| `VERSIO_INSTALL_DIR` | See the platform table | Where the `versio` command goes |

### Platform detection

| | Windows | macOS | Linux |
|---|---|---|---|
| OS | Running `install.ps1` at all | `uname -s` = `Darwin` | `uname -s` = `Linux` |
| CPU | `[RuntimeInformation]::OSArchitecture`: `X64` / `Arm64` | `uname -m`: `x86_64` / `arm64` | `uname -m`: `x86_64` / `aarch64` |
| Go name | `amd64` / `arm64` | `amd64` / `arm64` | `amd64` / `arm64` |

Pitfalls the scripts must handle:

- **Rosetta on Apple Silicon.** A terminal running under Rosetta reports
  `uname -m` = `x86_64`. The script checks `sysctl -n sysctl.proc_translated`;
  if it prints `1`, the machine is really `arm64`, and the native launcher
  should be installed.
- **Emulated or 32-bit PowerShell on Windows.** `$env:PROCESSOR_ARCHITECTURE`
  reports the architecture of the *PowerShell process*, not of the machine: a
  32-bit shell says `x86`, and x64 emulation on ARM says `AMD64`.
  `OSArchitecture` reports the operating system instead. Fall back to
  `PROCESSOR_ARCHITEW6432` only on very old systems.
- **Linux C libraries do not matter.** Releases are built with
  `CGO_ENABLED=0`, so binaries are statically linked and run on both glibc
  distributions (Ubuntu, Fedora) and musl ones (Alpine) without separate
  builds.

### Install locations and PATH

These match the launcher's existing default root (`defaultRoot` in
`client/internal/launcher`), so no `--root` flag is ever needed.

| | Windows | macOS | Linux |
|---|---|---|---|
| `versio` command | `%LOCALAPPDATA%\versio\bin\versio.exe` | `~/.local/bin/versio` | `~/.local/bin/versio` |
| Install root (releases, state) | `%LOCALAPPDATA%\versio` | `~/.versio` | `~/.versio` |
| On PATH already? | No | Usually not | Usually yes (most distributions add `~/.local/bin` when it exists) |
| PATH handling | Add the folder to the **user** PATH (`[Environment]::SetEnvironmentVariable(…, 'User')`). New terminals see it; the script also updates `$env:Path` for the current session | Print the exact line to add to `~/.zprofile` (zsh is the default shell) | Same as macOS if missing, for `~/.profile` or `~/.bashrc` |

On macOS and Linux the script **prints** the line rather than editing shell
startup files itself: silently rewriting dotfiles surprises people, and the
right file depends on their shell setup. On Windows, the user PATH is a
registry setting meant to be changed this way, so the script updates it
directly.

On Windows the launcher's `bin` folder sits inside the install root. That is
safe: the updater only deletes inside `releases/` and `staging/`.

### Platform security prompts

| Platform | What can interfere | Effect on this design |
|---|---|---|
| macOS | **Gatekeeper** blocks unsigned binaries that carry the *quarantine* attribute. Browsers add that attribute to downloads; `curl` does not. | The `curl … \| sh` route works unsigned. A browser download of the launcher would need Developer ID signing and notarization. |
| Windows | **SmartScreen** and **Mark of the Web** warn about unsigned downloads from a browser. **Execution policy** can block `.ps1` files. | `irm \| iex` runs the script from memory, so execution policy for script files doesn't apply (though group policy on managed machines can still block it). Unsigned binaries work but may be scanned or held briefly by Defender; production launchers should be Authenticode-signed. |
| Linux | Rare: a home directory mounted `noexec` | The script checks the installed binary runs and reports the problem if not. |

## 3. First-run install in the launcher

Today the launcher exits with an error when `current.json` is missing. The
change is to treat a missing file as "nothing installed" and let the normal
update path install the latest release:

1. In `launcher.Run`, if `active.Load` fails because `current.json` **does
   not exist**, continue with an empty state instead of exiting. A malformed
   or invalid `current.json` is still an error: it signals damage, not a
   fresh machine.
2. `update.Run` already handles this case. An empty active version is not
   semver, so `isNewer` treats any published release as newer. The install
   step creates `staging/` and `releases/` as needed, and the saved state has
   no `previous_version`.
3. If that attempt fails (e.g. no network), there is nothing to fall back to.
   The launcher prints a clear message, such as `versio is not installed yet
   and the release server could not be reached: …`, and exits non-zero,
   regardless of `VERSIO_DEBUG`.
4. **`versio --version` stays offline** even on a fresh machine. It reports
   that versio isn't installed yet instead of downloading. That keeps the
   existing rule simple: `--version` never touches the network. The install
   script triggers the first download by running plain `versio`.

This is roughly fifteen lines in `launcher.Run` plus tests, and it reuses all
the verification the updater already does. Importantly, the first install
of the app is checked exactly as strictly as every later update.

## Trust model

Trust is handed off in three steps:

| Step | What is being trusted | What protects it |
|---|---|---|
| 1. Script | The script comes from us | HTTPS to our domain |
| 2. Launcher | The binary is the one we built | HTTPS, plus the SHA-256 baked into the script (catches corruption and mismatched files). In production, code signing too |
| 3. App and every update | Each release is one we signed | The Ed25519 public key inside the launcher (already implemented) |

Step 3 is the strong one, and it covers the app from its very first download.
Steps 1 and 2 are **trust on first use**, the same as any installer
downloaded from a website. The baked-in checksum doesn't add authenticity on
its own, because an attacker who can change the script can also change the
checksum. It guarantees the script and the binaries match.

Common concerns about `curl … | sh`, and the answers:

- *"The script could be tampered with."* It's no weaker than downloading an
  installer from the same site over HTTPS. Anyone who prefers can download the
  script, read it, and run it.
- *"A truncated download could run half a script."* Handled by the
  function-wrapping rule above.
- *"Unsigned binaries."* Acceptable for this exercise. Production would sign
  the launchers (Authenticode on Windows; Developer ID with notarization on
  macOS) and publish signed checksums.

## Failure modes

| Situation | Behavior |
|---|---|
| Unsupported OS or CPU | Script stops before downloading anything, and names the detected platform |
| Download fails or is truncated | `curl -f` / `Invoke-WebRequest` fail. Nothing is written to the install location |
| Checksum mismatch | Script stops and deletes the temporary file |
| `versio` not on PATH in the current terminal | Script prints the full path to run now, and the PATH fix for new terminals |
| First run can't reach the release server | Launcher is installed; the script reports that the app will be downloaded on first use. The next `versio` retries |
| Script run again | Replaces the launcher with the current one and leaves releases and `current.json` alone, so it doubles as a launcher upgrade (matches `make -C client install`) |
| Existing install from `make -C client install` | Same as running again: the two methods share the install root |

## Uninstall

The scripts print these at the end, and a production version might ship
`uninstall.sh` / `uninstall.ps1`:

```sh
rm -f ~/.local/bin/versio && rm -rf ~/.versio
```

```powershell
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\versio"   # also remove its bin folder from the user PATH
```

## Alternatives considered

| Option | Why not (for now) |
|---|---|
| **Download page** that detects the platform in the browser | Out of scope. Browser CPU detection is also unreliable (Apple Silicon reports Intel in the classic User-Agent), so it still needs a manual fallback |
| **`go install`** | One command on every OS, and Go's checksum database verifies the source. But it needs Go installed, and three changes first: module paths matching the repository (`github.com/remzelg/versio/…`), production URL and key as defaults in source (since `go install` ignores `-ldflags`), and the user-facing command living in `cmd/versio` so the binary is named `versio`. A good extra option for developers, not a replacement |
| **Native installers** (MSI, `.pkg`, `.deb`/`.rpm`) | The best experience on each OS and the natural home for code signing, but three toolchains to maintain. The step after scripts |
| **Package managers** (winget, Homebrew, apt/dnf) | Familiar and updatable, but separate listings and review processes. They would install the same launcher binaries described here |
| **One universal file** (Cosmopolitan "Actually Portable Executables", WebAssembly, JVM) | Go can't produce these; the others need a runtime installed first |

## What would need to be built

| Piece | Where | Size |
|---|---|---|
| Launcher builds for 6 targets with production URL and key | `server/cmd/versio-release build`, `server/internal/publish` | Small: the existing target loop, second package |
| `install.sh` / `install.ps1` templates with baked-in version and checksums | `server/internal/publish` (templates embedded with `go:embed`) | ~60 lines per script |
| First-run install | `client/internal/launcher` | ~15 lines plus tests |
| Local demo | Serve `dist/`, then `curl -fsSL http://127.0.0.1:8080/install.sh \| VERSIO_BASE_URL=http://127.0.0.1:8080 sh` | Docs only |
