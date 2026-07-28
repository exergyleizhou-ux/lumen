<#
.SYNOPSIS
    Lumen Windows Network Toolkit — diagnose and bypass network restrictions.
.DESCRIPTION
    GFW / corporate firewall blocking GitHub / crates.io
    This toolkit auto-detects proxies, configures git/cargo, and provides
    fallback push mechanisms.  Born from real-world Windows development in
    restricted network environments (China GFW, 2026-07).

    Capabilities:
      - Scan for local HTTP/SOCKS5 proxy ports (V2Ray, Clash, Shadowsocks)
      - Extract git credentials from Windows Credential Manager
      - Configure git proxy automatically
      - Fallback push via GitHub API (bypasses git protocol)
      - Cargo mirror configuration (TUNA/USTC/SJTU)
      - Network connectivity diagnostics (GitHub, crates.io, DNS)

.PARAMETER AutoFix
    Automatically detect and configure the best available proxy.
.PARAMETER ScanOnly
    Only scan for proxies, don't configure anything.
.PARAMETER ResetProxy
    Remove all proxy configuration from git and cargo.
.PARAMETER ApiPush
    Push current branch using GitHub API (bypasses git protocol issues).
.PARAMETER Branch
    Branch name to push (used with -ApiPush). Default: current branch.
.EXAMPLE
    .\lumen-network-toolkit.ps1 -AutoFix
    .\lumen-network-toolkit.ps1 -ScanOnly
    .\lumen-network-toolkit.ps1 -ResetProxy
    .\lumen-network-toolkit.ps1 -ApiPush -Branch windows-fix-ps-scripts
#>

param(
    [switch]$AutoFix,
    [switch]$ScanOnly,
    [switch]$ResetProxy,
    [switch]$ApiPush,
    [string]$Branch
)

$ErrorActionPreference = "Continue"
$Green  = "$([char]27)[0;32m"
$Yellow = "$([char]27)[1;33m"
$Red    = "$([char]27)[0;31m"
$Bold   = "$([char]27)[1m"
$Reset  = "$([char]27)[0m"

Write-Host ""
Write-Host "$Bold=== Lumen Windows Network Toolkit ===$Reset"
Write-Host ""

# ---
# PROXY SCANNER
# ---
function Find-HttpProxy {
    <#
    .SYNOPSIS
        Scan common proxy ports for a working HTTP proxy.
        Tests each port by requesting https://github.com.
        V2RayN default: SOCKS5 10808, HTTP 10809.
        Clash default: HTTP 7890, SOCKS5 7891.
    #>
    param([int]$StartPort = 10800, [int]$EndPort = 11000, [int]$TimeoutSec = 2)

    Write-Host "${Bold}Scanning for HTTP proxy (ports $StartPort-$EndPort)...$Reset"

    # Priority ports first
    $priorityPorts = @(10809, 7890, 8080, 3128, 8118, 10080)
    foreach ($p in $priorityPorts) {
        try {
            $r = Invoke-WebRequest -Uri "https://github.com" `
                -TimeoutSec $TimeoutSec -UseBasicParsing `
                -Proxy "http://127.0.0.1:$p"
            Write-Host "  ${Green}FOUND:$Reset HTTP proxy on port $p"
            return $p
        } catch {}
    }

    # Full scan if priority fails
    foreach ($p in $StartPort..$EndPort) {
        if ($priorityPorts -contains $p) { continue }
        try {
            $r = Invoke-WebRequest -Uri "https://github.com" `
                -TimeoutSec 1 -UseBasicParsing `
                -Proxy "http://127.0.0.1:$p"
            Write-Host "  ${Green}FOUND:$Reset HTTP proxy on port $p"
            return $p
        } catch {}
    }
    Write-Host "  ${Yellow}No HTTP proxy found$Reset"
    return $null
}

function Find-SocksProxy {
    <#
    .SYNOPSIS
        Detect SOCKS5 proxy by attempting TCP connection.
        SOCKS5 proxies cannot be tested with Invoke-WebRequest directly.
        V2RayN default: SOCKS5 10808. Clash default: SOCKS5 7891.
    #>
    param([int]$StartPort = 10800, [int]$EndPort = 11000)

    $priorityPorts = @(10808, 7891, 1080, 1087)
    foreach ($p in $priorityPorts) {
        try {
            $tcp = New-Object System.Net.Sockets.TcpClient
            $tcp.Connect("127.0.0.1", $p)
            Write-Host "  ${Yellow}SOCKS5:$Reset port $p responds (SOCKS5 -- needs HTTP bridge for git)"
            $tcp.Close()
            $tcp.Dispose()
            return $p
        } catch {}
    }
    return $null
}

