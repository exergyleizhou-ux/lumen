<#
.SYNOPSIS
    Self-update Lumen to the latest version from GitHub releases.

.DESCRIPTION
    Checks the GitHub releases page for the latest Windows binary, downloads
    it, verifies SHA-256, and replaces the current lumen.exe. Backs up the
    old binary before replacing.

.PARAMETER Check
    Only check for updates, don't install.

.PARAMETER Force
    Skip version comparison, always reinstall.

.EXAMPLE
    .\lumen-update.ps1
    .\lumen-update.ps1 -Check
    .\lumen-update.ps1 -Force
#>

param(
    [switch]$Check,
    [switch]$Force
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$Green  = "$([char]27)[0;32m"
$Yellow = "$([char]27)[1;33m"
$Bold   = "$([char]27)[1m"
$Reset  = "$([char]27)[0m"

$Repo = "exergyleizhou-ux/lumen"

function Get-CurrentVersion {
    try {
        $output = & lumen --version 2>$null
        if ($output -match "lumen\s+([\d.]+)") {
            return $Matches[1]
        }
    } catch {}
    return $null
}

function Get-LatestVersion {
    try {
        $apiUrl = "https://api.github.com/repos/$Repo/releases/latest"
        $release = Invoke-RestMethod -Uri $apiUrl -TimeoutSec 10
        $tag = $release.tag_name
        $version = $tag -replace '^v', ''

        # Find Windows x64 asset (exact match only)
        $asset = $release.assets | Where-Object { $_.name -match 'lumen-windows-amd64\.zip$' } | Select-Object -First 1
        if (-not $asset) {
            Write-Host "No Windows x64 asset found in release $($release.tag_name)"
            exit 1
        }

        return @{
            Version = $version
            Tag     = $tag
            Url     = $asset.browser_download_url
            Size    = $asset.size
            Name    = $asset.name
        }
    } catch {
        Write-Host "Cannot reach GitHub API — checking local build instead..."
        return $null
    }
}

function Get-LumenExePath {
    $cmd = Get-Command lumen -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }

    $commonPaths = @(
        "$env:LOCALAPPDATA\Lumen\lumen.exe",
        "C:\Lumen\lumen.exe"
    )
    foreach ($p in $commonPaths) {
        if (Test-Path $p) { return $p }
    }
    throw "lumen.exe not found"
}

# ── Main ──
Write-Host ""
Write-Host "$Bold=== Lumen Self-Update ===$Reset"
Write-Host ""

$current = Get-CurrentVersion
if ($current) {
    Write-Host "  Current:  $current"
} else {
    Write-Host "  ${Yellow}Current version unknown${Reset}"
}

$latest = Get-LatestVersion
if (-not $latest) {
    Write-Host "  ${Yellow}Could not check latest version (offline?)${Reset}"
    exit 1
}

Write-Host "  Latest:   $($latest.Version)"
Write-Host "  Release:  $($latest.Tag)"
Write-Host ""

if ($Check) {
    if ($current -and [version]$current -ge [version]$latest.Version) {
        Write-Host "${Green}✓ Lumen is up to date${Reset}"
    } else {
        Write-Host "${Yellow}Update available: $($latest.Version)${Reset}"
        Write-Host "Run without -Check to install."
    }
    exit 0
}

if (-not $Force -and $current -and [version]$current -ge [version]$latest.Version) {
    Write-Host "${Green}✓ Already up to date${Reset}"
    exit 0
}

# Download
Write-Host "Downloading $($latest.Name) ($([Math]::Round($latest.Size/1MB,1)) MB)..."
$tempDir = "$env:TEMP\lumen-update"
New-Item -ItemType Directory -Force -Path $tempDir | Out-Null
$tempZip = Join-Path $tempDir $latest.Name

try {
    Invoke-WebRequest -Uri $latest.Url -OutFile $tempZip -TimeoutSec 120
} catch {
    Write-Host "Download failed. Cannot update without network."
    Write-Host "For manual Windows build: cd agent && cargo rustc --locked --release -p xai-grok-pager-bin --features release-dist --target x86_64-pc-windows-msvc -- -C link-arg=/DEBUG:NONE"
    exit 1
}

# Extract
Write-Host "Extracting..."
$extractDir = Join-Path $tempDir "extract"
New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
Expand-Archive -Path $tempZip -DestinationPath $extractDir -Force

# Find lumen.exe in extracted files
$newBinary = Get-ChildItem -Path $extractDir -Recurse -Filter "lumen.exe" | Select-Object -First 1
if (-not $newBinary) {
    Write-Host "ERROR: No lumen.exe found in release archive"
    exit 1
}

# Verify SHA-256 (MUST be present and match)
$shaFile = Get-ChildItem -Path $extractDir -Recurse -Filter "SHA256SUMS.txt" | Select-Object -First 1
if (-not $shaFile) {
    Write-Host "ERROR: SHA256SUMS.txt not found in release archive — cannot verify integrity"
    exit 1
}
$expected = (Get-Content $shaFile.FullName | Select-String "lumen.exe").ToString().Split()[0]
$actual = (Get-FileHash $newBinary.FullName -Algorithm SHA256).Hash
if (-not $expected) {
    Write-Host "ERROR: Could not find SHA-256 for lumen.exe in SHA256SUMS.txt"
    exit 1
}
if ($actual -ne $expected) {
    Write-Host "ERROR: SHA-256 mismatch!"
    Write-Host "  Expected: $expected"
    Write-Host "  Actual:   $actual"
    exit 1
}
Write-Host "  ${Green}✓${Reset} SHA-256 verified"

# Backup old binary
$lumenPath = Get-LumenExePath
$backup = "$lumenPath.bak.$(if ($current) { $current } else { 'unknown' })"
Copy-Item -Force $lumenPath $backup
Write-Host "  ${Green}✓${Reset} Backup saved: $backup"

# Replace
Copy-Item -Force $newBinary.FullName $lumenPath
$newVer = & $lumenPath --version 2>$null
Write-Host "  ${Green}✓${Reset} Updated to: $newVer"

# Cleanup
Remove-Item -Recurse -Force $tempDir

Write-Host ""
Write-Host "=" * 50
Write-Host "$Bold$Green  Lumen updated successfully!$Reset"
Write-Host "  Old backup: $backup"
Write-Host "=" * 50
Write-Host ""
