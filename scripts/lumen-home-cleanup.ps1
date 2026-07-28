<#
.SYNOPSIS
    Lumen HOME selective compression (K3 quantization-inspired).
.DESCRIPTION
    Hot files untouched, cold files compressed, obsolete files deleted.
    Like K3: attention=BF16, Linear=MXFP4, pruned experts.
.PARAMETER DryRun
    Show without doing.
.PARAMETER Aggressive
    Also clean build artifacts over 7 days old.
#>
param([switch]$DryRun, [switch]$Aggressive)
$ErrorActionPreference = "Continue"
$LumenHome = if ($env:LUMEN_HOME) { $env:LUMEN_HOME } else { "$env:USERPROFILE\.lumen" }
$GrokHome  = if ($env:GROK_HOME)  { $env:GROK_HOME  } else { "$env:USERPROFILE\.grok"  }
$TargetDir = Join-Path (Split-Path $PSScriptRoot) "..\agent\target"
$totalSaved = 0; $totalItems = 0
Write-Host "=== Lumen Selective Cleanup ==="

# Tier 1: always keep (BF16)
$alwaysKeep = @("config.toml","lumen.toml","*.current.jsonl","skills/*.md","plugins/*")
# Tier 2: compress if old (MXFP4)
$compressIfOld = @{ Patterns = @("sessions/*.jsonl","logs/*.log","*.jsonl.bak","cache/**"); MaxDays = 30 }
# Tier 3: delete if obsolete
$safeToDelete = @{ Patterns = @("**/*.tmp","**/*.temp","**/*.bak.unknown","proxy.log.*"); MaxDays = 7 }

Write-Host "Tier 1 - Hot (keep):"
foreach ($p in $alwaysKeep) {
    foreach ($h in @($LumenHome,$GrokHome)) {
        if (-not (Test-Path $h)) { continue }
        Get-ChildItem $h -Filter $p -Recurse -Depth 3 -ErrorAction SilentlyContinue | ForEach-Object {
            Write-Host "  KEEP $($_.FullName) ($([Math]::Round($_.Length/1KB,0)) KB)"
        }
    }
}

Write-Host "Tier 2 - Cold (compress old):"
foreach ($p in $compressIfOld.Patterns) {
    foreach ($h in @($LumenHome,$GrokHome)) {
        if (-not (Test-Path $h)) { continue }
        Get-ChildItem $h -Filter $p -Recurse -Depth 3 -ErrorAction SilentlyContinue | ForEach-Object {
            $days = ((Get-Date) - $_.LastWriteTime).TotalDays
            if ($days -lt $compressIfOld.MaxDays -or $_.Length -lt 1024) { continue }
            Write-Host "  COLD $($_.FullName) ($([Math]::Round($_.Length/1KB,0)) KB, $([Math]::Round($days,0))d)"
            if (-not $DryRun) {
                $zip = "$($_.FullName).lumen.gz"
                try {
                    $fin = [System.IO.File]::OpenRead($_.FullName)
                    $fout = [System.IO.File]::Create($zip)
                    $gz = New-Object System.IO.Compression.GZipStream($fout,[System.IO.Compression.CompressionMode]::Compress)
                    $fin.CopyTo($gz); $gz.Close(); $fout.Close(); $fin.Close()
                    $nz = (Get-Item $zip).Length
                    if ($nz -lt $_.Length) { $totalSaved += ($_.Length - $nz); Remove-Item $_.FullName -Force; Rename-Item $zip $_.FullName -Force }
                    else { Remove-Item $zip -Force }
                } catch {}
                $totalItems++
            }
        }
    }
}

Write-Host "Tier 3 - Obsolete (delete):"
foreach ($p in $safeToDelete.Patterns) {
    foreach ($h in @($LumenHome,$GrokHome)) {
        if (-not (Test-Path $h)) { continue }
        Get-ChildItem $h -Filter $p -Recurse -Depth 3 -ErrorAction SilentlyContinue | ForEach-Object {
            $days = ((Get-Date) - $_.LastWriteTime).TotalDays
            if ($days -lt $safeToDelete.MaxDays) { continue }
            Write-Host "  DEL $($_.FullName) ($([Math]::Round($_.Length/1KB,0)) KB, $([Math]::Round($days,0))d)"
            if (-not $DryRun) { $totalSaved += $_.Length; Remove-Item $_.FullName -Force; $totalItems++ }
        }
    }
}

if ($Aggressive -and (Test-Path $TargetDir)) {
    Write-Host "Aggressive - build cache:"
    Get-ChildItem $TargetDir -Recurse -Depth 5 -Include "*.o","*.rmeta","*.d" -ErrorAction SilentlyContinue | ForEach-Object {
        $days = ((Get-Date) - $_.LastWriteTime).TotalDays
        if ($days -lt 7) { continue }
        Write-Host "  CLEAN $($_.FullName) ($([Math]::Round($_.Length/1KB,0)) KB)"
        if (-not $DryRun) { $totalSaved += $_.Length; Remove-Item $_.FullName -Force; $totalItems++ }
    }
}

$mb = [Math]::Round($totalSaved/1MB, 1)
Write-Host "=== $totalItems items, ${mb} MB saved ==="
