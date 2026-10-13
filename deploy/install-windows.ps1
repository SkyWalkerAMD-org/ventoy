param(
    [Parameter(Mandatory=$true)][string]$Binary,
    [string]$Destination = 'C:\ProgramData\VentoyCenter'
)
$ErrorActionPreference = 'Stop'
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (!$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run in an administrator PowerShell.' }
if (!(Test-Path -LiteralPath $Binary -PathType Leaf)) { throw 'Binary not found.' }
if (Get-ScheduledTask -TaskName 'VentoyCenter' -ErrorAction SilentlyContinue) { throw 'Existing scheduled task retained. Stop it and update the executable explicitly.' }
if (Test-Path -LiteralPath $Destination) { throw 'Destination already exists. Existing configuration is retained.' }
New-Item -ItemType Directory -Path $Destination | Out-Null
& icacls.exe $Destination '/inheritance:r' '/grant:r' '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Cannot restrict directory permissions.' }
$exe = Join-Path $Destination 'ventoy-center.exe'
Copy-Item -LiteralPath $Binary -Destination $exe
$runtime = Join-Path $Destination 'runtime'
& $exe init --dir $runtime
if ($LASTEXITCODE -ne 0) { throw 'Initialization failed.' }
$config = Join-Path $runtime 'config.json'
$action = New-ScheduledTaskAction -Execute $exe -Argument ('serve --config "' + $config + '"')
$trigger = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName 'VentoyCenter' -Action $action -Trigger $trigger -Settings $settings -User 'SYSTEM' -RunLevel Highest | Out-Null
Disable-ScheduledTask -TaskName 'VentoyCenter' | Out-Null
Write-Output ('Installed. Configure TLS and addresses in ' + $config)
Write-Output 'Then use Enable-ScheduledTask and Start-ScheduledTask for VentoyCenter.'
