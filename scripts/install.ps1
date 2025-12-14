# Claude Mnemonic - Windows Installation Script
# Usage: irm https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.ps1 | iex
#
# Or with a specific version:
# $env:MNEMONIC_VERSION = "v1.0.0"; irm https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.ps1 | iex

param(
    [string]$Version = $env:MNEMONIC_VERSION,
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"

# Configuration
$GitHubRepo = "lukaszraczylo/claude-mnemonic"
$InstallDir = "$env:USERPROFILE\.claude\plugins\marketplaces\claude-mnemonic"
$CacheDir = "$env:USERPROFILE\.claude\plugins\cache\claude-mnemonic\claude-mnemonic"
$PluginsFile = "$env:USERPROFILE\.claude\plugins\installed_plugins.json"
$SettingsFile = "$env:USERPROFILE\.claude\settings.json"
$MarketplacesFile = "$env:USERPROFILE\.claude\plugins\known_marketplaces.json"
$PluginKey = "claude-mnemonic@claude-mnemonic"

function Write-Info { param($Message) Write-Host "[INFO] $Message" -ForegroundColor Blue }
function Write-Success { param($Message) Write-Host "[OK] $Message" -ForegroundColor Green }
function Write-Warn { param($Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-Error { param($Message) Write-Host "[ERROR] $Message" -ForegroundColor Red; exit 1 }

function Get-LatestVersion {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$GitHubRepo/releases/latest"
        return $release.tag_name
    } catch {
        Write-Error "Failed to fetch latest version from GitHub: $_"
    }
}

function Stop-ExistingWorker {
    Write-Info "Stopping existing worker (if running)..."
    Get-Process | Where-Object { $_.ProcessName -like "*worker*" -and $_.Path -like "*claude-mnemonic*" } | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 1
}

function Install-Release {
    param([string]$Version)

    $TempDir = New-Item -ItemType Directory -Path "$env:TEMP\claude-mnemonic-$(Get-Random)" -Force

    try {
        # Construct download URL
        $VersionClean = $Version -replace "^v", ""
        $ArchiveName = "claude-mnemonic_${VersionClean}_windows_amd64.zip"
        $DownloadUrl = "https://github.com/$GitHubRepo/releases/download/$Version/$ArchiveName"

        Write-Info "Downloading $ArchiveName..."
        $ZipPath = Join-Path $TempDir "release.zip"
        Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath -UseBasicParsing

        Write-Info "Extracting archive..."
        Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

        Stop-ExistingWorker

        # Create installation directories
        Write-Info "Installing to $InstallDir..."
        New-Item -ItemType Directory -Path "$InstallDir\hooks" -Force | Out-Null
        New-Item -ItemType Directory -Path "$InstallDir\.claude-plugin" -Force | Out-Null

        # Copy binaries
        Copy-Item "$TempDir\worker.exe" "$InstallDir\" -Force
        Copy-Item "$TempDir\mcp-server.exe" "$InstallDir\" -Force
        Copy-Item "$TempDir\hooks\*" "$InstallDir\hooks\" -Force

        # Copy plugin configuration
        Copy-Item "$TempDir\.claude-plugin\*" "$InstallDir\.claude-plugin\" -Force

        Write-Success "Binaries installed to $InstallDir"
    } finally {
        Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
    }
}

function Register-Plugin {
    param([string]$Version)

    $Timestamp = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ss.000Z")
    $VersionClean = $Version -replace "^v", ""
    $CachePath = "$CacheDir\$VersionClean"

    # Ensure directories exist
    New-Item -ItemType Directory -Path "$env:USERPROFILE\.claude\plugins" -Force | Out-Null
    New-Item -ItemType Directory -Path $CachePath -Force | Out-Null

    # Create JSON files if they don't exist
    if (-not (Test-Path $PluginsFile)) {
        '{"version": 2, "plugins": {}}' | Out-File -Encoding UTF8 $PluginsFile
    }
    if (-not (Test-Path $SettingsFile)) {
        '{}' | Out-File -Encoding UTF8 $SettingsFile
    }
    if (-not (Test-Path $MarketplacesFile)) {
        '{}' | Out-File -Encoding UTF8 $MarketplacesFile
    }

    # Copy files to cache directory
    New-Item -ItemType Directory -Path "$CachePath\.claude-plugin" -Force | Out-Null
    New-Item -ItemType Directory -Path "$CachePath\hooks" -Force | Out-Null
    Copy-Item "$InstallDir\*" $CachePath -Recurse -Force -ErrorAction SilentlyContinue

    try {
        # Update installed_plugins.json
        $Plugins = Get-Content $PluginsFile -Raw | ConvertFrom-Json
        $PluginEntry = @(
            @{
                scope = "user"
                installPath = $CachePath
                version = $VersionClean
                installedAt = $Timestamp
                lastUpdated = $Timestamp
                isLocal = $true
            }
        )
        if (-not $Plugins.plugins) {
            $Plugins | Add-Member -NotePropertyName "plugins" -NotePropertyValue @{} -Force
        }
        $Plugins.plugins | Add-Member -NotePropertyName $PluginKey -NotePropertyValue $PluginEntry -Force
        $Plugins | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $PluginsFile
        Write-Success "Plugin registered in installed_plugins.json"

        # Update settings.json
        $Settings = Get-Content $SettingsFile -Raw | ConvertFrom-Json
        if (-not $Settings.enabledPlugins) {
            $Settings | Add-Member -NotePropertyName "enabledPlugins" -NotePropertyValue @{} -Force
        }
        $Settings.enabledPlugins | Add-Member -NotePropertyName $PluginKey -NotePropertyValue $true -Force
        $Settings | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $SettingsFile
        Write-Success "Plugin enabled in settings.json"

        # Update known_marketplaces.json
        $Marketplaces = Get-Content $MarketplacesFile -Raw | ConvertFrom-Json
        $MarketplaceEntry = @{
            source = @{
                source = "directory"
                path = $InstallDir
            }
            installLocation = $InstallDir
            lastUpdated = $Timestamp
        }
        $Marketplaces | Add-Member -NotePropertyName "claude-mnemonic" -NotePropertyValue $MarketplaceEntry -Force
        $Marketplaces | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $MarketplacesFile
        Write-Success "Marketplace registered in known_marketplaces.json"
    } catch {
        Write-Warn "Plugin registration encountered an error: $_"
    }
}

function Start-Worker {
    $WorkerPath = Join-Path $InstallDir "worker.exe"
    if (-not (Test-Path $WorkerPath)) {
        Write-Error "Worker binary not found at $WorkerPath"
    }

    Write-Info "Starting worker service..."
    Start-Process -FilePath $WorkerPath -WindowStyle Hidden

    Start-Sleep -Seconds 2

    try {
        $response = Invoke-WebRequest -Uri "http://localhost:37777/health" -UseBasicParsing -TimeoutSec 5
        Write-Success "Worker started successfully at http://localhost:37777"
    } catch {
        Write-Warn "Worker may not have started properly. Check the process manually."
    }
}

function Uninstall-ClaudeMnemonic {
    param([switch]$KeepData)

    Write-Info "Uninstalling Claude Mnemonic..."

    Stop-ExistingWorker

    # Remove directories
    Remove-Item -Recurse -Force $InstallDir -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force $CacheDir -ErrorAction SilentlyContinue

    # Remove from JSON files
    try {
        if (Test-Path $PluginsFile) {
            $Plugins = Get-Content $PluginsFile -Raw | ConvertFrom-Json
            $Plugins.plugins.PSObject.Properties.Remove($PluginKey)
            $Plugins | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $PluginsFile
        }
        if (Test-Path $SettingsFile) {
            $Settings = Get-Content $SettingsFile -Raw | ConvertFrom-Json
            if ($Settings.enabledPlugins) {
                $Settings.enabledPlugins.PSObject.Properties.Remove($PluginKey)
                $Settings | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $SettingsFile
            }
        }
        if (Test-Path $MarketplacesFile) {
            $Marketplaces = Get-Content $MarketplacesFile -Raw | ConvertFrom-Json
            $Marketplaces.PSObject.Properties.Remove("claude-mnemonic")
            $Marketplaces | ConvertTo-Json -Depth 10 | Out-File -Encoding UTF8 $MarketplacesFile
        }
    } catch {
        Write-Warn "Error cleaning up JSON files: $_"
    }

    # Handle data directory
    $DataDir = "$env:USERPROFILE\.claude-mnemonic"
    if (Test-Path $DataDir) {
        if ($KeepData) {
            Write-Warn "Keeping data directory: $DataDir"
        } else {
            Remove-Item -Recurse -Force $DataDir -ErrorAction SilentlyContinue
            Write-Success "Data directory removed"
        }
    }

    Write-Success "Claude Mnemonic uninstalled successfully"
}

# Main
Write-Host ""
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "         Claude Mnemonic - Windows Installation Script          " -ForegroundColor Cyan
Write-Host "       Persistent Memory System for Claude Code                 " -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host ""

if ($Uninstall) {
    Uninstall-ClaudeMnemonic
    exit 0
}

# Get version
if (-not $Version) {
    Write-Info "Fetching latest release..."
    $Version = Get-LatestVersion
}
Write-Info "Installing version: $Version"

# Install
Install-Release -Version $Version
Register-Plugin -Version $Version
Start-Worker

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "                  Installation Complete!                        " -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  Dashboard: http://localhost:37777" -ForegroundColor White
Write-Host ""
Write-Host "  Start a new Claude Code session to activate memory." -ForegroundColor White
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
