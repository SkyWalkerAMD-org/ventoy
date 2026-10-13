$ErrorActionPreference = 'Stop'
$expectedMac = {{ps .Machine.MAC}}
$expectedDisk = {{.Machine.WindowsDisk}}
$expectedSerial = {{ps .Machine.DiskSerial}}
$macs = @(Get-CimInstance Win32_NetworkAdapter | Where-Object MACAddress | ForEach-Object { $_.MACAddress.ToLowerInvariant() })
if ($macs -notcontains $expectedMac) { throw 'MAC mismatch; installation stopped' }
$disks = @(Get-Disk | Where-Object { $_.SerialNumber.Trim() -eq $expectedSerial })
if ($disks.Count -ne 1 -or $disks[0].Number -ne $expectedDisk -or $disks[0].BusType -eq 'USB') { throw 'Target disk identity mismatch; installation stopped' }
wpeutil UpdateBootInfo
if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect firmware mode' }
$firmware = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control').PEFirmwareType
if ($firmware -ne {{if eq .Machine.Firmware "uefi"}}2{{else}}1{{end}}) { throw 'Firmware mode mismatch' }
if (Get-Volume -DriveLetter W -ErrorAction SilentlyContinue) { throw 'Temporary drive W is in use' }
{{if .CA}}Import-Certificate -FilePath X:\ventoy-center\ca.crt -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
{{end}}$headers = @{ Authorization = {{ps (printf "Bearer %s" .Token)}} }
Invoke-RestMethod -Uri {{ps (printf "%s/event" .Base)}} -Method Post -Headers $headers -ContentType 'application/json' -Body '{"state":"installing"}' -TimeoutSec 30 | Out-Null
# The claim above is single-use. A canceled, expired or already-started job stops here.
$partitionScript = @'
select disk {{.Machine.WindowsDisk}}
clean
{{if eq .Machine.Firmware "uefi"}}convert gpt
create partition efi size=260
format quick fs=fat32 label=System
create partition msr size=16
{{else}}convert mbr
create partition primary size=550
format quick fs=ntfs label=System
active
{{end}}create partition primary
format quick fs=ntfs label=Windows
assign letter=W
'@
$partitionFile = Join-Path $env:TEMP 'ventoy-center-diskpart.txt'
[IO.File]::WriteAllText($partitionFile, $partitionScript, [Text.Encoding]::ASCII)
diskpart.exe /s $partitionFile
if ($LASTEXITCODE -ne 0 -or !(Test-Path W:\)) { throw 'Disk preparation failed' }
$target = 'W:\ProgramData\VentoyCenter'
New-Item -ItemType Directory -Path $target -Force | Out-Null
Copy-Item -LiteralPath X:\ventoy-center\postinstall.ps1 -Destination $target
{{if .CA}}Copy-Item -LiteralPath X:\ventoy-center\ca.crt -Destination $target
{{end}}
