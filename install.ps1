# RDR2 Living NPC Agent - Windows bootstrap.
#
# Finds a suitable Python, runs the cross-platform installer, and optionally
# copies the built bridge into your Red Dead Redemption 2 folder.
#
#   .\install.ps1                     guided install
#   .\install.ps1 -Check              diagnose only, change nothing
#   .\install.ps1 -WithAsi            download SDK + build the .asi
#   .\install.ps1 -WithAsi -Deploy    ...and copy it into the game folder
#   .\install.ps1 -ApiKey sk-...      save a key to .env
#
# If PowerShell blocks the script, run it like this instead:
#   powershell -ExecutionPolicy Bypass -File .\install.ps1

[CmdletBinding()]
param(
    [switch]$Check,
    [switch]$WithAsi,
    [switch]$WithAdk,
    [switch]$WithAudio,
    [switch]$Probe,
    [switch]$FullSmoke,
    [switch]$Deploy,
    [string]$ApiKey = "",
    [string]$GameDir = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ProjectRoot

function Write-Head($text) {
    Write-Host ""
    Write-Host "=== $text ===" -ForegroundColor Cyan
}

function Find-Python {
    # Prefer the py launcher, then whatever is on PATH.
    foreach ($candidate in @("py", "python", "python3")) {
        $command = Get-Command $candidate -ErrorAction SilentlyContinue
        if (-not $command) { continue }
        try {
            if ($candidate -eq "py") {
                $version = & py -3 -c "import sys; print('%d.%d' % sys.version_info[:2])" 2>$null
            } else {
                $version = & $candidate -c "import sys; print('%d.%d' % sys.version_info[:2])" 2>$null
            }
        } catch {
            continue
        }
        if (-not $version) { continue }

        # Tolerate stray warnings on stdout: keep the first line that looks like X.Y.
        $text = ($version | Where-Object { $_ -match '^\s*\d+\.\d+\s*$' } | Select-Object -First 1)
        if (-not $text) { continue }
        $text = $text.Trim()
        $major = [int]($text.Split(".")[0])
        $minor = [int]($text.Split(".")[1])
        if ($major -gt 3 -or ($major -eq 3 -and $minor -ge 10)) {
            if ($candidate -eq "py") { return @{ Exe = "py"; Args = @("-3") } }
            return @{ Exe = $command.Source; Args = @() }
        }
    }
    return $null
}

Write-Host ""
Write-Host "RDR2 Living NPC Agent - Windows setup" -ForegroundColor White
Write-Host "project: $ProjectRoot"

Write-Head "Locating Python"
$python = Find-Python
if (-not $python) {
    Write-Host "No Python 3.10+ found." -ForegroundColor Red
    Write-Host ""
    Write-Host "Install it with one of these, then re-run this script:"
    Write-Host "  winget install -e --id Python.Python.3.12"
    Write-Host "  https://www.python.org/downloads/windows/"
    Write-Host ""
    Write-Host "During setup, tick 'Add python.exe to PATH'." -ForegroundColor Yellow
    exit 1
}
Write-Host "Using: $($python.Exe) $($python.Args -join ' ')" -ForegroundColor Green

$installerArgs = @("install.py", "--yes")
if ($Check) { $installerArgs += "--check" }
if ($WithAsi) { $installerArgs += @("--with-sdk", "--with-asi") }
if ($WithAdk) { $installerArgs += @("--with-adk", "--venv") }
if ($WithAudio) { $installerArgs += @("--with-audio", "--venv") }
if ($Probe) { $installerArgs += "--probe" }
if ($FullSmoke) { $installerArgs += "--full-smoke" }
if ($ApiKey) { $installerArgs += @("--api-key", $ApiKey) }

Write-Head "Running installer"
# Splat through a variable: @array is only a splat when it prefixes a name.
$allArgs = $python.Args + $installerArgs
& $python.Exe @allArgs
$installerExit = $LASTEXITCODE
if ($installerExit -ne 0) {
    Write-Host ""
    Write-Host "Installer reported blocking problems (exit $installerExit)." -ForegroundColor Red
    exit $installerExit
}

# ---------------------------------------------------------------------------
# Optional: deploy the bridge into the game folder
# ---------------------------------------------------------------------------
$asi = Join-Path $ProjectRoot "bridge\build\asi\Release\rdr2_ai_bridge.asi"
if (-not (Test-Path $asi)) {
    $found = Get-ChildItem -Path (Join-Path $ProjectRoot "bridge\build") -Filter "rdr2_ai_bridge.asi" `
        -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found) { $asi = $found.FullName }
}

if ($Deploy -and -not $Check) {
    Write-Head "Deploying the bridge"
    if (-not (Test-Path $asi)) {
        Write-Host "No rdr2_ai_bridge.asi found - build it first with -WithAsi." -ForegroundColor Red
        exit 1
    }

    $candidates = @()
    if ($GameDir) { $candidates += $GameDir }
    $candidates += @(
        "C:\Program Files (x86)\Steam\steamapps\common\Red Dead Redemption 2",
        "C:\Program Files\Steam\steamapps\common\Red Dead Redemption 2",
        "C:\Program Files\Rockstar Games\Red Dead Redemption 2",
        "C:\Program Files\Epic Games\RedDeadRedemption2"
    )

    $target = $null
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path (Join-Path $candidate "RDR2.exe"))) {
            $target = $candidate
            break
        }
    }
    if (-not $target) {
        Write-Host "Could not find RDR2.exe automatically." -ForegroundColor Yellow
        Write-Host "Re-run with:  .\install.ps1 -Deploy -GameDir 'D:\Games\Red Dead Redemption 2'"
        exit 0
    }

    Copy-Item $asi -Destination $target -Force
    Write-Host "Copied $(Split-Path -Leaf $asi) -> $target" -ForegroundColor Green

    $hook = Join-Path $target "ScriptHookRDR2.dll"
    if (-not (Test-Path $hook)) {
        Write-Host ""
        Write-Host "ScriptHookRDR2.dll is still missing from the game folder." -ForegroundColor Yellow
        Write-Host "Download it from http://www.dev-c.com/rdr2/scripthookrdr2/ and copy it next to RDR2.exe."
        Write-Host "The .asi cannot load without it."
    }
}

Write-Head "Done"
Write-Host "Next: start the runtime, then launch the game."
Write-Host "  $($python.Exe) -m runtime.main --backend llm"
Write-Host ""
