# Proxmox NUT Control

Docker web interface for configuring a NUT server on a Proxmox host over SSH. It generates the NUT configuration and a shutdown script that stops Docker containers in the configured order when the battery timer expires or `LOWBATT` is reported, then shuts down the host.

## Getting started

Make sure SSH key authentication to the Proxmox host works, then run:

```sh
mkdir -p data
docker compose up -d --build
```

Open `http://IP-of-the-machine-running-the-app:8080`. Configure the host, UPS and containers, test the connection, then deploy.

## Warnings

- The SSH account must be able to write to `/etc/nut`, `/usr/local/sbin` and restart NUT services; `root` is typically used.
- Without `ssh_known_hosts`, the app automatically accepts the SSH host key on first connection. For production, mount a `known_hosts` file and configure its path.
- A backup is created at `/root/nut-gui-backup-YYYYMMDD-HHMMSS` on Proxmox before deployment.
- Verify actual container names with `docker ps --format '{{.Names}}'`.
- Deployment enables or restarts `nut-server` and `nut-monitor`; test during a maintenance window first.

The configuration assumes that the CyberPower UPS is connected to the Proxmox host over USB and that `usbhid-ups` is installed.

## Preview

![Proxmox UPS GUI](proxmox-ups-gui-screenshot.jpg)
