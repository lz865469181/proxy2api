param(
    [string]$Source = "config/config.example.yaml",
    [string]$Target = "config/config.yaml"
)

if (!(Test-Path -LiteralPath $Source)) {
    Write-Error "source config not found: $Source"
    exit 1
}

if (Test-Path -LiteralPath $Target) {
    Write-Output "config already exists: $Target"
    exit 0
}

Copy-Item -LiteralPath $Source -Destination $Target -Force
Write-Output "created $Target from $Source"

