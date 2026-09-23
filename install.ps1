# Installs the latest gignore release on Windows.
#
#   irm https://raw.githubusercontent.com/kwmx/gignore/main/install.ps1 | iex
#
# Set $env:GIGNORE_VERSION (for example "v1.2.0") to install a specific release.
$ErrorActionPreference = 'Stop'
$repo = 'kwmx/gignore'

$version = $env:GIGNORE_VERSION
if (-not $version) {
    $version = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name
}
$number = $version.TrimStart('v')

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$archive = "gignore_${number}_windows_${arch}.zip"
$base = "https://github.com/$repo/releases/download/$version"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("gignore-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host "Downloading gignore $version for windows/$arch"
    Invoke-WebRequest "$base/$archive" -OutFile "$tmp\$archive" -UseBasicParsing
    Invoke-WebRequest "$base/checksums.txt" -OutFile "$tmp\checksums.txt" -UseBasicParsing

    $line = Select-String -Path "$tmp\checksums.txt" -Pattern " $([regex]::Escape($archive))$"
    if (-not $line) { throw "$archive is not listed in checksums.txt" }
    $want = ($line.Line -split ' ')[0]
    $got = (Get-FileHash "$tmp\$archive" -Algorithm SHA256).Hash.ToLower()
    if ($want -ne $got) { throw "Checksum mismatch for $archive" }

    $dir = Join-Path $env:LOCALAPPDATA 'Programs\gignore'
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Expand-Archive "$tmp\$archive" -DestinationPath $tmp -Force
    Copy-Item "$tmp\gignore.exe" "$dir\gignore.exe" -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
        Write-Host "Added $dir to your user PATH. Open a new terminal to use it."
    }
    Write-Host "Installed $(& "$dir\gignore.exe" version)"
}
finally {
    Remove-Item -Recurse -Force $tmp
}
