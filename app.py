import json
import io
import os
import shlex
import socket
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from flask import Flask, jsonify, render_template, request
import paramiko

app = Flask(__name__, static_folder="static", static_url_path="")
CONFIG_PATH = Path(os.getenv("SETTINGS_PATH", "/data/settings.json"))
LEGACY_CONFIG_PATH = Path(os.getenv("CONFIG_PATH", "/data/config.json"))
DEFAULTS = {
    "ssh_host": "192.168.88.120", "ssh_port": 22, "ssh_user": "root", "ssh_auth_method": "key",
    "ssh_key": "", "ssh_password": "", "ssh_known_hosts": "",
    "ups_host": "127.0.0.1", "ups_port": 3493, "nut_user": "nutmon", "nut_password": "change-this",
    "ups_name": "cyberpower", "ups_driver": "usbhid-ups",
    "ups_description": "CyberPower UPS", "upsmon_password": "change-this", "battery_minutes": 5,
    "low_battery_minutes": 2, "stop_timeout": 30,
    "shutdown_command": "shutdown -h now", "containers": [],
}

def save_config(value):
    CONFIG_PATH.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix="nut-config-", dir=CONFIG_PATH.parent)
    try:
        with os.fdopen(fd, "w") as handle:
            json.dump(value, handle, indent=2)
            handle.write("\n")
        os.replace(name, CONFIG_PATH)
    finally:
        if os.path.exists(name): os.unlink(name)

def environment_config():
    mapping = {
        "PROXMOX_SSH_HOST": "ssh_host", "PROXMOX_SSH_PORT": "ssh_port", "PROXMOX_SSH_USER": "ssh_user",
        "PROXMOX_SSH_AUTH_METHOD": "ssh_auth_method", "PROXMOX_SSH_KEY": "ssh_key", "PROXMOX_SSH_PASSWORD": "ssh_password",
        "PROXMOX_SSH_KNOWN_HOSTS": "ssh_known_hosts", "UPS_HOST": "ups_host", "UPS_PORT": "ups_port",
        "UPS_NAME": "ups_name", "UPS_DRIVER": "ups_driver", "UPS_DESCRIPTION": "ups_description",
        "NUT_USER": "nut_user", "NUT_PASSWORD": "nut_password", "UPSMON_PASSWORD": "upsmon_password",
        "BATTERY_MINUTES": "battery_minutes", "LOW_BATTERY_MINUTES": "low_battery_minutes", "STOP_TIMEOUT": "stop_timeout",
        "SHUTDOWN_COMMAND": "shutdown_command",
    }
    values = {key: os.environ[env_name] for env_name, key in mapping.items() if os.getenv(env_name, "") != ""}
    for key in ("ssh_port", "ups_port", "battery_minutes", "low_battery_minutes", "stop_timeout"):
        if key in values:
            try: values[key] = int(values[key])
            except ValueError: pass
    if os.getenv("CONTAINERS_JSON", ""):
        try: values["containers"] = json.loads(os.environ["CONTAINERS_JSON"])
        except ValueError: pass
    ui = {key: os.environ[name] for name, key in (("UI_LANGUAGE", "language"), ("UI_THEME_STYLE", "themeStyle"), ("UI_FONT_SIZE", "fontSize")) if os.getenv(name, "")}
    if ui: values["ui_settings"] = ui
    return values

def load_config():
    source = CONFIG_PATH if CONFIG_PATH.exists() else LEGACY_CONFIG_PATH
    config = DEFAULTS.copy()
    if source.exists():
        try: config.update(json.loads(source.read_text()))
        except (OSError, ValueError): pass
    env_values = environment_config()
    if env_values:
        if "ui_settings" in env_values:
            env_values["ui_settings"] = {**config.get("ui_settings", {}), **env_values["ui_settings"]}
        config.update(env_values)
        save_config(config)
    return config

