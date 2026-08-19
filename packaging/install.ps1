<#
    telltale - the one-paste Windows install.

        irm https://raw.githubusercontent.com/sanlee-ys/telltale/main/packaging/install.ps1 | iex

    What this script does, in order: it reads the release you asked for from
    the GitHub API, downloads telltale_<version>_windows_amd64.zip and
    checksums.txt, compares the archive's SHA-256 against that file, and only
    then unpacks the binary. A mismatch deletes the download and stops.

    What this script cannot do: tell you WHO built the archive. No telltale
    binary carries an Authenticode signature. That is the owner's recorded
    decision (docs/design.md section 8, item 8), not an oversight, and
    checksums.txt is the whole of the verification this release can honestly
    offer. It proves the archive is the one the release workflow produced. It
    proves nothing about who produced it. Windows SmartScreen can warn on an
    unsigned binary, and it is correct to.

    This script makes network calls. The gauges do not, and that boundary is
    unchanged: an installer is not a gauge (CLAUDE.md, the read/write
    boundary). Nothing here reads a credential store, and nothing here writes
    outside the install directory and the user PATH entry.

    Environment variables, because a piped script takes no parameters:

      TELLTALE_VERSION      a tag, for example v0.2.0. Default: the latest
                            published release.
      TELLTALE_INSTALL_DIR  where telltale.exe lands. Default:
                            $env:LOCALAPPDATA\Programs\telltale
      TELLTALE_NO_PATH      set to 1 to skip the user PATH edit.

    Windows only, and windows_amd64 only. The release builds no windows/arm64
    binary, so this script refuses that machine by name rather than installing
    something nobody has run. macOS and Linux use the curl and shasum walk in
    README.md.

    scoop is the other Windows route and it is the older one:
    "scoop bucket add telltale https://github.com/sanlee-ys/telltale", then
    "scoop install telltale". Use scoop if you already have it. This script
    exists for the machine that does not.

    THIS FILE IS ASCII ONLY, ON PURPOSE. Windows PowerShell 5.1 reads a
    BOM-less file as ANSI, so a single em dash in a string breaks the parse
    before any of the above runs. Measured 2026-08-18: an em dash in one
    throw produced four parser errors under 5.1.26100.9168 and none under
    PowerShell 7.6.5. Keep every character in this file under 0x80.
#>

