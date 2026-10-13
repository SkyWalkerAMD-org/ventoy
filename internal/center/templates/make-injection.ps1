param([Parameter(Mandatory=$true)][string]$SevenZip)
$ErrorActionPreference = 'Stop'
$source = Join-Path $PSScriptRoot 'injection'
$output = Join-Path $PSScriptRoot 'injection.7z'
if (Test-Path -LiteralPath $output) { throw 'injection.7z already exists; retained' }
if (!(Test-Path -LiteralPath $SevenZip -PathType Leaf)) { throw 'Provide the full path to 7z.exe' }
Push-Location -LiteralPath $source
try {
    & $SevenZip a -t7z $output '.\ventoy-center'
    if ($LASTEXITCODE -ne 0) { throw '7-Zip failed' }
} finally { Pop-Location }
Write-Output $output