def validate(value):
    errors = []
    for key in ("ssh_host", "ssh_user", "ups_name", "ups_driver"):
        if not str(value.get(key, "")).strip(): errors.append(f"{key} is required")
    for key in ("battery_minutes", "low_battery_minutes", "stop_timeout"):
        try:
            if int(value.get(key, 0)) < 0: errors.append(f"{key} must be positive")
        except (TypeError, ValueError): errors.append(f"{key} must be a number")
    try:
        if int(value.get("low_battery_minutes", 0)) > int(value.get("battery_minutes", 0)):
            errors.append("The critical battery threshold cannot exceed the main timer")
        if not 1 <= int(value.get("ssh_port", 0)) <= 65535: errors.append("Invalid SSH port")
        if not 1 <= int(value.get("ups_port", 0)) <= 65535: errors.append("Invalid UPS/NUT port")
    except (TypeError, ValueError): errors.append("Invalid SSH or UPS/NUT port")
    if not isinstance(value.get("containers"), list): errors.append("Invalid container list")
    if value.get("ssh_auth_method", "key") not in {"key", "password"}: errors.append("Invalid SSH authentication method")
    return errors

def connect(cfg):
    client = paramiko.SSHClient()
    known_hosts = cfg.get("ssh_known_hosts", "").strip()
    if known_hosts:
        client.load_host_keys(known_hosts)
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
    else:
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    auth_method = cfg.get("ssh_auth_method", "key")
    password = cfg.get("ssh_password") or None if auth_method == "password" else None
    key_text = str(cfg.get("ssh_key") or "").strip()
    pkey = None
    if auth_method == "key" and key_text:
        key_errors = []
        for key_type in (paramiko.Ed25519Key, paramiko.RSAKey, paramiko.ECDSAKey, paramiko.DSSKey):
            try:
                pkey = key_type.from_private_key(io.StringIO(key_text))
                break
            except (paramiko.SSHException, ValueError) as exc:
                key_errors.append(str(exc))
        if pkey is None:
            raise paramiko.SSHException("The SSH private key could not be parsed")
    if auth_method == "key" and pkey is None:
        raise paramiko.SSHException("An SSH private key is required when key authentication is selected")
    if auth_method == "password" and not password:
        raise paramiko.SSHException("An SSH password is required when password authentication is selected")
    client.connect(cfg["ssh_host"], port=int(cfg["ssh_port"]), username=cfg["ssh_user"],
                   password=password, pkey=pkey,
                   timeout=8, banner_timeout=8, look_for_keys=False, allow_agent=False)
    return client

def remote_files(cfg):
    name, minutes, low, timeout = cfg["ups_name"], int(cfg["battery_minutes"]), int(cfg["low_battery_minutes"]), int(cfg["stop_timeout"])
    ups_host, ups_port = cfg.get("ups_host", "127.0.0.1"), int(cfg.get("ups_port", 3493))
    monitor_target = f"{name}@{ups_host}:{ups_port}"
    driver_port = "auto" if cfg["ups_driver"] == "usbhid-ups" else ups_host
    active = [item for item in cfg["containers"] if str(item.get("name", "")).strip()]
    commands = []
    for item in active:
        commands.append(f"docker stop -t {int(item.get('timeout', timeout))} {shlex.quote(str(item['name']))} || true")
        if int(item.get("delay", 0)) > 0: commands.append(f"sleep {int(item['delay'])}")
    commands.append(cfg.get("shutdown_command", "shutdown -h now"))
    return {
        "/etc/nut/ups.conf": f"[{name}]\n  driver = {cfg['ups_driver']}\n  port = {driver_port}\n  desc = {cfg['ups_description']}\n",
        "/etc/nut/upsd.users": f'''[{cfg.get("nut_user", "nutmon")}]
  password = {cfg.get("nut_password", "change-this")}
  upsmon master
''',
        "/etc/nut/upsmon.conf": f'''MONITOR {monitor_target} 1 {cfg.get("nut_user", "nutmon")} {cfg.get("nut_password", "change-this")} master
MINSUPPLIES 1
SHUTDOWNCMD "/usr/local/sbin/nut-docker-shutdown"
POWERDOWNFLAG /etc/killpower
POLLFREQ 5
POLLFREQALERT 5
HOSTSYNC 15
DEADTIME 15
FINALDELAY 5
NOTIFYCMD /usr/sbin/upssched
NOTIFYFLAG ONLINE SYSLOG+EXEC
NOTIFYFLAG ONBATT SYSLOG+EXEC
NOTIFYFLAG LOWBATT SYSLOG+EXEC
NOTIFYFLAG FSD SYSLOG+EXEC
''',
        "/etc/nut/upssched.conf": f'''CMDSCRIPT /usr/local/sbin/nut-upssched-cmd
PIPEFN /run/nut/upssched.pipe
LOCKFN /run/nut/upssched.lock
AT ONBATT * START-TIMER onbatt {minutes * 60}
AT ONLINE * CANCEL-TIMER onbatt
AT LOWBATT * EXECUTE lowbatt
AT FSD * EXECUTE fsd
''',
        "/usr/local/sbin/nut-docker-shutdown": "#!/bin/sh\nset -eu\n" + "\n".join(commands) + "\n",
        "/usr/local/sbin/nut-upssched-cmd": "#!/bin/sh\ncase \"$1\" in\n  onbatt|lowbatt|fsd) exec /usr/local/sbin/nut-docker-shutdown ;;\n  *) exit 0 ;;\nesac\n",
    }

