$ErrorActionPreference = "Stop"

$Repo = "SilkageNet/codex-switch"
$Version = if ($env:CODEX_SWITCH_VERSION) { $env:CODEX_SWITCH_VERSION.TrimStart("v") } else { $null }
$Destination = if ($env:CODEX_SWITCH_INSTALL_DIR) { $env:CODEX_SWITCH_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\codex-switch" }

if (-not $Version) {
    $Release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $Release.tag_name.TrimStart("v")
}

$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "ARM64" { "arm64" }
    "AMD64" { "amd64" }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$Archive = "codex-switch_${Version}_windows_${Arch}.zip"
$Base = "https://github.com/$Repo/releases/download/v$Version"
$Temp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $Temp | Out-Null

try {
    Invoke-WebRequest "$Base/$Archive" -OutFile (Join-Path $Temp $Archive)
    Invoke-WebRequest "$Base/checksums.txt" -OutFile (Join-Path $Temp "checksums.txt")
    $Expected = ((Get-Content (Join-Path $Temp "checksums.txt")) | Where-Object { $_ -match " $([regex]::Escape($Archive))$" }).Split(" ")[0]
    $Actual = (Get-FileHash (Join-Path $Temp $Archive) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Expected -ne $Actual) { throw "Checksum verification failed" }
    Expand-Archive (Join-Path $Temp $Archive) -DestinationPath $Temp -Force
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    Copy-Item (Join-Path $Temp "codex-switch.exe") (Join-Path $Destination "codex-switch.exe") -Force
    Write-Host "Installed codex-switch to $Destination\codex-switch.exe"
} finally {
    Remove-Item -Recurse -Force $Temp -ErrorAction SilentlyContinue
}
