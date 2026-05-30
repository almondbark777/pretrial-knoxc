# add-my-ip.ps1
# ────────────────────────────────────────────────────────────────────
# Refresh the Azure SQL firewall to allow your CURRENT public IP.
# Run this whenever your IP has changed (VPN, new wifi, etc) and
# the webapp can't reach the database locally.
#
# What it does:
#   1. Looks up your current public IP via api.ipify.org
#   2. Removes any existing "alex-home-*" firewall rules
#   3. Adds a new one for today's IP
#
# Requires: Azure CLI (`az`) installed and logged in (`az login`).
# ────────────────────────────────────────────────────────────────────

$ErrorActionPreference = "Stop"
$RG     = "myself"
$SERVER = "ptrknoxc"

Write-Host "Looking up your current public IP..." -ForegroundColor Cyan
$ip = (Invoke-RestMethod "https://api.ipify.org").Trim()
if (-not $ip) { Write-Error "Could not determine public IP."; exit 1 }
Write-Host "  → Your IP is $ip" -ForegroundColor Green

Write-Host "Removing any old alex-home-* rules..." -ForegroundColor Cyan
$existing = az sql server firewall-rule list -g $RG -s $SERVER --query "[?starts_with(name,'alex-home-')].name" -o tsv
if ($existing) {
  $existing -split "`n" | ForEach-Object {
    if ($_) {
      Write-Host "  - removing $_"
      az sql server firewall-rule delete -g $RG -s $SERVER -n $_ | Out-Null
    }
  }
} else {
  Write-Host "  (none to remove)"
}

$today = Get-Date -Format "yyyy-MM-dd"
$name  = "alex-home-$today"
Write-Host "Adding firewall rule '$name' for $ip..." -ForegroundColor Cyan
az sql server firewall-rule create -g $RG -s $SERVER -n $name `
  --start-ip-address $ip --end-ip-address $ip -o table

Write-Host ""
Write-Host "Done. The webapp should be able to reach the DB now." -ForegroundColor Green
Write-Host "If you want to revoke access later, run remove-my-ip.ps1." -ForegroundColor DarkGray
