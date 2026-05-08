$ErrorActionPreference = "Stop"

$BaseUrl = if ($env:CETUS_BASE_URL) { $env:CETUS_BASE_URL } else { "https://cetus.cenvero.org" }
$env:CETUS_CHANNEL = "beta"

Invoke-Expression (Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/install.ps1").Content
