$ErrorActionPreference = "Stop"

$BaseUrl = if ($env:CETUS_BASE_URL) { $env:CETUS_BASE_URL } else { "https://cetus.cenvero.org" }
$Channel = if ($env:CETUS_CHANNEL) { $env:CETUS_CHANNEL.ToLowerInvariant() } else { "stable" }
$InstallDir = if ($env:CETUS_INSTALL_DIR) { $env:CETUS_INSTALL_DIR } else { Join-Path $HOME "bin" }
$Tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $Tmp | Out-Null

try {
  if ($Channel -notin @("stable", "beta", "rc")) { throw "unsupported channel: $Channel" }

  $ManifestUrl = "$BaseUrl/manifest.json"
  $ManifestPath = Join-Path $Tmp "manifest.json"
  Invoke-WebRequest -UseBasicParsing -Uri $ManifestUrl -OutFile $ManifestPath
  $Manifest = Get-Content $ManifestPath -Raw | ConvertFrom-Json
  $ChannelInfo = $Manifest.channels.$Channel
  $Version = $ChannelInfo.version
  if (-not $Version) { throw "no $Channel Cetus release is published yet" }

  $Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "X64") { "amd64" } else { throw "unsupported architecture" }
  $Platform = "windows-$Arch"
  $Binary = $Manifest.binaries.$Version.$Platform
  if (-not $Binary) { throw "manifest does not contain a binary for $Platform" }

  $Archive = Join-Path $Tmp "cetus.zip"
  Invoke-WebRequest -UseBasicParsing -Uri $Binary.url -OutFile $Archive
  $Actual = (Get-FileHash -Algorithm SHA256 $Archive).Hash.ToLowerInvariant()
  if ($Actual -ne $Binary.sha256.ToLowerInvariant()) { throw "checksum mismatch" }

  Expand-Archive -Force -Path $Archive -DestinationPath $Tmp
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Copy-Item -Force (Join-Path $Tmp "cetus.exe") (Join-Path $InstallDir "cetus.exe")
  Write-Host "installed cetus $Version ($Channel) to $InstallDir\cetus.exe"
} finally {
  Remove-Item -Recurse -Force $Tmp
}
