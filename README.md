# Versio

Versio is a small self-updating command-line tool written in Go (standard
library only, Go 1.22+). On every normal launch, the launcher checks a signed
release manifest, installs a newer release if there is one, and runs it in the
same invocation.

## Repository layout

The repository holds two independent Go modules:

```text
.
├── client/    module github.com/remycarr/versio/client (shared with customers)
│   ├── release/              public contract: manifest schema, naming,
│   │                         version rules, signature verification
│   ├── cmd/versio-launcher   stable entry point; performs updates
│   ├── cmd/versio            the versioned application (the payload)
│   └── internal/             active (current.json), launcher, update, semver
└── server/    module github.com/remycarr/versio/server (proprietary)
    ├── cmd/versio-release    keygen / build / serve
    └── internal/
        ├── publish/          packages binaries, writes the signed manifest
        └── signing/          Ed25519 signing and private-key handling
```

Dependencies point one way. The server requires the client module to use its
`release` contract and to build `client/cmd/versio`. The client never imports
server code, so it can be distributed on its own. Customers receive only
verification code and the public key. Signing and private-key handling exist
only in `server/`.

During development, `server/go.mod` points at `../client` with a `replace`
directive. Once the client is published separately, that line becomes a normal
requirement on a tagged client version. The client version a release ships is
then pinned in `server/go.mod`.

## Client