# ---
# GIT CREDENTIAL EXTRACTION
# ---
function Get-GitCredential {
    <#
    .SYNOPSIS
        Extract GitHub credentials from Windows Credential Manager
        via git credential-manager. Used for API-based fallback push.
    #>
    $input = "protocol=https`nhost=github.com`n`n"
    $cred = $input | git credential-manager get 2>$null
    if (-not $cred) {
        $cred = $input | git credential fill 2>$null
    }
    if (-not $cred) { return $null }

    $user = ($cred | Select-String "username=(.*)").Matches.Groups[1].Value
    $pass = ($cred | Select-String "password=(.*)").Matches.Groups[1].Value

    if ($user -and $pass) {
        return @{ Username = $user; Token = $pass }
    }
    return $null
}

# ---
# AUTO-FIX: DETECT + CONFIGURE
# ---
function Invoke-AutoFix {
    Write-Host "${Bold}[AutoFix] Detecting best network configuration...$Reset"
    Write-Host ""

    # 1. Check direct connectivity
    Write-Host "1. Direct connectivity..."
    try {
        $r = Invoke-WebRequest -Uri "https://github.com" -TimeoutSec 5 -UseBasicParsing
        Write-Host "   ${Green}Direct connection OK$Reset — no proxy needed"
        git config --global --unset http.proxy 2>$null
        git config --global --unset https.proxy 2>$null
        return $true
    } catch {
        Write-Host "   ${Yellow}Direct connection blocked$Reset"
    }

    # 2. Check Windows system proxy
    Write-Host "2. Windows system proxy..."
    $sysProxy = (Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -ErrorAction SilentlyContinue).ProxyServer
    $sysEnabled = (Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -ErrorAction SilentlyContinue).ProxyEnable
    if ($sysEnabled -eq 1 -and $sysProxy) {
        Write-Host "   System proxy: $sysProxy (enabled)"
        try {
            $r = Invoke-WebRequest -Uri "https://github.com" -TimeoutSec 5 -UseBasicParsing -Proxy "http://$sysProxy"
            Write-Host "   ${Green}System proxy works!$Reset Configuring git..."
            git config --global http.proxy "http://$sysProxy"
            git config --global https.proxy "http://$sysProxy"
            return $true
        } catch {
            Write-Host "   System proxy unreachable (may be SOCKS5)"
        }
    } else {
        Write-Host "   No system proxy configured"
    }

    # 3. Scan for local HTTP proxy
    Write-Host "3. Local HTTP proxy scan..."
    $httpPort = Find-HttpProxy
    if ($httpPort) {
        git config --global http.proxy "http://127.0.0.1:${httpPort}"
        git config --global https.proxy "http://127.0.0.1:${httpPort}"
        Write-Host "   ${Green}Git configured to use 127.0.0.1:${httpPort}$Reset"
        return $true
    }

    # 4. SOCKS5 detected but no HTTP — note limitation
    $socksPort = Find-SocksProxy
    if ($socksPort) {
        Write-Host "   ${Yellow}SOCKS5 on port $socksPort found, but git needs HTTP proxy.$Reset"
        Write-Host "   ${Yellow}Enable HTTP inbound in V2RayN/Clash (typically port 10809 or 7890)$Reset"
        Write-Host "   ${Yellow}Or use -ApiPush to push via GitHub API$Reset"
    }

    # 5. Cargo mirror configuration
    Write-Host "4. Cargo mirror..."
    $cargoConfig = "$env:USERPROFILE\.cargo\config.toml"
    $cargoDir = "$env:USERPROFILE\.cargo"
    if (-not (Test-Path $cargoDir)) { New-Item -ItemType Directory -Force $cargoDir | Out-Null }
    if (-not (Test-Path $cargoConfig) -or (Get-Content $cargoConfig -Raw) -notmatch "replace-with") {
        Write-Host "   Setting up TUNA cargo mirror..."
        @"
[source.crates-io]
replace-with = 'tuna-sparse'

[source.tuna-sparse]
registry = "sparse+https://mirrors.tuna.tsinghua.edu.cn/crates.io-index/"

[net]
git-fetch-with-cli = true
"@ | Set-Content $cargoConfig
        Write-Host "   ${Green}Cargo mirror configured (TUNA sparse)$Reset"
    } else {
        Write-Host "   Cargo mirror already configured"
    }

    return $false
}

