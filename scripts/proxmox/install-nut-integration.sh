#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BACKUP_DIR=/root/nut-gui-backup-$(date +%Y%m%d-%H%M%S)
mkdir -p "$BACKUP_DIR"

for file in /etc/nut/upssched.conf /usr/local/sbin/nut-upssched-cmd; do
  if [ -e "$file" ]; then cp -a "$file" "$BACKUP_DIR/"; fi
done

install -o root -g root -m 0755 "$SCRIPT_DIR/nut-upssched-cmd.sh" /usr/local/sbin/nut-upssched-cmd
install -o root -g root -m 0755 "$SCRIPT_DIR/nut-proxmox-policy-daemon.sh" /usr/local/sbin/nut-proxmox-policy-daemon
install -o root -g root -m 0644 "$SCRIPT_DIR/upssched.conf" /etc/nut/upssched.conf
install -o root -g root -m 0644 "$SCRIPT_DIR/nut-proxmox-policy.service" /etc/systemd/system/nut-proxmox-policy.service

test -f /etc/nut/upsmon.conf
grep -q '^NOTIFYCMD[[:space:]]\+/usr/sbin/upssched' /etc/nut/upsmon.conf
grep -q '^SHUTDOWNCMD[[:space:]]\+/usr/local/sbin/nut-proxmox-shutdown' /etc/nut/upsmon.conf
sh -n /usr/local/sbin/nut-upssched-cmd
sh -n /usr/local/sbin/nut-proxmox-policy-daemon

systemctl daemon-reload
systemctl enable --now nut-proxmox-policy.service
systemctl restart nut-monitor
systemctl is-active --quiet nut-monitor
echo "NUT integration installed. Backup: $BACKUP_DIR"
