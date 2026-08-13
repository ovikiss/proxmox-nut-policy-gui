#!/bin/sh
set -u

LOG_FILE=/var/log/nut-proxmox-shutdown.log
LOCK_DIR=/run/nut-proxmox-shutdown.lock

log_message() {
  mkdir -p "$(dirname "$LOG_FILE")"
  printf '[%s] %s\n' "$(date -Is)" "$1" >>"$LOG_FILE"
  logger -t nut-proxmox-shutdown -- "$1" 2>/dev/null || true
}

case "${1:-}" in
  online)
    log_message "NUT ONLINE received"
    ;;
  onbatt)
    log_message "NUT ONBATT received"
    nohup /usr/local/sbin/nut-proxmox-shutdown onbatt >/dev/null 2>&1 &
    ;;
  lowbatt|fsd)
    log_message "NUT $1 received"
    if [ -d "$LOCK_DIR" ]; then
      : >"$LOCK_DIR/force"
    else
      nohup /usr/local/sbin/nut-proxmox-shutdown immediate >/dev/null 2>&1 &
    fi
    ;;
  *)
    log_message "Ignoring unknown NUT event: $1"
    ;;
esac
