# Versio

Versio is a small self-updating command-line tool written in Go (standard
library only, Go 1.22+). Customers install it with one command. After that,
every launch checks for a newer signed release, installs it, and runs it in
the same invocation.

## Repository layout

The repository holds two independent Go modules.

```text
.
├── client/     everything that runs on a customer's machine
└── server/     everything used to build, sign, and host releases
```

**`client/`** (module `github.com/remycarr/versio/client`) is the code that
ships to customers: the `versio` command they run, the application it starts,
and the update logic. It contains no secrets, only the public key used to
verify releases, so it can be shared with customers as-is.

- `cmd/versio-launcher`: the launcher, installed as `versio`. It updates the
  app and runs it.
- `cmd/versio`: the application itself (the "payload"), one per release.
- `release/`: the contract shared with the server: manifest format, file
  naming, version rules, and signature checks.
- `internal/`: the launcher, the updater, and the install state on disk.

**`server/`** (module `github.com/remycarr/versio/server`) is proprietary and
never shipped. It is the only place the private signing key is used.

- `cmd/versio-release`: the release tool (`keygen`, `build`, `serve`).
- `install/`: the install scripts customers run (`install.sh`,
  `install.ps1`).
- `internal/publish`: packages builds into the static release tree.
- `internal/signing`: signs manifests with the private key.
- `.keys/` (git-ignored): the signing key pair, created by `keygen`.
- `dist/` (git-ignored): the published release tree, created by `build`.

The server depends on the client (to build it and to share `release/`), never
the other way around. During development, `server/go.mod` points at
`../client` with a `replace` directive.

## Server: publishing releases

All commands run from the repository root. Each `make` target wraps a
`versio-release` command, shown after it.

### 1. Create the signing key (once)

```sh
make -C server keys       # go run ./cmd/versio-release keygen
```

This writes `server/.keys/versio.key` (private, never commit it) and
`server/.keys/versio.pub`. The public key is built into every launcher, and
installed launchers only accept releases signed with the matching private
key, so keep the key: `keygen` refuses to overwrite it.

### 2. Publish a release

```sh
make -C server release RELEASE=0.2.0   # go run ./cmd/versio-release build -version 0.2.0
```

`build` writes everything a customer needs into `server/dist/`:

1. Builds the app for six platforms (Windows, macOS, and Linux, each on
   amd64 and arm64), zips each build, and records its SHA-256 and size in a
   new `manifest.json`.
2. Signs the manifest with the private key (`manifest.json.sig`).
3. Builds the launcher for the same six platforms, with the server's
   manifest URL and the public key built in, and lists their checksums in
   `SHA256SUMS`.
4. Copies the two install scripts to the top of the tree.

Releases are immutable and versions must be semver: `build` refuses to
republish a version or publish one that isn't newer than the current one.
Launchers and install scripts are replaced on every build, so new customers
always get the newest launcher.

```text
server/dist/
├── install.sh                      macOS/Linux install script
├── install.ps1                     Windows install script
└── versio/
    ├── stable/
    │   ├── manifest.json           latest version and its archives
    │   └── manifest.json.sig       Ed25519 signature of manifest.json
    ├── releases/0.2.0/
    │   ├── versio_0.2.0_linux_amd64.zip
    │   └── ...                     one zip per platform, never changed
    └── launcher/
        ├── versio-launcher_linux_amd64
        ├── versio-launcher_windows_amd64.exe
        ├── ...                     one launcher per platform
        └── SHA256SUMS
```

### 3. Serve it

```sh
make -C server serve      # go run ./cmd/versio-release serve
```

This serves `server/dist/` at `http://127.0.0.1:8080` until Ctrl+C. Pass
`-cert` and `-key` to serve HTTPS. A new release can be published while the
server is running; clients see it on their next launch.

`serve` is only a local stand-in. `dist/` is a plain folder of static files
and can be uploaded to any static host instead. The launchers and scripts
contain the host's address, so build for the address customers will use:

```sh
cd server && go run ./cmd/versio-release build -version 0.2.0 -base-url https://versio.example.com
```

Plain HTTP is fine locally because trust comes from the signature, not the
connection. A real host should use HTTPS.

### Other commands

```sh
make -C server test       # go test ./...
make -C server clean      # delete dist/ (keeps the keys)
```

## Client: installing and using versio

### Install

The customer runs one command. It needs no Go, no source code, and no admin
rights.

