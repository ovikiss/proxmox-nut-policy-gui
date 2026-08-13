# Proxmox/NUT integration

This project installs its own `nut-proxmox-policy.service` daemon. It polls
the UPS named by `/etc/nut/upsmon.conf`, detects state transitions, and sends
them to `nut-upssched-cmd`. The GUI deploys the generated
`/usr/local/sbin/nut-proxmox-shutdown` policy script. NUT's `upsmon` and
`upssched` integration remains enabled for native event delivery and FSD.

Run `install-nut-integration.sh` as root on Proxmox to install the daemon,
systemd unit, event handler and `upssched.conf`. It creates a backup under `/root` first and does
not modify NUT users, passwords, UPS definitions, or models.

The generated shutdown script receives the UPS host from the GUI SSH
settings, so no site-specific IP address is stored in this repository.
