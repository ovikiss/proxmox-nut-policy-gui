#!/bin/sh
set -eu

INTERVAL=${NUT_POLICY_INTERVAL:-5}
LOG_FILE=/var/log/nut-proxmox-shutdown.log
previous=

log_message() {
  mkdir -p "$(dirname "$LOG_FILE")"
  printf '[%s] %s\n' "$(date -Is)" "$1" >>"$LOG_FILE"
  logger -t nut-proxmox-policy -- "$1" 2>/dev/null || true
}

monitor_target() {
  awk '$1 == "MONITOR" {split($2, a, "@"); split(a[2], b, ":"); print a[1] "@" b[1]; exit}' /etc/nut/upsmon.conf
}

while :; do
  target=$(monitor_target 2>/dev/null || true)
  if [ -n "$target" ]; then
    status=$(upsc "$target" ups.status 2>/dev/null || printf 'UNKNOWN')
    case "$status" in
      *OL*) current=online ;;
      *OB*) current=onbatt ;;
      *LB*|*FSD*) current=force ;;
      *) current=unknown ;;
    esac
    if [ "$current" != "$previous" ]; then
      log_message "Policy daemon detected UPS state: $current"
      case "$current" in
        online) /usr/local/sbin/nut-upssched-cmd online ;;
        onbatt) /usr/local/sbin/nut-upssched-cmd onbatt ;;
        force) /usr/local/sbin/nut-upssched-cmd fsd ;;
      esac
      previous=$current
    fi
  fi
  sleep "$INTERVAL"
done