# ---
# API-BASED FALLBACK PUSH
# ---
function Invoke-ApiPush {
    <#
    .SYNOPSIS
        Push the current HEAD via GitHub REST API directly.
        Bypasses git protocol restrictions — only needs HTTPS connectivity
        to api.github.com (which may be less aggressively blocked).
        Requires git credentials in Windows Credential Manager.
    #>
    param([string]$BranchName)

    Write-Host "${Bold}[ApiPush] Pushing via GitHub API...$Reset"

    # Get credential
    $cred = Get-GitCredential
    if (-not $cred) {
        Write-Host "${Red}No git credentials found.$Reset"
        Write-Host "Push normally first to save credentials, then retry."
        return $false
    }

    $token = $cred.Token

    # Get branch name
    if (-not $BranchName) {
        $BranchName = (git rev-parse --abbrev-ref HEAD 2>$null)
        if (-not $BranchName) {
            Write-Host "${Red}Cannot determine current branch$Reset"
            return $false
        }
    }

    # Get HEAD SHA
    $sha = (git rev-parse HEAD 2>$null).Trim()
    if (-not $sha) {
        Write-Host "${Red}Cannot determine HEAD SHA$Reset"
        return $false
    }

    Write-Host "  Branch: $BranchName"
    Write-Host "  SHA: $sha"

    # First try: check if ref exists
    $owner = "exergyleizhou-ux"
    $repo = "lumen"
    $refPath = "heads/$BranchName"
    $headers = @{
        Authorization = "Bearer $token"
        Accept = "application/vnd.github+json"
    }

    $existingSha = $null
    try {
        $ref = Invoke-RestMethod `
            -Uri "https://api.github.com/repos/$owner/$repo/git/refs/$refPath" `
            -Headers $headers -TimeoutSec 30
        $existingSha = $ref.object.sha
        Write-Host "  Existing remote SHA: $existingSha"
    } catch {
        Write-Host "  Ref does not exist yet — will create"
    }

    # Update/create ref
    $body = @{ sha = $sha; force = $false } | ConvertTo-Json -Compress
    try {
        if ($existingSha) {
            $result = Invoke-RestMethod `
                -Uri "https://api.github.com/repos/$owner/$repo/git/refs/$refPath" `
                -Method Patch -Headers $headers -Body $body -ContentType "application/json" -TimeoutSec 30
        } else {
            $body = @{ ref = "refs/$refPath"; sha = $sha } | ConvertTo-Json -Compress
            $result = Invoke-RestMethod `
                -Uri "https://api.github.com/repos/$owner/$repo/git/refs" `
                -Method Post -Headers $headers -Body $body -ContentType "application/json" -TimeoutSec 30
        }
        Write-Host "  ${Green}API push successful!$Reset"
        Write-Host "  Ref: $($result.ref) -> $($result.object.sha)"
        return $true
    } catch {
        Write-Host "${Red}API push failed:$Reset $($_.Exception.Message)"
        Write-Host "  Note: API push only updates the ref pointer."
        Write-Host "  All commit objects must already exist on the remote."
        Write-Host "  Run 'git push' at least once first to upload objects."
        return $false
    }
}

