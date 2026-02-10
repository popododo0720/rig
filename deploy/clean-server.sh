#!/bin/bash
# Ubuntu Server Cleanup Script
# Removes unnecessary services and packages for a lean Rig deployment server
# Run as root on the target server
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

echo "=== Phase 1: Stop unnecessary services ==="
SERVICES_TO_STOP=(
    crypto-hft-monitor
    postgresql
    ModemManager
    multipathd
    wpa_supplicant
    thermald
    udisks2
    open-iscsi iscsid
    open-vm-tools
    polkit
    upower
    gpu-manager
    apport
    unattended-upgrades
    netplan-wpa-wlp1s0
    cloud-init cloud-init-local cloud-config cloud-final
    snapd snapd.socket snapd.apparmor
    networkd-dispatcher
)

for svc in "${SERVICES_TO_STOP[@]}"; do
    systemctl stop "$svc" 2>/dev/null || true
    systemctl disable "$svc" 2>/dev/null || true
done
echo "[OK] Stopped and disabled unnecessary services"

rm -f /etc/systemd/system/crypto-hft-monitor.service

echo "=== Phase 2: Remove packages ==="
apt-get remove --purge -y \
    postgresql* modemmanager snapd cloud-init cloud-guest-utils \
    open-vm-tools open-iscsi multipath-tools \
    thermald udisks2 wpasupplicant \
    upower gpu-manager apport apport-symptoms apport-core-dump-handler \
    unattended-upgrades pollinate \
    byobu landscape-common ubuntu-advantage-tools \
    2>/dev/null || true

echo "=== Phase 3: Autoremove ==="
apt-get autoremove --purge -y 2>/dev/null || true
apt-get clean

echo "=== Phase 4: Remove leftover data ==="
rm -rf /opt/crypto-hft-monitor
rm -rf /var/lib/postgresql
rm -rf /var/lib/snapd /snap
rm -rf /var/lib/cloud /etc/cloud

echo "=== Phase 5: Reload systemd ==="
systemctl daemon-reload

echo ""
echo "=== Result ==="
echo "Running services:"
systemctl list-units --type=service --state=running --no-pager --no-legend | wc -l
echo ""
echo "Listening ports:"
ss -tlnp
echo ""
echo "Disk usage:"
df -h /
echo ""
echo "Memory:"
free -h
echo ""
echo "[DONE] Server cleaned. Ready for Rig deployment."
