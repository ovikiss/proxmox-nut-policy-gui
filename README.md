# Proxmox NUT Control

Interfață web Docker pentru configurarea unui NUT server pe un host Proxmox prin SSH. Generează configurația NUT și un script care, la expirarea timerului de baterie sau la `LOWBATT`, oprește containerele Docker în ordinea afișată, apoi oprește hostul.

## Pornire

Asigură-te că autentificarea SSH cu cheie către hostul Proxmox funcționează, apoi rulează:

```sh
mkdir -p data
docker compose up -d --build
```

Deschide `http://IP-ul-mașinii-care-rulează-aplicația:8080`. Completează hostul, UPS-ul și containerele, testează conexiunea, apoi aplică.

## Atenționări

- Contul SSH trebuie să poată scrie în `/etc/nut`, `/usr/local/sbin` și să repornească serviciile NUT; în mod uzual este `root`.
- Fără `ssh_known_hosts`, aplicația acceptă automat cheia hostului SSH la prima conectare. Pentru producție, montează un `known_hosts` și completează calea.
- Înainte de aplicare se creează backup în `/root/nut-gui-backup-YYYYMMDD-HHMMSS` pe Proxmox.
- Verifică numele reale ale containerelor cu `docker ps --format '{{.Names}}'`.
- Aplicarea activează sau repornește `nut-server` și `nut-monitor`; testează întâi într-o fereastră de mentenanță.

Configurația presupune că UPS-ul CyberPower este conectat USB la hostul Proxmox și că `usbhid-ups` este instalat.

## Preview

![Proxmox UPS GUI](proxmox-ups-gui-screenshot.jpg)
