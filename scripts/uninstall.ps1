# Claude Mnemonic - Windows Uninstallation Script
# Usage: irm https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/uninstall.ps1 | iex
#
# Options:
#   -KeepData    Keep the data directory (~/.claude-mnemonic/)
#   -Purge       Remove everything including data (default)

param(
    [switch]$KeepData,
    [switch]$Purge
)

$ErrorActionPreference = "Stop"

# Configuration
$InstallDir = "$env:USERPROFILE\.claude\plugins\marketplaces\claude-mnemonic"
$CacheDir = "$env:USERPROFILE\.claude\plugins\cache\claude-mnemonic"
$DataDir = "$env:USERPROFILE\.claude-mnemonic"
$PluginsFile = "$env:USERPROFILE\.claude\plugins\installed_plugins.json"
$SettingsFile = "$env:USERPROFILE\.claude\settings.json"
$MarketplacesFile = "$env:USERPROFILE\.claude\plugins\known_marketplaces.json"
$PluginKey = "claude-mnemonic@claude-mnemonic"

function Write-Info { param($Message) Write-Host "[INFO] $Message" -ForegroundColor Blue }
function Write-Success { param($Message) Write-Host "[OK] $Message" -ForegroundColor Green }
function Write-Warn { param($Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }

Write-Host ""
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "       Claude Mnemonic - Windows Uninstallation Script          " -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host ""

# Stop worker processes
Write-Info "Stopping worker processes..."
Get-Process | Where-Object { $_.ProcessName -like "*worker*" -and $_.Path -like "*claude-mnemonic*" } | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1
Write-Success "Worker processes stopped"

# Remove plugin directories
Write-Info "Removing plugin directories..."

if (Test-Path $InstallDir) {
    Remove-Item -Recurse -Force $InstallDir -ErrorAction SilentlyContinue
    Write-Success "Removed $InstallDir"
} else {
    Write-Info "Plugin directory not found (already removed)"
}

if (Test-Path $CacheDir) {
    Remove-Item -Recurse -Force $CacheDir -ErrorAction SilentlyContinue
    Write-Success "Removed $CacheDir"
}

# Remove from Claude Code configuration
Write-Info "Cleaning up Claude Code configuration..."

try {
    if (Test-Path $PluginsFile) {
        $Plugins = Get-Content $PluginsFile -Raw | ConvertFrom-Json
        if ($Plugins.plugins.PSObject.Properties[$PluginKey]) {
            $Plugins.plugins.PSObject.Properties.Remove($PluginKey)
            $Plugins | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $PluginsFile
            Write-Success "Removed from installed_plugins.json"
        }
    }

    if (Test-Path $SettingsFile) {
        $Settings = Get-Content $SettingsFile -Raw | ConvertFrom-Json
        $modified = $false
        if ($Settings.enabledPlugins -and $Settings.enabledPlugins.PSObject.Properties[$PluginKey]) {
            $Settings.enabledPlugins.PSObject.Properties.Remove($PluginKey)
            $modified = $true
        }
        # Remove statusline if it's ours
        if ($Settings.statusLine -and $Settings.statusLine.command -match "claude-mnemonic") {
            $Settings.PSObject.Properties.Remove("statusLine")
            $modified = $true
        }
        if ($modified) {
            $Settings | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $SettingsFile
            Write-Success "Removed from settings.json (including statusline)"
        }
    }

    if (Test-Path $MarketplacesFile) {
        $Marketplaces = Get-Content $MarketplacesFile -Raw | ConvertFrom-Json
        if ($Marketplaces.PSObject.Properties["claude-mnemonic"]) {
            $Marketplaces.PSObject.Properties.Remove("claude-mnemonic")
            $Marketplaces | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $MarketplacesFile
            Write-Success "Removed from known_marketplaces.json"
        }
    }
} catch {
    Write-Warn "Error cleaning up configuration files: $_"
}

# Handle data directory
if (Test-Path $DataDir) {
    if ($KeepData) {
        Write-Warn "Keeping data directory: $DataDir"
        Write-Warn "To remove it later, run: Remove-Item -Recurse -Force $DataDir"
    } else {
        Write-Info "Removing data directory..."
        Remove-Item -Recurse -Force $DataDir -ErrorAction SilentlyContinue
        Write-Success "Removed $DataDir"
    }
}

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "                Uninstallation Complete!                        " -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""

if ($KeepData) {
    Write-Host "  Data preserved at: $DataDir" -ForegroundColor White
    Write-Host "  To reinstall: irm .../install.ps1 | iex" -ForegroundColor White
    Write-Host ""
}

Write-Success "Claude Mnemonic has been uninstalled"