# ---
# CONNECTIVITY DIAGNOSTICS
# ---
function Show-Diagnostics {
    Write-Host "${Bold}=== Network Diagnostics ===$Reset"
    Write-Host ""

    # DNS resolution
    Write-Host "--- DNS ---"
    try {
        $ips = [System.Net.Dns]::GetHostAddresses("github.com")
        Write-Host "  github.com: $($ips[0])"
    } catch { Write-Host "  ${Red}github.com: DNS FAIL$Reset" }
    try {
        $ips = [System.Net.Dns]::GetHostAddresses("static.crates.io")
        Write-Host "  static.crates.io: $($ips[0])"
    } catch { Write-Host "  ${Yellow}static.crates.io: DNS FAIL$Reset" }
    try {
        $ips = [System.Net.Dns]::GetHostAddresses("api.github.com")
        Write-Host "  api.github.com: $($ips[0])"
    } catch { Write-Host "  ${Yellow}api.github.com: DNS FAIL$Reset" }

    # TCP connectivity
    Write-Host "--- TCP ---"
    $targets = @(
        @{Host="github.com"; Port=443; Name="GitHub HTTPS"},
        @{Host="github.com"; Port=22; Name="GitHub SSH"},
        @{Host="static.crates.io"; Port=443; Name="crates.io CDN"},
        @{Host="api.github.com"; Port=443; Name="GitHub API"}
    )
    foreach ($t in $targets) {
        try {
            $tcp = New-Object System.Net.Sockets.TcpClient
            $tcp.Connect($t.Host, $t.Port)
            Write-Host "  $($t.Name): ${Green}OK$Reset ($($t.Host):$($t.Port))"
            $tcp.Close()
            $tcp.Dispose()
        } catch {
            Write-Host "  $($t.Name): ${Red}BLOCKED$Reset"
        }
    }

    # Windows system proxy
    Write-Host "--- System Proxy ---"
    $proxyServer = (Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -ErrorAction SilentlyContinue).ProxyServer
    $proxyEnable = (Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -ErrorAction SilentlyContinue).ProxyEnable
    if ($proxyEnable -eq 1 -and $proxyServer) {
        Write-Host "  ${Green}Enabled:$Reset $proxyServer"
    } else {
        Write-Host "  Not configured"
    }

    # Git proxy config
    Write-Host "--- Git Proxy ---"
    $gitHttp = git config --global http.proxy 2>$null
    $gitHttps = git config --global https.proxy 2>$null
    if ($gitHttp) { Write-Host "  http.proxy: $gitHttp" } else { Write-Host "  http.proxy: (not set)" }
    if ($gitHttps) { Write-Host "  https.proxy: $gitHttps" } else { Write-Host "  https.proxy: (not set)" }

    # Cargo mirror
    Write-Host "--- Cargo Mirror ---"
    $cargoConfig = "$env:USERPROFILE\.cargo\config.toml"
    if (Test-Path $cargoConfig) {
        $mirror = Select-String -Path $cargoConfig -Pattern 'replace-with' 2>$null
        if ($mirror) { Write-Host "  ${Green}$($mirror.Line.Trim())$Reset" } else { Write-Host "  No mirror configured" }
    } else {
        Write-Host "  No cargo config"
    }

    # VPN processes
    Write-Host "--- VPN/Proxy Processes ---"
    $vpnNames = @("v2ray*", "xray*", "sing-box*", "clash*", "hysteria*", "nekoray*", "shadowsocks*")
    $found = Get-Process -Name $vpnNames -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found) {
        Write-Host "  ${Green}Detected:$Reset"
        Get-Process -Name $vpnNames -ErrorAction SilentlyContinue | ForEach-Object {
            Write-Host "    $($_.Name) (PID: $($_.Id))"
        }
    } else {
        Write-Host "  ${Yellow}No VPN/proxy process detected$Reset"
    }
}

function Invoke-ResetProxy {
    git config --global --unset http.proxy 2>$null
    git config --global --unset https.proxy 2>$null
    git config --global --unset core.sshCommand 2>$null
    Write-Host "${Green}Git proxy configuration cleared$Reset"
}

# ---
# MAIN
# ---
if ($ResetProxy) {
    Invoke-ResetProxy
} elseif ($ApiPush) {
    $null = Invoke-ApiPush -BranchName $Branch
} elseif ($ScanOnly) {
    Show-Diagnostics
    Write-Host ""
    $http = Find-HttpProxy
    $socks = Find-SocksProxy
} elseif ($AutoFix) {
    Show-Diagnostics
    Write-Host ""
    $result = Invoke-AutoFix
} else {
    Show-Diagnostics
    Write-Host ""
    Write-Host "Usage:"
    Write-Host "  $Bold-AutoFix$Reset     Auto-detect and configure best proxy"
    Write-Host "  $Bold-ScanOnly$Reset    Show diagnostics only"
    Write-Host "  $Bold-ResetProxy$Reset  Clear git proxy config"
    Write-Host "  $Bold-ApiPush$Reset     Push via GitHub API (fallback)"
    Write-Host "  $Bold-Branch$Reset NAME Branch for -ApiPush"
}

