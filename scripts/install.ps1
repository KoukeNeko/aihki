#Requires -Version 5.1
<#
.SYNOPSIS
    Install the Taiga CLI on Windows.

.DESCRIPTION
    The archive is never trusted on its own: this script downloads the release
    SHA256SUMS alongside it and refuses to install anything whose digest does
    not match, so an interrupted or tampered download fails loudly instead of
    leaving a broken binary on PATH.

.PARAMETER Version
    Release tag to install, for example v0.1.0. Defaults to the latest stable
    release, which never resolves to a pre-release.

.PARAMETER InstallDir
    Directory to install into. Defaults to %LOCALAPPDATA%\Programs\taiga.

.EXAMPLE
    irm https://raw.githubusercontent.com/KoukeNeko/Taiga-CLI/main/scripts/install.ps1 | iex

.EXAMPLE
    .\install.ps1 -Version v0.1.0 -InstallDir C:\Tools\taiga
#>
[CmdletBinding()]
param(
    [string]$Version,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\taiga')
)

$ErrorActionPreference = 'Stop'
# Invoke-WebRequest renders a progress bar that dominates its own runtime on
# Windows PowerShell, so it is turned off for the duration of the download.
$ProgressPreference = 'SilentlyContinue'

$Repository = 'KoukeNeko/taiga-cli'
$LatestReleaseUrl = "https://github.com/$Repository/releases/latest"
$DownloadBase = "https://github.com/$Repository/releases/download"

function Get-TargetArchitecture {
    $architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($architecture) {
        'X64' { return 'amd64' }
        'Arm64' { return 'arm64' }
        default { throw "Unsupported architecture $architecture." }
    }
}

function Resolve-ReleaseVersion {
    param([string]$Requested)
    if ($Requested) { return $Requested }
    # github.com redirects the latest release to its tag URL. Resolving the tag
    # that way avoids api.github.com, whose anonymous rate limit is shared by IP
    # and is routinely exhausted on CI runners and behind corporate NAT.
    $response = Invoke-WebRequest -Uri $LatestReleaseUrl -UseBasicParsing
    $finalUri = $null
    # Windows PowerShell exposes the resolved address as ResponseUri, while
    # PowerShell 7 carries it on the request message.
    if ($response.BaseResponse.PSObject.Properties['ResponseUri']) {
        $finalUri = [string]$response.BaseResponse.ResponseUri
    } elseif ($response.BaseResponse.RequestMessage) {
        $finalUri = [string]$response.BaseResponse.RequestMessage.RequestUri
    }
    if (-not $finalUri) { throw 'Could not determine the latest release. Pass -Version explicitly.' }
    $tag = $finalUri.Split('/')[-1]
    if ($tag -notmatch '^v[0-9]') { throw "Could not read a release tag from $finalUri. Pass -Version explicitly." }
    return $tag
}

function Assert-Checksum {
    param(
        [string]$ArchivePath,
        [string]$ChecksumPath,
        [string]$ArchiveName
    )
    $expected = $null
    foreach ($line in Get-Content -LiteralPath $ChecksumPath) {
        $fields = $line -split '\s+' | Where-Object { $_ -ne '' }
        if ($fields.Count -ge 2 -and $fields[1] -eq $ArchiveName) {
            $expected = $fields[0]
            break
        }
    }
    if (-not $expected) { throw "$ArchiveName is not listed in SHA256SUMS." }
    $actual = (Get-FileHash -LiteralPath $ArchivePath -Algorithm SHA256).Hash
    if ($expected -ine $actual) {
        throw "Checksum mismatch for ${ArchiveName}: expected $expected, got $actual."
    }
    Write-Host "Verified SHA-256 of $ArchiveName"
}

function Add-PathEntry {
    param([string]$Directory)
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @()
    if ($userPath) { $entries = $userPath -split ';' | Where-Object { $_ -ne '' } }
    if ($entries -contains $Directory) { return }
    $updated = (@($entries) + $Directory) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
    Write-Host "Added $Directory to your user PATH. Open a new terminal to pick it up."
}

# Windows PowerShell 5.1 defaults to a protocol GitHub no longer accepts.
if ([Net.ServicePointManager]::SecurityProtocol -notmatch 'Tls12') {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
}

$architecture = Get-TargetArchitecture
$resolvedVersion = Resolve-ReleaseVersion -Requested $Version
$numericVersion = $resolvedVersion -replace '^v', ''
$archiveName = "taiga_${numericVersion}_windows_${architecture}.zip"

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("taiga-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workDir -Force | Out-Null
try {
    $archivePath = Join-Path $workDir $archiveName
    $checksumPath = Join-Path $workDir 'SHA256SUMS'

    Write-Host "Downloading $archiveName ($resolvedVersion)"
    try {
        Invoke-WebRequest -Uri "$DownloadBase/$resolvedVersion/$archiveName" -OutFile $archivePath
    } catch {
        throw "Could not download $archiveName. Check that $resolvedVersion exists for windows/$architecture."
    }
    Invoke-WebRequest -Uri "$DownloadBase/$resolvedVersion/SHA256SUMS" -OutFile $checksumPath

    Assert-Checksum -ArchivePath $archivePath -ChecksumPath $checksumPath -ArchiveName $archiveName

    Expand-Archive -LiteralPath $archivePath -DestinationPath $workDir -Force
    $extracted = Join-Path $workDir "taiga_${numericVersion}_windows_${architecture}\taiga.exe"
    if (-not (Test-Path -LiteralPath $extracted)) { throw 'The archive did not contain taiga.exe.' }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $target = Join-Path $InstallDir 'taiga.exe'
    Copy-Item -LiteralPath $extracted -Destination $target -Force

    $reported = (& $target version) | Select-Object -First 1
    Write-Host "Installed $reported to $target"

    Add-PathEntry -Directory $InstallDir
    Write-Host 'Shell completion: taiga completion powershell'
} finally {
    Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
}
