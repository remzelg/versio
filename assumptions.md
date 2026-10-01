# Assumptions

The brief left several questions open. These are the ones that shaped the
design most, the answer we assumed for each, and how the code implements it.

## 1. When should an update take effect?

**Assumed:** Immediately, on the same launch that finds it.

**How:** Before starting the app, the launcher checks the manifest, and if a
newer release exists it downloads, verifies, and activates it, then runs the
new version in that same invocation (`versio: updated 0.2.0 -> 0.3.0`). The
cost is some launch latency, so network waits are bounded (5s to connect, 10s
for a response, 2 minutes in total). `versio --version` never checks for
updates, so it stays fast and works offline.

## 2. How does a customer get versio the first time, and on which platforms?

**Assumed:** Customers have no Go and no source code, and can't be required
to install anything first (such as WSL on Windows). They run one command and
get a working install, without admin rights. Supported platforms are Windows,
macOS, and Linux, each on amd64 and arm64.

**How:** There is no single command that works in every OS's default shell,
so there is one script per shell family: `curl -fsSL <host>/install.sh | sh`
for macOS and Linux, and `irm <host>/install.ps1 | iex` for Windows. The
script detects the OS and CPU, downloads the matching launcher, checks it
against `SHA256SUMS`, puts it on PATH, and runs it once. With nothing
installed yet, the launcher downloads the latest release through the normal
update path. Go was chosen because it cross-compiles all six targets from one
machine into self-contained binaries, which keeps the install script the only
platform-specific step.

## 3. How does the client know an update is genuine and safe to run?

**Assumed:** The download location can't be trusted (a compromised host or
network must not be able to run code on customer machines), and a broken or
interrupted update must never leave the install unusable.

**How:** Each release's manifest is signed with an Ed25519 private key that
exists only on the server side. The matching public key is built into the
launcher and can't be overridden. The launcher verifies the signature before
reading anything in the manifest, then checks the downloaded archive's size
and SHA-256 against it, and runs the new binary with `--version` to confirm it
starts and reports the promised version. Only then does it switch
`current.json`, in one atomic write. Each release has its own folder, so the
old version stays on disk until the new one is fully in place.

## 4. Does the update server need to be a custom service?

**Assumed:** No. Releases should work from any static file host (S3, a CDN,
GitHub Pages, nginx).

**How:** `versio-release build` writes a plain folder of files: the signed
manifest, one zip per platform, the launchers, and the install scripts.
Manifest links are relative, so the folder works under any address.
`versio-release serve` exists only to host that folder locally. Security
doesn't depend on the host: everything the launcher installs is checked
against the signature.

## 5. What happens when an update fails or a release is bad?

**Assumed:** The user should always be able to run versio, should not be
nagged when offline, and versions only ever move forward.

**How:** If any update step fails (offline, server down, bad signature, broken
download), the launcher silently runs the version already installed and tries
again next launch; `VERSIO_DEBUG=1` shows why. If the active release is
missing or broken, the launcher runs the previous version, which is kept on
disk. Updates are newer-only: a bad release is fixed by publishing a higher
version, never by moving clients backward, and `build` refuses to republish or
downgrade a version.

## 6. Should the installer support unusual environments?

Some setups make the obvious OS or CPU check give the wrong answer: Rosetta
on Apple Silicon Macs, 32-bit or emulated PowerShell on Windows, Git Bash or
MSYS2 on Windows, and Alpine Linux (musl instead of glibc).

**Assumed:** Yes, as far as installing the right native build. Each fix is a
line or two in the scripts. Supporting these as separate platforms (for
example a Git Bash or 32-bit build) is out of scope.

**How:** `install.sh` checks `sysctl.proc_translated` and installs the ARM
build under Rosetta. `install.ps1` reads the operating system's architecture
(`OSArchitecture`), not the PowerShell process's. `install.sh` stops on Git
Bash or MSYS2 and points to `install.ps1`. Alpine needs nothing: builds use
`CGO_ENABLED=0`, so one static Linux binary runs on both glibc and musl.

## Known limitations

Deliberately left out to keep the scope small:

- **The launcher never updates itself.** Only the app updates. A launcher fix
  ships by re-running the install script; doing it automatically would need,
  for example, a `min_launcher_version` field in the manifest.
- **No automatic rollback of a bad release.** A release that passes the
  `--version` check but misbehaves later stays active until a higher version
  is published. A health check or crash counter could switch back to the
  previous version.
- **A release that fails validation is downloaded again on every launch.**
  Recording the failed version in `current.json` would stop the repeats.
- **Leftover `staging/` folders are not cleaned up.** A launch killed
  mid-download leaves its private staging folder behind.
- **No code signing or key rotation.** Launchers aren't Authenticode-signed
  or notarized, and there is no way to replace the release signing key
  without reinstalling.
