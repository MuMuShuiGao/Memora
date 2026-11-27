# Pull Memora database snapshots from the server (ADR-0026).
#
# Run it by hand (double-click the wrapper that calls this script). There is no
# scheduled task -- off-machine copies move only when someone runs this. Use key
# auth only, so nothing prompts for a passphrase.
#
# The server keeps the rolling 5 daily snapshots (MEMORA_BACKUP_KEEP); this
# script keeps the newest 5 copies on the PC.
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [string]$User = "root",
    [string]$KeyPath = "$env:USERPROFILE\.ssh\id_ed25519",
    [string]$RemoteDir = "/var/lib/memora/backups",
    [Parameter(Mandatory = $true)][string]$Dest,
    [int]$Keep = 5
)

$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path $Dest | Out-Null

scp -i $KeyPath -q "${User}@${Server}:${RemoteDir}/memora-*.db" $Dest
if ($LASTEXITCODE -ne 0) {
    throw "scp failed with exit code $LASTEXITCODE"
}

$snapshots = @(Get-ChildItem -Path $Dest -Filter "memora-*.db" | Sort-Object Name)
if ($snapshots.Count -gt $Keep) {
    $snapshots | Select-Object -First ($snapshots.Count - $Keep) | Remove-Item -Force
}
Write-Output ("pulled {0} snapshot(s), kept {1}" -f $snapshots.Count, [Math]::Min($snapshots.Count, $Keep))
