# Installs versio for the current user on Windows:
#
#   irm http://127.0.0.1:8080/install.ps1 | iex
#
# It downloads the launcher for this CPU, checks it against the published
# SHA256SUMS, installs it as %LOCALAPPDATA%\versio\bin\versio.exe, adds that
# folder to the user PATH, and runs it once so the launcher downloads and
# verifies the app itself. No administrator rights are needed.
#
# Environment overrides:
#
#   VERSIO_BASE_URL        release host (default http://127.0.0.1:8080)
#   VERSIO_INSTALL_DIR     where versio.exe goes (default %LOCALAPPDATA%\versio\bin)
#   VERSIO_NO_MODIFY_PATH  "1" leaves the user PATH alone
#
# Everything is inside Install-Versio, which is called in the try block at
# the end, so a download cut off partway through fails to parse and runs
# nothing.

function Install-Versio {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # the progress bar slows downloads badly in Windows PowerShell
    # Windows PowerShell 5.1 may not enable TLS 1.2 by default.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $baseUrl = if ($env:VERSIO_BASE_URL) { $env:VERSIO_BASE_URL } else { 'http://127.0.0.1:8080' }
    $binDir = if ($env:VERSIO_INSTALL_DIR) { $env:VERSIO_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'versio\bin' }

    $arch = Get-Arch
    $name = "versio-launcher_windows_$arch.exe"
    $url = "$baseUrl/versio/launcher"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("versio-install-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Installing versio..."
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$url/$name" -OutFile (Join-Path $tmp $name)
            Invoke-WebRequest -UseBasicParsing -Uri "$url/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')
        } catch {
            throw "could not download versio from ${baseUrl}: $($_.Exception.Message)"
        }

        $expected = $null
        foreach ($line in Get-Content (Join-Path $tmp 'SHA256SUMS')) {
            $fields = $line -split '\s+'
            if ($fields.Count -ge 2 -and $fields[1] -eq $name) { $expected = $fields[0] }
        }
        if (-not $expected) { throw "could not verify the download; please try again" }
        $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $name)).Hash
        if ($actual -ne $expected) { throw "the download was corrupted; please try again" }

        # Copy next to the destination, then rename, so a partly written file
        # is never on PATH. Rerunning replaces only the launcher; installed
        # releases in %LOCALAPPDATA%\versio are kept.
        $dest = Join-Path $binDir 'versio.exe'
        New-Item -ItemType Directory -Force -Path $binDir | Out-Null
        Copy-Item (Join-Path $tmp $name) "$dest.tmp" -Force
        Move-Item "$dest.tmp" $dest -Force
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    # First run installs the app. Its output is only shown if it fails.
    # Windows PowerShell turns redirected stderr lines into errors, which
    # 'Stop' would make fatal, so relax it for this one call.
    $ErrorActionPreference = 'Continue'
    $firstRun = & $dest 2>&1
    $ok = $LASTEXITCODE -eq 0
    $ErrorActionPreference = 'Stop'
    if ($ok) {
        $result = "versio $(& $dest --version) is installed."
    } else {
        $firstRun | ForEach-Object { Write-Host "$_" }
        $result = "versio is installed, but could not finish setting up."
    }

    $next = Add-ToPath $binDir
    Write-Host "$result $next"
}

# Get-Arch returns the Go name of the operating system's CPU architecture.
# OSArchitecture is used rather than PROCESSOR_ARCHITECTURE, which describes
# this PowerShell process: "x86" in 32-bit PowerShell, "AMD64" under x64
# emulation on ARM.
function Get-Arch {
    try {
        $osArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    } catch {
        # .NET Framework before 4.7.1 lacks RuntimeInformation.
        $osArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    }
    switch ($osArch) {
        { $_ -in 'X64', 'AMD64' } { return 'amd64' }
        'Arm64' { return 'arm64' }
        default { throw "this processor ($osArch) is not supported" }
    }
}

# Add-ToPath adds dir to the user PATH for new terminals and to this session,
# unless it is already there or VERSIO_NO_MODIFY_PATH=1. It returns what the
# user should do to start versio.
function Add-ToPath($dir) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = if ($userPath) { $userPath -split ';' } else { @() }
    if ($entries -contains $dir) { return 'Run: versio' }
    if ($env:VERSIO_NO_MODIFY_PATH -eq '1') {
        return "Add $dir to your PATH, then run: versio"
    }
    $newPath = (@($entries | Where-Object { $_ }) + $dir) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$dir"
    return 'Run: versio'
}

# Errors are reported as one line rather than a PowerShell error record. The
# script returns instead of calling exit, which would close the user's
# PowerShell window under "irm | iex".
try {
    Install-Versio
} catch {
    Write-Host "versio: install failed: $($_.Exception.Message)" -ForegroundColor Red
}
