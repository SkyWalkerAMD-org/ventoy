$ErrorActionPreference = 'Stop'
{{if .CA}}Import-Certificate -FilePath (Join-Path $PSScriptRoot 'ca.crt') -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
{{end}}$headers = @{ Authorization = {{ps (printf "Bearer %s" .Token)}} }
# This records the installed Windows image during specialize, not fyhocsys enrollment.
Invoke-RestMethod -Uri {{ps (printf "%s/event" .Base)}} -Method Post -Headers $headers -ContentType 'application/json' -Body '{"state":"installed"}' -TimeoutSec 60 | Out-Null
Remove-Item -LiteralPath $PSCommandPath -Force