macOS and Linux:

```sh
curl -fsSL http://127.0.0.1:8080/install.sh | sh
```

Windows (PowerShell):

```powershell
irm http://127.0.0.1:8080/install.ps1 | iex
```

The script:

1. Works out the OS and CPU (amd64 or arm64).
2. Downloads the matching launcher and checks it against `SHA256SUMS`.
3. Installs it as the `versio` command and adds its folder to PATH.
4. Runs `versio` once. With nothing installed yet, the launcher downloads the
   latest release, verifies it the same way as any update, and runs it.

```text
versio-install: downloading versio for linux/amd64 from http://127.0.0.1:8080
versio-install: installed /home/remy/.local/bin/versio
versio: installed 0.2.0
Your version is 0.2.0
```

|                    | macOS / Linux          | Windows                                |
|--------------------|------------------------|----------------------------------------|
| `versio` command   | `~/.local/bin/versio`  | `%LOCALAPPDATA%\versio\bin\versio.exe` |
| Releases and state | `~/.versio`            | `%LOCALAPPDATA%\versio`                |
| PATH               | Added to the shell profile (`.zshrc`, `.bashrc`, or `.profile`) if missing | Added to the user PATH if missing |

Running the script again replaces only the launcher; installed releases are
kept. Optional environment variables: `VERSIO_BASE_URL` (another host),
`VERSIO_INSTALL_DIR` (another folder for the command), and
`VERSIO_NO_MODIFY_PATH=1` (leave PATH alone).

To uninstall:

```sh
rm -f ~/.local/bin/versio && rm -rf ~/.versio            # macOS / Linux
```

```powershell
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\versio"   # Windows; also remove its bin folder from the user PATH
```

### Use

```text
$ versio
Your version is 0.2.0
$ versio --version
0.2.0
```

Any other arguments print `usage: versio [--version]` and exit non-zero.

### How updates work

The `versio` command is a small **launcher**. Each release of the actual app
lives in its own folder, and `current.json` records which one is active:

```text
~/.versio/
├── current.json          {"active_version": "0.3.0", "previous_version": "0.2.0"}
└── releases/
    ├── 0.2.0/versio
    └── 0.3.0/versio
```

On every launch except `versio --version`, before starting the app, the
launcher:

1. Downloads `manifest.json` and its signature, and checks the signature with
   the public key built into the launcher. Nothing in the manifest is trusted
   before this passes.
2. Stops if the manifest's version isn't newer than the active one.
3. Downloads the zip for this platform and checks its size and SHA-256
   against the signed manifest.
4. Extracts it and runs `versio --version` on it, which must print the
   promised version.
5. Moves it into `releases/<version>/`, then switches `current.json` to it
   in one atomic write. The old version becomes `previous_version`; anything
   older is deleted.

The app then starts from the new release, in the same invocation:

```text
$ versio
versio: updated 0.2.0 -> 0.3.0
Your version is 0.3.0
```

If any step fails (offline, server down, bad signature, broken download), the
launcher runs the version already installed and tries again next launch. A
failed attempt is silent unless `VERSIO_DEBUG=1`. If the active release is
missing or broken, the launcher runs `previous_version` instead.

- `versio --version` never touches the network, so it is always fast and
  offline.
- Network waits are bounded: 5 seconds to connect, 10 seconds for a response,
  2 minutes for the whole update.
- Updates only move forward. A bad release is fixed by publishing a higher
  version.
- The launcher itself is not updated; re-running the install script replaces
  it.

| Variable              | Effect                                     |
|-----------------------|--------------------------------------------|
| `VERSIO_NO_UPDATE=1`  | Skip update checks.                        |
| `VERSIO_DEBUG=1`      | Print why an update attempt was skipped.   |
| `VERSIO_MANIFEST_URL` | Check a different manifest URL.            |

The public key cannot be overridden: it comes only from the build.

### Building from source (development)

Developers with Go can skip the install script (after `make -C server keys`,
since the launcher is built with the public key):

```sh
make -C client install    # build the launcher into ~/.local/bin and install 0.1.0 into ~/.versio
make -C client test       # go test ./...
make -C client uninstall  # remove both
```

`make -C client install-dev` and `make -C client run` use a throwaway install
in `client/.dev-install` instead of `~/.versio`. These builds check for
updates at `http://127.0.0.1:8080`, so they update from `make -C server serve`
like an installed copy.
