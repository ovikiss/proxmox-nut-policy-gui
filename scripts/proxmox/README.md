# Proxmox/NUT integration

The NUT daemon already provided by Proxmox is `nut-monitor.service`; it runs
`upsmon`. This project does not add a second shutdown daemon. `upsmon` sends
events to `upssched`, which invokes `nut-upssched-cmd`, and the GUI deploys the
generated `/usr/local/sbin/nut-proxmox-shutdown` policy script.

Run `install-nut-integration.sh` as root on Proxmox to install the event
handler and `upssched.conf`. It creates a backup under `/root` first and does
not modify NUT users, passwords, UPS definitions, or models.

The generated shutdown script receives the UPS host from the GUI SSH
settings, so no site-specific IP address is stored in this repository.
