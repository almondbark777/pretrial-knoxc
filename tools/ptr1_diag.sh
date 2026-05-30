# ─── PTR1 SERVER DIAGNOSTIC ───────────────────────────────────────────────
# Run this on ptr1 and paste the output back.
# Command:  bash <(cat <<'DIAG'   ... paste all of this ... DIAG)
# Or save as diag.sh and run:  bash diag.sh

echo "===== 1. SYSTEM ====="
uname -a
uptime
echo ""

echo "===== 2. DISK ====="
df -h /
echo ""

echo "===== 3. MEMORY ====="
free -h
echo ""

echo "===== 4. SERVICES ====="
systemctl is-active ptr-webapp && echo "ptr-webapp: RUNNING" || echo "ptr-webapp: NOT RUNNING"
systemctl is-active cloudflared && echo "cloudflared: RUNNING" || echo "cloudflared: NOT RUNNING"
systemctl is-active ptr-import.timer && echo "ptr-import.timer: RUNNING" || echo "ptr-import.timer: NOT RUNNING"
echo ""

echo "===== 5. APP HEALTH ====="
curl -s http://127.0.0.1:8000/health
echo ""
echo ""

echo "===== 6. DATABASE ====="
find /opt/ptr-knoxc -name "*.db" 2>/dev/null
DB=$(find /opt/ptr-knoxc -name "*.db" 2>/dev/null | head -1)
if [ -n "$DB" ]; then
  echo "DB path: $DB"
  ls -lh "$DB"
  sqlite3 "$DB" "PRAGMA integrity_check;" 2>/dev/null || echo "(sqlite3 not installed)"
  sqlite3 "$DB" "SELECT name, COUNT(*) FROM sqlite_master WHERE type='table' GROUP BY name;" 2>/dev/null | head -20
fi
echo ""

echo "===== 7. PYTHON / GO VERSIONS ====="
python3 --version 2>/dev/null || echo "python3: not found"
go version 2>/dev/null || echo "go: not found"
echo ""

echo "===== 8. APP FILES ====="
ls -lh /opt/ptr-knoxc/webapp/ 2>/dev/null
echo ""

echo "===== 9. RECENT APP LOGS (last 30 lines) ====="
journalctl -u ptr-webapp -n 30 --no-pager 2>/dev/null
echo ""

echo "===== 10. RECENT IMPORT LOGS (last 20 lines) ====="
journalctl -u ptr-import.service -n 20 --no-pager 2>/dev/null
echo ""

echo "===== 11. NEXT IMPORT TIMER RUN ====="
systemctl list-timers ptr-import.timer --no-pager 2>/dev/null
echo ""

echo "===== 12. CLOUDFLARED STATUS ====="
journalctl -u cloudflared -n 10 --no-pager 2>/dev/null
echo ""

echo "===== DONE ====="
