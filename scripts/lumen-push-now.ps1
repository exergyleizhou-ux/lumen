<#
.SYNOPSIS
    Auto-push daemon — waits for GFW window, pushes, exits.
.DESCRIPTION
    Keeps retrying `git push` until it succeeds. Designed for networks
    where GitHub is intermittently blocked (GFW).
    No proxy needed — works with direct connections during window openings.
    Run it, leave it, come back when it's done.
.PARAMETER Branch
    Branch to push. Default: current branch.
.PARAMETER Interval
    Seconds between retries. Default: 30.
.PARAMETER MaxRetries
    Maximum retries before giving up. Default: 0 (unlimited).
.EXAMPLE
    .\lumen-push-now.ps1
    .\lumen-push-now.ps1 -Branch main -Interval 60 -MaxRetries 100
#>
param(
    [string]$Branch,
    [int]$Interval = 30,
    [int]$MaxRetries = 0
)

$ErrorActionPreference = "Continue"
try { $RepoRoot = git rev-parse --show-toplevel 2>$null } catch { $RepoRoot = $PSScriptRoot }
Set-Location $RepoRoot

if (-not $Branch) { $Branch = (git rev-parse --abbrev-ref HEAD 2>$null) }

# Check if there's anything to push
$localSha = (git rev-parse HEAD 2>$null).Trim()
$remoteSha = (git ls-remote origin "refs/heads/$Branch" 2>$null | ForEach-Object { ($_ -split '\s+')[0] }).Trim()

Write-Host "Lumen Push Daemon — waiting for network window..."
Write-Host "  Branch : $Branch"
Write-Host "  Local  : $localSha"
Write-Host "  Remote : $remoteSha"

if ($localSha -eq $remoteSha -and $remoteSha) {
    Write-Host "  Already up-to-date. Nothing to push."
    exit 0
}

$attempt = 0
while ($MaxRetries -eq 0 -or $attempt -lt $MaxRetries) {
    $attempt++
    $ts = Get-Date -Format "HH:mm:ss"
    
    try {
        $result = git push origin $Branch 2>&1
        if ($LASTEXITCODE -eq 0 -or "$result" -match "up-to-date|Everything") {
            Write-Host "[$ts] #$attempt SUCCESS — pushed to $Branch"
            exit 0
        }
    } catch {}

    if ($attempt % 5 -eq 0) {
        Write-Host "[$ts] #$attempt still waiting... (GFW window not open yet)"
    }
    
    Start-Sleep -Seconds $Interval
}

Write-Host "[$ts] Stopped after $MaxRetries attempts. Try again later."
exit 1