- **`versio-launcher`** (installed as `versio`) is the stable Go launcher. It finds the active release,
  updates it if a newer signed release is published (see [Updates](#updates)),
  runs its `versio` binary, forwards stdin/stdout/stderr and arguments
  unchanged, and returns the child's exit code. It owns only a leading
  `--root <path>` (or `--root=<path>`). Everything after that, including
  `--version`, belongs to the application.
- **`versio`** is the versioned Go application. Its version is injected at
  build time (`-ldflags "-X main.version=0.1.0"`, defaulting to `dev`).
  - `versio` prints `Your version is <version>`.
  - `versio --version` prints only `<version>`.
  - Anything else prints `usage: versio [--version]` to stderr and exits
    non-zero.

### Why a launcher plus immutable releases?

The launcher never changes, so it can always start whichever release is active.
Each release lives in its own directory and is never modified after install.
Switching versions is then a single atomic rewrite of `current.json`, and the
previous release is still on disk for rollback.

The state file is treated as untrusted: it names versions, never paths. The
launcher validates the version as a single safe path component (no separators,
no `..`) and derives the release directory as `<root>/releases/<version>`,
refusing it if it is a symlink rather than a real directory.

## Server

`versio-release` is the release tooling:

- `keygen` writes `.keys/versio.key` (secret, git-ignored) and
  `.keys/versio.pub`. It refuses to overwrite existing keys.
- `build -version X.Y.Z` cross-compiles `client/cmd/versio`, zips it, and
  writes a signed manifest into `dist/`. It builds six targets
  (`publish.DefaultTargets`): windows, darwin (macOS), and linux, each for
  amd64 and arm64. It refuses to republish or downgrade.
- `serve` hosts `dist/` over HTTP for local development, or over HTTPS with
  `-cert`/`-key`.

There is no custom backend. `dist/` is a static tree for any static host, and
`serve` is only a local stand-in for one.

## Usage

End-to-end demo (two terminals):

```text
$ make -C server keys                  # signing key pair in server/.keys/
$ make -C client install               # `versio` on PATH (~/.local/bin), 0.1.0 in ~/.versio
$ versio
Your version is 0.1.0

$ make -C server release RELEASE=0.2.0 # publish a signed 0.2.0 into server/dist/
$ make -C server serve                 # terminal 2: serve on 127.0.0.1:8080

$ versio --version                     # offline, no update check
0.1.0
$ versio
versio: updated 0.1.0 -> 0.2.0
Your version is 0.2.0
```

`make -C client build` produces only the launcher, as `client/bin/versio`. The
payload is never placed there. It is built straight into a release directory,
so there is no stray binary that would bypass updates. `make -C client install`
is idempotent: re-running it replaces the launcher and keeps the installed
releases. `make -C client uninstall` removes both.

`make -C client install` builds from source. How customers without Go would
get versio the first time (install scripts, published launchers, and a
first-run download) is designed in [docs/first-install.md](docs/first-install.md)
but not implemented.

For a throwaway install that doesn't touch `~`, `make -C client install-dev`
resets `client/.dev-install` to 0.1.0, and `make -C client run` or
`make -C client version` runs the launcher against it.

Without `make` (PowerShell, from the repository root):

```powershell
cd server
go run ./cmd/versio-release keygen
go run ./cmd/versio-release build -version 0.2.0
go run ./cmd/versio-release serve                 # leave running

cd ..\client                                      # second terminal
go build -ldflags "-X main.manifestURL=http://127.0.0.1:8080/versio/stable/manifest.json -X main.publicKey=$(Get-Content ..\server\.keys\versio.pub)" -o bin\versio.exe ./cmd/versio-launcher
```

Run `go test ./...` in `client/` and in `server/` (or `make -C client test`
and `make -C server test`). `make -C client clean` removes
builds and the dev install. `make -C server clean` removes `dist/` but keeps the
keys.

Plain HTTP is acceptable locally because trust comes from the signature, not
the transport. Production should still use HTTPS, which adds confidentiality
and makes replaying a stale (but validly signed) manifest harder.

Default root when `--root` is omitted: `~/.versio` (Linux/macOS) or
`%LOCALAPPDATA%\versio` (Windows).

## Install layout

The same layout is used on every platform:

```text
.dev-install/
├── current.json
└── releases/
    └── 0.1.0/
        └── versio        (versio.exe on Windows)
```

```json
{ "active_version": "0.1.0", "previous_version": "dev" }
```

`current.json` is replaced atomically (write a temp file, fsync, rename), so a
concurrent launch sees either the old state or the new one.

## Updates

The launcher is built with a manifest URL and an Ed25519 public key:

```bash
go build -ldflags "-X main.manifestURL=https://example.com/versio/stable/manifest.json \
  -X main.publicKey=<base64 key>" ./cmd/versio-launcher   # from client/
```

A launcher built without both never updates. On each launch except
`versio --version`, `client/internal/update`:

1. Fetches `manifest.json` and `manifest.json.sig`, and verifies the signature
   over the exact manifest bytes before trusting anything in it.
2. Stops unless the manifest's semver version is newer than the active one
   (`dev` and other non-semver builds are older than any release).
3. Downloads this platform's ZIP into a private directory under `staging/`,
   checking its size and SHA-256 against the signed manifest.
4. Extracts the single payload and runs `<payload> --version`, requiring it to
   print the promised version.
5. Renames the release into `releases/<version>/`, rewrites `current.json`
   atomically (`previous_version` = the old active), and prunes other releases.

Any failure leaves `current.json` untouched, and the existing release runs. If
the active release is missing or not executable, the launcher falls back to
`previous_version`.

Network waits are bounded: 5s to connect, 10s for response headers, 2 minutes
for the whole attempt.

| Variable              | Effect                                                 |
|-----------------------|--------------------------------------------------------|
| `VERSIO_MANIFEST_URL` | Overrides the built-in manifest URL.                   |
| `VERSIO_NO_UPDATE=1`  | Disables update checks.                                |
| `VERSIO_DEBUG=1`      | Prints why an update attempt was skipped.              |

The public key has no override: it is the root of trust and comes only from the
build. The static host just transports files; tampering there causes signature
or hash failures, never code execution.

The protocol both sides rely on is defined once, in `client/release`.
`server/internal/publish` tests that the generated tree satisfies it: the
signature verifies, the manifest parses, and each archive's hash, size, and
single entry are what the client expects.

## Release tree

```text
server/dist/versio/
├── stable/
│   ├── manifest.json
│   └── manifest.json.sig
└── releases/0.2.0/
    ├── versio_0.2.0_darwin_amd64.zip    (contains versio at the root)
    ├── versio_0.2.0_darwin_arm64.zip
    ├── versio_0.2.0_linux_amd64.zip
    ├── versio_0.2.0_linux_arm64.zip
    ├── versio_0.2.0_windows_amd64.zip   (contains versio.exe at the root)
    └── versio_0.2.0_windows_arm64.zip
```

Archives are written first and the manifest last, so a client never sees a
manifest pointing at missing files. Archives are reproducible (fixed
timestamps, `-trimpath`), and artifact URLs are relative, so the tree can be
uploaded to any static host.

## Known limitations

- There is no installer for customers yet: first install is from source
  (`make -C client install`). The planned design is in
  [docs/first-install.md](docs/first-install.md).
- The launcher itself never updates. Shipping a launcher fix needs a separate
  mechanism, e.g. a `min_launcher_version` manifest field.
- A release that validates (`--version` works) but misbehaves later is not
  rolled back automatically. A health check or crash counter could trigger a
  switch back to `previous_version`.
- A release that fails validation is downloaded again on every launch. A
  "known bad version" record in `current.json` would stop that.
- Concurrent launches may both download the same release. That is safe (the
  rename is atomic, and the loser reuses the winner's install) but wasteful. A
  lock file would avoid it.
- `staging/` directories from killed launches are not garbage collected.
- No code signing (Authenticode, notarization), key rotation, or TUF-style
  freeze and rollback protection for the manifest.

## Clarifying questions

The brief invites writing down questions and assuming an answer.

- **Q:** Should an update take effect on the invocation that discovers it, or on
  the next one?
  **Assumed:** Immediately. The launcher checks, installs, and activates a new
  release before starting the payload, so the same run uses the new version.
  The cost is launch latency, bounded by a short network timeout; on timeout or
  any failure the current release runs and the next launch tries again.

- **Q:** Is a custom server acceptable, or should releases work from any
  static host?
  **Assumed:** Any static host. Releases are plain files with a signed manifest.
  `versio-release serve` exists only for local development.
- **Q:** How do customers get versio the first time?
  **Assumed:** A one-line install script per shell family (`install.sh` for
  macOS and Linux, `install.ps1` for Windows) detects the platform and
  installs the matching launcher. The launcher then downloads the app on its
  first run and handles all later updates. Go was chosen so that one codebase
  cross-compiles to every target as a self-contained binary, which keeps this
  install step the only platform-specific one. See
  [docs/first-install.md](docs/first-install.md).
- **Q:** Should users see update activity?
  **Assumed:** Minimally. A successful update prints one line to stderr
  (`versio: updated 0.1.0 -> 0.2.0`), and stdout stays the application's. Failed
  attempts are silent unless `VERSIO_DEBUG=1`, so offline users are not nagged on
  every launch.
- **Q:** May a client ever move to an older version (e.g. the publisher pulls a
  bad release)?
  **Assumed:** No. Updates are strictly newer-only. A bad release is fixed by
  publishing a higher version.
