# Installs pethost, Pethost's command-line tool, on Windows:
#
#   irm https://pethost.dev/install.ps1 | iex
#
# It finds the CPU, downloads the release's binary from GitHub, checks it against the release's
# checksums and puts it in ~\.local\bin, without an administrator. It touches nothing else: when
# that folder is not on PATH it says so, and changes no setting.
#
#   $env:PETHOST_VERSION        the release to install, as 0.4.0; without it, the newest
#   $env:PETHOST_RELEASES_URL   where the releases are, in place of GitHub
#
# What a release is made of is defined by .github/workflows/release.yml; `pethost update`
# (internal/release) and install.sh read the same three addresses.
#
# The script is one block, run on its last line: `iex` runs it in the person's own session, so
# its settings must not outlive it, and it never calls `exit`, which would close their window.

& {
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell draws a progress bar that slows a download many times over.
    $ProgressPreference = 'SilentlyContinue'
    # An old Windows PowerShell offers only TLS 1.0, which GitHub refuses: TLS 1.2 is added to
    # what it offers. One that leaves the choice to Windows (0, "SystemDefault") is left alone.
    if ([int][Net.ServicePointManager]::SecurityProtocol -ne 0) {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }

    $releases = 'https://github.com/pethost-dev/cli/releases'
    if ($env:PETHOST_RELEASES_URL) { $releases = $env:PETHOST_RELEASES_URL.TrimEnd('/') }
    $dir = Join-Path $HOME '.local\bin'

    # A 32-bit PowerShell on 64-bit Windows names the real CPU in the second variable.
    $processor = $env:PROCESSOR_ARCHITEW6432
    if (-not $processor) { $processor = $env:PROCESSOR_ARCHITECTURE }
    switch ($processor) {
        'AMD64' { $cpu = 'amd64' }
        'ARM64' { $cpu = 'arm64' }
        default { throw "pethost has releases for x86-64 and ARM64 CPUs, and none for $processor." }
    }
    $binary = "pethost-windows-$cpu.exe"

    $work = Join-Path ([IO.Path]::GetTempPath()) ('pethost-' + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $work | Out-Null
    try {
        # The release to install: the one PETHOST_VERSION names, else the newest. Only digits
        # and dots make a version: it becomes part of an address.
        $version = $env:PETHOST_VERSION
        if (-not $version) {
            try {
                Invoke-WebRequest -UseBasicParsing -Uri "$releases/latest/download/version" -OutFile "$work\version"
            } catch {
                throw "The newest release of pethost could not be read from $releases."
            }
            $version = Get-Content -Raw "$work\version"
        }
        $version = "$version".Trim().TrimStart('v')
        if ($version -notmatch '^[0-9]+(\.[0-9]+)*$') {
            throw "`"$version`" is not a release of pethost: a version reads as 0.4.0."
        }
        $release = "$releases/download/v$version"

        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$release/$binary" -OutFile "$work\pethost.exe"
        } catch {
            throw "pethost $version could not be downloaded for this computer ($release/$binary)."
        }
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$release/checksums.txt" -OutFile "$work\checksums.txt"
        } catch {
            throw "The checksums of pethost $version could not be downloaded: nothing was installed."
        }

        # The checksums are sha256sum's lines: the sum, two spaces, the name.
        $expected = $null
        foreach ($line in Get-Content "$work\checksums.txt") {
            $sum, $name = $line -split '\s+', 2
            if ($name -eq $binary) { $expected = $sum }
        }
        if (-not $expected) {
            throw "The release's checksums do not list ${binary}: nothing was installed."
        }
        $actual = (Get-FileHash -Algorithm SHA256 "$work\pethost.exe").Hash
        if ($actual -ne $expected) {
            throw 'The download is not what the release published: its checksum differs. Nothing was installed.'
        }

        # Windows does not let a running program's file be replaced, but lets it be renamed: a
        # pethost that is there is moved aside first, as `pethost update` does, and deleted
        # by the next install or update.
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        $target = Join-Path $dir 'pethost.exe'
        Remove-Item "$target.old" -Force -ErrorAction SilentlyContinue
        if (Test-Path $target) { Move-Item -Force $target "$target.old" }
        Move-Item "$work\pethost.exe" $target
    } finally {
        Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
    }

    Write-Output "pethost $version is in ~\.local\bin (checksum verified)."
    $onPath = ($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') }) -contains $dir
    if ($onPath) {
        Write-Output 'Next: cd into your app, and run   pethost deploy'
    } else {
        Write-Output '~\.local\bin is not on your PATH. Add it:'
        Write-Output '  [Environment]::SetEnvironmentVariable("Path", "$HOME\.local\bin;" + [Environment]::GetEnvironmentVariable("Path", "User"), "User")'
        Write-Output 'Next: open a new terminal, cd into your app, and run   pethost deploy'
    }
}
