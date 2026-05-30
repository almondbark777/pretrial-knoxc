# remove-my-ip.ps1
# ────────────────────────────────────────────────────────────────────
# Remove all "alex-home-*" firewall rules from the Azure SQL server.
# Use this when you're done working for the day and want to close the
# door behind you.
# ────────────────────────────────────────────────────────────────────

$ErrorActionPreference = "Stop"
$RG     = "myself"
$SERVER = "ptrknoxc"

Write-Host "Listing current alex-home-* rules..." -ForegroundColor Cyan
$existing = az sql server firewall-rule list -g $RG -s $SERVER --query "[?starts_with(name,'alex-home-')].name" -o tsv
if (-not $existing) {
  Write-Host "  (no alex-home rules to remove)" -ForegroundColor DarkGray
  exit 0
}

$existing -split "`n" | ForEach-Object {
  if ($_) {
    Write-Host "  - removing $_"
    az sql server firewall-rule delete -g $RG -s $SERVER -n $_ | Out-Null
  }
}
Write-Host "Done." -ForegroundColor Green
