$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$repository = "Inzaniak/rts"
$installDir = if ($env:RTS_INSTALL_DIR) {
    $env:RTS_INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA "Programs\rts"
}

switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { $architecture = "amd64" }
    "ARM64" { $architecture = "arm64" }
    default { throw "RTS does not provide a Windows binary for $env:PROCESSOR_ARCHITECTURE." }
}

$release = Invoke-RestMethod `
    -Headers @{ "User-Agent" = "rts-installer" } `
    -Uri "https://api.github.com/repos/$repository/releases/latest"
$tag = $release.tag_name
$version = $tag.TrimStart("v")
$asset = "rts_${version}_windows_${architecture}.zip"
$downloadBase = "https://github.com/$repository/releases/download/$tag"
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("rts-install-" + [guid]::NewGuid())

New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
    $archivePath = Join-Path $tempDir $asset
    $checksumsPath = Join-Path $tempDir "checksums.txt"
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/$asset" -OutFile $archivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/checksums.txt" -OutFile $checksumsPath

    $checksumLine = Get-Content $checksumsPath | Where-Object {
        $_ -match "^([0-9a-fA-F]{64})\s+\*?$([regex]::Escape($asset))$"
    } | Select-Object -First 1
    if (-not $checksumLine) {
        throw "No checksum found for $asset."
    }
    $expected = ($checksumLine -split "\s+")[0]
    $actual = (Get-FileHash -Algorithm SHA256 $archivePath).Hash
    if ($actual -ne $expected) {
        throw "Checksum verification failed for $asset."
    }

    Expand-Archive -LiteralPath $archivePath -DestinationPath $tempDir -Force
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item -LiteralPath (Join-Path $tempDir "rts.exe") -Destination (Join-Path $installDir "rts.exe") -Force
} finally {
    Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host "Installed RTS $tag to $(Join-Path $installDir 'rts.exe')"
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$pathEntries = @($userPath -split ";" | Where-Object { $_ })
if ($pathEntries -notcontains $installDir) {
    $newPath = (@($pathEntries) + $installDir) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Host "Added $installDir to your user PATH. Open a new terminal to run rts."
}
