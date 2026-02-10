#!/bin/bash
# Rig Server Setup Script
# Usage: scp this + rig binary + rig.yaml to server, then run as root
set -euo pipefail

echo "=== Rig Server Setup ==="

# 1. Create rig user (no login shell)
if ! id -u rig &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin rig
    echo "[OK] Created system user 'rig'"
else
    echo "[SKIP] User 'rig' already exists"
fi

# 2. Create directories
mkdir -p /opt/rig
echo "[OK] Created /opt/rig"

# 3. Install binary
if [ -f ./rig-linux-amd64 ]; then
    cp ./rig-linux-amd64 /opt/rig/rig
    chmod 755 /opt/rig/rig
    echo "[OK] Installed rig binary"
else
    echo "[ERROR] rig-linux-amd64 not found in current directory"
    exit 1
fi

# 4. Install config (if provided)
if [ -f ./rig.yaml ]; then
    cp ./rig.yaml /opt/rig/rig.yaml
    chmod 640 /opt/rig/rig.yaml
    chown root:rig /opt/rig/rig.yaml
    echo "[OK] Installed rig.yaml"
else
    echo "[WARN] No rig.yaml found — you'll need to create /opt/rig/rig.yaml"
fi

# 5. Set ownership
chown -R rig:rig /opt/rig
echo "[OK] Set ownership"

# 6. Install systemd services
cp ./rig-web.service /etc/systemd/system/
cp ./rig-webhook.service /etc/systemd/system/
systemctl daemon-reload
echo "[OK] Installed systemd services"

# 7. Enable and start services
systemctl enable rig-web rig-webhook
systemctl start rig-web
systemctl start rig-webhook
echo "[OK] Started rig-web and rig-webhook"

# 8. Status check
echo ""
echo "=== Status ==="
systemctl status rig-web --no-pager -l || true
echo "---"
systemctl status rig-webhook --no-pager -l || true
echo ""
echo "=== Ports ==="
ss -tlnp | grep -E '(3000|9000)' || echo "(no rig ports yet — may need a few seconds)"
echo ""
echo "=== Done ==="
echo "Web dashboard: http://$(hostname -I | awk '{print $1}'):3000"
echo "Webhook server: http://$(hostname -I | awk '{print $1}'):9000"
