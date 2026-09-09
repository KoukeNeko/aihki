#Requires -Version 5.1
<#
.SYNOPSIS
    Remove the Taiga CLI from Windows.

.DESCRIPTION
    The binary and its PATH entry go by default. Configuration and stored
    credentials stay unless -Purge is given, because uninstalling is often one
    step of an upgrade and silently discarding a login would be a poor trade to
    make on the user's behalf.

.PARAMETER InstallDir
    Directory to remove. Defaults to %LOCALAPPDATA%\Programs\taiga.

.PARAMETER Purge
    Also remove the configuration directory and credentials from Windows
    Credential Manager, including those left by the pre-rename name.

.PARAMETER DryRun
    Report what would be removed and change nothing.

.EXAMPLE
    irm https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/uninstall.ps1 | iex

.EXAMPLE
    .\uninstall.ps1 -Purge
#>
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\taiga'),
    [switch]$Purge,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

$Binary = 'taiga'
$LegacyName = 'taiga-cli'

function Remove-Target {
    param([string]$Path, [string]$Description)
    if (-not (Test-Path -LiteralPath $Path)) { return }
    if ($DryRun) {
        Write-Host "would remove $Description $Path"
        return
    }
    Remove-Item -LiteralPath $Path -Recurse -Force
    Write-Host "removed $Description $Path"
}

function Remove-PathEntry {
    param([string]$Directory)
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) { return }
    $entries = $userPath -split ';' | Where-Object { $_ -ne '' }
    if ($entries -notcontains $Directory) { return }
    if ($DryRun) {
        Write-Host "would remove $Directory from your user PATH"
        return
    }
    $kept = $entries | Where-Object { $_ -ne $Directory }
    [Environment]::SetEnvironmentVariable('Path', ($kept -join ';'), 'User')
    Write-Host "removed $Directory from your user PATH"
}

# Credentials live in Windows Credential Manager rather than a file, stored
# under a target of the form service:account, so they are enumerated and
# removed by prefix instead of guessed at.
function Remove-StoredCredentials {
    $targets = @()
    foreach ($line in (cmdkey /list 2>$null)) {
        if ($line -match 'Target:\s*(.+)$') {
            $target = $Matches[1].Trim()
            foreach ($service in @($Binary, $LegacyName)) {
                if ($target -like "*$service`:*") { $targets += $target }
            }
        }
    }
    $targets = $targets | Select-Object -Unique
    if ($targets.Count -eq 0) {
        Write-Host 'no stored credentials found'
        return
    }
    foreach ($target in $targets) {
        if ($DryRun) {
            Write-Host "would remove credential $target"
            continue
        }
        cmdkey /delete:$target | Out-Null
        Write-Host "removed credential $target"
    }
}

Remove-Target -Path $InstallDir -Description 'install directory'
Remove-PathEntry -Directory $InstallDir

if ($Purge) {
    foreach ($name in @($Binary, $LegacyName)) {
        Remove-Target -Path (Join-Path $env:APPDATA $name) -Description 'configuration'
    }
    Remove-StoredCredentials
    Write-Host ''
    Write-Host "A repository pinned with ``$Binary project use --local`` keeps its setting in"
    Write-Host 'its own .git/config. Clear it there with:'
    Write-Host ''
    Write-Host "    git config --local --remove-section $Binary"
} else {
    Write-Host ''
    Write-Host 'Configuration and credentials were kept. Pass -Purge to remove them too.'
}
