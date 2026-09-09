# GPM installer for Windows - downloads the latest release binary.
#
#   irm https://raw.githubusercontent.com/tonmoydeb404/gpm/main/install.ps1 | iex
#
# Optional install location:
#   & ([scriptblock]::Create((irm <url>))) -InstallDir D:\tools\gpm

param(
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\gpm"
)

$ErrorActionPreference = "Stop"
$Repo = "tonmoydeb404/gpm"

function Die($msg) {
    Write-Error $msg
    exit 1
}

# --- detect architecture ----------------------------------------------------
switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { $arch = "amd64" }
    "ARM64" { $arch = "arm64" }
    default { Die "unsupported architecture: $env:PROCESSOR_ARCHITECTURE (gpm ships for amd64 and arm64)" }
}

$url = "https://github.com/$Repo/releases/latest/download/gpm_windows_$arch.zip"
$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ([guid]::NewGuid().ToString()))
try {
    # --- fetch --------------------------------------------------------------
    Write-Host "Downloading gpm (windows/$arch)..."
    $zip = Join-Path $tmp "gpm.zip"
    if (Get-Command curl.exe -ErrorAction SilentlyContinue) {
        curl.exe -fsSL -o $zip $url
        if ($LASTEXITCODE -ne 0) { Die "download failed - is there a published release? https://github.com/$Repo/releases" }
    }
    else {
        try { Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing }
        catch { Die "download failed - is there a published release? https://github.com/$Repo/releases" }
    }

    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    $exe = Join-Path $tmp "gpm.exe"
    if (-not (Test-Path $exe)) { Die "archive did not contain a gpm.exe binary" }

    # --- install ------------------------------------------------------------
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Move-Item -Path $exe -Destination (Join-Path $InstallDir "gpm.exe") -Force
    Write-Host "Installed: $(Join-Path $InstallDir 'gpm.exe')"

    # --- PATH ----------------------------------------------------------------
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($userPath -split ";") -notcontains $InstallDir) {
        [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
        Write-Host "Added $InstallDir to your user PATH - restart your terminal for it to take effect."
    }
    Write-Host "Run 'gpm --help' to get started."
}
finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