function Install-Telltale {
    [CmdletBinding()]
    param()

    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1 draws a progress bar per response chunk, which
    # costs more than the download does. It also defaults to whatever the .NET
    # framework configured, which on an unpatched 5.1 excludes TLS 1.2, and
    # api.github.com answers nothing else.
    $ProgressPreference = 'SilentlyContinue'
    try {
        [Net.ServicePointManager]::SecurityProtocol = `
            [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    } catch {
        # PowerShell 7 manages this itself and the type may be absent. A
        # failure here is not a reason to stop.
    }

    $repo = 'sanlee-ys/telltale'

    if ($env:OS -ne 'Windows_NT') {
        throw 'This script installs the Windows build. macOS and Linux use the curl and shasum walk in README.md.'
    }
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($arch -ne 'AMD64') {
        throw "This machine reports PROCESSOR_ARCHITECTURE=$arch. The release builds windows_amd64 only, so there is no binary to install here. Build from source: go build -o telltale.exe ./cmd/telltale"
    }

    # 1. Which release.
    $tag = $env:TELLTALE_VERSION
    if (-not $tag) {
        Write-Host 'telltale: reading the latest release...'
        $latest = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'telltale-install' }
        $tag = $latest.tag_name
    }
    if (-not $tag) { throw 'No release tag was found. Set TELLTALE_VERSION to a tag, for example v0.2.0.' }
    # The archive name carries the version WITHOUT the leading v; the tag keeps
    # its v only inside the URL. The scoop and winget manifests hard-code the
    # same shape.
    $version = $tag -replace '^v', ''
    $archive = "telltale_${version}_windows_amd64.zip"
    $base = "https://github.com/$repo/releases/download/$tag"

    # 2. Download the archive and the checksums beside it.
    $work = Join-Path ([IO.Path]::GetTempPath()) ("telltale-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $work | Out-Null
    try {
        $zip = Join-Path $work $archive
        $sums = Join-Path $work 'checksums.txt'
        Write-Host "telltale: downloading $archive ($tag)..."
        # The URL is named in the failure, because the common failure here is a
        # tag that has no published release, and a bare "404 (Not Found)" does
        # not tell the reader which of the two files was missing.
        foreach ($pair in @(, @("$base/$archive", $zip)) + @(, @("$base/checksums.txt", $sums))) {
            try {
                Invoke-WebRequest -Uri $pair[0] -OutFile $pair[1] -UseBasicParsing
            } catch {
                throw "$($pair[0]) could not be downloaded. $($_.Exception.Message) Nothing was installed."
            }
        }

        # 3. Verify before unpacking. A checksum checked after the binary is
        #    already on PATH is a checksum that verified nothing.
        $line = Select-String -Path $sums -Pattern $archive -SimpleMatch | Select-Object -First 1
        if (-not $line) {
            throw "checksums.txt on $tag names no entry for $archive. Nothing was installed."
        }
        $want = ($line.Line -split '\s+')[0].ToLowerInvariant()
        $got = (Get-FileHash -Path $zip -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($want -ne $got) {
            Remove-Item -Path $zip -Force
            throw "SHA-256 mismatch on $archive. checksums.txt says $want and the download hashes $got. The download was deleted and nothing was installed."
        }
        Write-Host "telltale: sha256 ok ($got)"

        # 4. Unpack and place.
        $dest = $env:TELLTALE_INSTALL_DIR
        if (-not $dest) { $dest = Join-Path $env:LOCALAPPDATA 'Programs\telltale' }
        if (-not (Test-Path -LiteralPath $dest)) {
            New-Item -ItemType Directory -Path $dest -Force | Out-Null
        }
        $unpack = Join-Path $work 'unpack'
        Expand-Archive -LiteralPath $zip -DestinationPath $unpack -Force
        $exe = Join-Path $unpack 'telltale.exe'
        if (-not (Test-Path -LiteralPath $exe)) {
            throw "$archive holds no telltale.exe. Nothing was installed."
        }
        Copy-Item -LiteralPath $exe -Destination (Join-Path $dest 'telltale.exe') -Force
        Write-Host "telltale: installed to $dest"

        # 5. PATH, user scope only. This script asks for no administrator
        #    rights and edits no machine-wide setting.
        if ($env:TELLTALE_NO_PATH -ne '1') {
            $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
            $parts = @()
            if ($userPath) { $parts = $userPath -split ';' | Where-Object { $_ -ne '' } }
            if ($parts -notcontains $dest) {
                [Environment]::SetEnvironmentVariable('Path', (($parts + $dest) -join ';'), 'User')
                Write-Host "telltale: added $dest to your user PATH. Open a new terminal for it to take effect."
            }
            # The running shell gets it too, so the next command below works in
            # THIS window rather than only in the next one.
            if (($env:Path -split ';') -notcontains $dest) { $env:Path = "$env:Path;$dest" }
        }

        Write-Host ''
        Write-Host 'This binary is NOT signed. No telltale release carries an Authenticode'
        Write-Host 'signature, by the owner''s decision (docs/design.md section 8, item 8).'
        Write-Host 'The SHA-256 above is the whole verification: it proves this archive is the'
        Write-Host 'one the release workflow built, and it proves nothing about who built it.'
        Write-Host ''
        Write-Host 'Now run:'
        Write-Host '  telltale doctor    (which vendor CLIs this machine has)'
        Write-Host '  telltale council   (the room)'
    } finally {
        Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Called rather than left as a definition, because "irm | iex" runs this file
# for its effect. throw rather than exit throughout: exit inside a piped script
# ends the user's whole shell session.
try {
    Install-Telltale
} catch {
    Write-Host "telltale: install failed. $($_.Exception.Message)" -ForegroundColor Red
}