def write_remote(client, files):
    stamp = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    backup = f"/root/nut-gui-backup-{stamp}"
    client.exec_command(f"mkdir -p {shlex.quote(backup)} /usr/local/sbin")
    sftp = client.open_sftp()
    try:
        for path, content in files.items():
            client.exec_command(f"mkdir -p {shlex.quote(str(Path(path).parent))}")
            client.exec_command(f"test ! -e {shlex.quote(path)} || cp -a {shlex.quote(path)} {shlex.quote(backup)}/")
            with sftp.open(path, "w") as stream: stream.write(content)
            if path.startswith("/usr/local/sbin/"): client.exec_command(f"chmod 755 {shlex.quote(path)}")
        client.exec_command("systemctl enable --now nut-server nut-monitor 2>/dev/null || systemctl restart nut-server nut-monitor")
    finally: sftp.close()
    return backup

@app.get("/")
def index(): return render_template("index.html", config=load_config())

@app.get("/api/config")
def get_config(): return jsonify(load_config())

@app.get("/api/settings.json")
def get_settings():
    return jsonify(load_config().get("ui_settings", {}))

@app.post("/api/settings")
def put_settings():
    value = request.get_json(silent=True) or {}
    cfg = load_config()
    cfg["ui_settings"] = {**cfg.get("ui_settings", {}), **value}
    save_config(cfg)
    return jsonify({"ok": True, "settings": cfg["ui_settings"]})

@app.post("/api/config")
def put_config():
    value = {**load_config(), **(request.get_json(silent=True) or {})}
    errors = validate(value)
    if errors: return jsonify({"ok": False, "errors": errors}), 400
    save_config(value)
    return jsonify({"ok": True, "config": value})

@app.post("/api/test-connection")
def test_connection():
    cfg = {**load_config(), **(request.get_json(silent=True) or {})}
    try:
        client = connect(cfg)
        _, stdout, _ = client.exec_command("hostname && command -v docker || true && systemctl is-active nut-server || true")
        output = stdout.read().decode().strip(); client.close()
        return jsonify({"ok": True, "output": output})
    except (paramiko.SSHException, socket.error, OSError) as exc:
        return jsonify({"ok": False, "error": str(exc)}), 400

@app.post("/api/apply")
def apply_config():
    cfg = {**load_config(), **(request.get_json(silent=True) or {})}
    errors = validate(cfg)
    if errors: return jsonify({"ok": False, "errors": errors}), 400
    try:
        client = connect(cfg); backup = write_remote(client, remote_files(cfg)); client.close(); save_config(cfg)
        return jsonify({"ok": True, "backup": backup})
    except (paramiko.SSHException, socket.error, OSError, ValueError) as exc:
        return jsonify({"ok": False, "error": str(exc)}), 400

if __name__ == "__main__": app.run(host="0.0.0.0", port=8080)
