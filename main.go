package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var version = "dev"

var defaults = map[string]any{
	"ssh_host": "192.168.88.120", "ssh_port": 22, "ssh_user": "root", "ssh_auth_method": "key",
	"ssh_key": "", "ssh_password": "", "ssh_known_hosts": "",
	"stop_timeout": 30,
	"shutdown_command": "shutdown -h now", "containers": []any{},
}

func settingsPath() string {
	if path := os.Getenv("SETTINGS_PATH"); path != "" {
		return path
	}
	return "/data/settings.json"
}

func legacySettingsPath() string {
	if path := os.Getenv("CONFIG_PATH"); path != "" {
		return path
	}
	return "/data/config.json"
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func pruneConfig(config map[string]any) map[string]any {
	allowed := map[string]bool{
		"ssh_host": true, "ssh_port": true, "ssh_user": true, "ssh_auth_method": true,
		"ssh_key": true, "ssh_password": true, "ssh_known_hosts": true,
		"stop_timeout": true, "shutdown_command": true, "containers": true, "ui_settings": true,
	}
	pruned := map[string]any{}
	for key := range allowed {
		if value, ok := config[key]; ok {
			pruned[key] = value
		}
	}
	return pruned
}

func saveConfig(config map[string]any) error {
	config = pruneConfig(config)
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "nut-config-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func environmentConfig() map[string]any {
	mapping := map[string]string{
		"PROXMOX_SSH_HOST": "ssh_host", "PROXMOX_SSH_PORT": "ssh_port", "PROXMOX_SSH_USER": "ssh_user",
		"PROXMOX_SSH_AUTH_METHOD": "ssh_auth_method", "PROXMOX_SSH_KEY": "ssh_key", "PROXMOX_SSH_PASSWORD": "ssh_password",
		"PROXMOX_SSH_KNOWN_HOSTS": "ssh_known_hosts", "STOP_TIMEOUT": "stop_timeout",
		"SHUTDOWN_COMMAND": "shutdown_command",
	}
	config := map[string]any{}
	for envName, key := range mapping {
		if value := os.Getenv(envName); value != "" {
			config[key] = value
			if key == "ssh_port" || key == "stop_timeout" {
				if number, err := strconv.Atoi(value); err == nil {
					config[key] = number
				}
			}
		}
	}
	if raw := os.Getenv("CONTAINERS_JSON"); raw != "" {
		var containers []any
		if json.Unmarshal([]byte(raw), &containers) == nil {
			config["containers"] = containers
		}
	}
	ui := map[string]any{}
	for envName, key := range map[string]string{"UI_LANGUAGE": "language", "UI_THEME_STYLE": "themeStyle", "UI_FONT_SIZE": "fontSize"} {
		if value := os.Getenv(envName); value != "" {
			ui[key] = value
		}
	}
	if len(ui) > 0 {
		config["ui_settings"] = ui
	}
	return config
}

func loadConfig() map[string]any {
	config := cloneMap(defaults)
	path := settingsPath()
	if data, err := os.ReadFile(path); err != nil {
		if legacy, legacyErr := os.ReadFile(legacySettingsPath()); legacyErr == nil {
			_ = json.Unmarshal(legacy, &config)
		}
	} else {
		_ = json.Unmarshal(data, &config)
	}
	if key, ok := config["ssh_key"].(string); ok && strings.HasPrefix(strings.TrimSpace(key), "/") {
		config["ssh_key"] = ""
	}
	env := environmentConfig()
	if ui, ok := env["ui_settings"].(map[string]any); ok {
		merged := map[string]any{}
		if existing, ok := config["ui_settings"].(map[string]any); ok {
			for key, value := range existing {
				merged[key] = value
			}
		}
		for key, value := range ui {
			merged[key] = value
		}
		env["ui_settings"] = merged
	}
	if len(env) > 0 {
		for key, value := range env {
			config[key] = value
		}
		_ = saveConfig(config)
	}
	return pruneConfig(config)
}

func stringValue(config map[string]any, key, fallback string) string {
	if value, ok := config[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func intValue(config map[string]any, key string, fallback int) int {
	switch value := config[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case string:
		if number, err := strconv.Atoi(value); err == nil {
			return number
		}
	}
	return fallback
}

func enabledValue(config map[string]any) bool {
	if value, ok := config["enabled"].(bool); ok {
		return value
	}
	return stringValue(config, "status", "running") != "stopped"
}

func validate(config map[string]any) []string {
	errorsList := []string{}
	for _, key := range []string{"ssh_host", "ssh_user"} {
		if strings.TrimSpace(stringValue(config, key, "")) == "" {
			errorsList = append(errorsList, key+" is required")
		}
	}
	port := intValue(config, "ssh_port", 0)
	if intValue(config, "stop_timeout", 0) < 0 {
		errorsList = append(errorsList, "stop_timeout must be positive")
	}
	if port < 1 || port > 65535 {
		errorsList = append(errorsList, "Invalid SSH port")
	}
	method := stringValue(config, "ssh_auth_method", "key")
	if method != "key" && method != "password" {
		errorsList = append(errorsList, "Invalid SSH authentication method")
	}
	if _, ok := config["containers"].([]any); !ok {
		errorsList = append(errorsList, "Invalid container list")
	}
	return errorsList
}

func sshClient(config map[string]any) (*ssh.Client, error) {
	method := stringValue(config, "ssh_auth_method", "key")
	var auth ssh.AuthMethod
	if method == "password" {
		password := stringValue(config, "ssh_password", "")
		if password == "" {
			return nil, errors.New("An SSH password is required when password authentication is selected")
		}
		auth = ssh.Password(password)
	} else {
		key := stringValue(config, "ssh_key", "")
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("An SSH private key is required when key authentication is selected")
		}
		signer, err := ssh.ParsePrivateKey([]byte(key))
		if err != nil {
			return nil, fmt.Errorf("the SSH private key could not be parsed: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	}
	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if knownHosts := stringValue(config, "ssh_known_hosts", ""); knownHosts != "" {
		callback, err := knownhosts.New(knownHosts)
		if err != nil {
			return nil, err
		}
		hostKeyCallback = callback
	}
	clientConfig := &ssh.ClientConfig{User: stringValue(config, "ssh_user", "root"), Auth: []ssh.AuthMethod{auth}, HostKeyCallback: hostKeyCallback, Timeout: 8 * time.Second}
	address := fmt.Sprintf("%s:%d", stringValue(config, "ssh_host", ""), intValue(config, "ssh_port", 22))
	return ssh.Dial("tcp", address, clientConfig)
}

func runRemote(client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	output, err := session.CombinedOutput(command)
	return strings.TrimSpace(string(output)), err
}

func proxmoxVMs(client *ssh.Client) ([]map[string]any, error) {
	output, err := runRemote(client, "pvesh get /cluster/resources --type vm --output-format json 2>/dev/null")
	if err != nil {
		return nil, fmt.Errorf("could not query Proxmox VMs: %w: %s", err, output)
	}
	var resources []map[string]any
	if err := json.Unmarshal([]byte(output), &resources); err != nil {
		return nil, fmt.Errorf("Proxmox returned invalid VM data: %w", err)
	}
	result := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		if resource["vmid"] == nil || (resource["type"] != "qemu" && resource["type"] != "lxc") {
			continue
		}
		result = append(result, map[string]any{
			"vmid":   resource["vmid"],
			"type":   resource["type"],
			"node":   resource["node"],
			"name":   stringValue(resource, "name", fmt.Sprintf("VM %v", resource["vmid"])),
			"status": stringValue(resource, "status", "unknown"),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return intValue(result[i], "vmid", 0) < intValue(result[j], "vmid", 0)
	})
	return result, nil
}

func nutStatus(client *ssh.Client, config map[string]any) (map[string]any, error) {
	host := stringValue(config, "ssh_host", "")
	if host == "" {
		return nil, errors.New("SSH host is required")
	}
	port := 3493
	command := fmt.Sprintf(`set -eu
ups_name=$(upsc -L %s:%d 2>/dev/null | awk -F: 'NR==1 {print $1; exit}')
if [ -z "$ups_name" ]; then
  exit 1
fi
upsc "${ups_name}@%s:%d" 2>/dev/null
`, host, port, host, port)
	output, err := runRemote(client, command)
	if err != nil {
		return nil, fmt.Errorf("could not query NUT status: %w: %s", err, output)
	}
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, ": "); idx > 0 {
			values[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+2:])
		}
	}
	runtimeSeconds, _ := strconv.Atoi(values["battery.runtime"])
	batteryCharge, _ := strconv.Atoi(values["battery.charge"])
	upsLoad, _ := strconv.Atoi(values["ups.load"])
	status := strings.TrimSpace(values["ups.status"])
	if status == "" {
		status = "unknown"
	}
	return map[string]any{
		"ups_name":               values["device.model"],
		"ups_status":             status,
		"battery_charge":         batteryCharge,
		"battery_runtime":        runtimeSeconds,
		"battery_runtime_min":    float64(runtimeSeconds) / 60,
		"ups_load":               upsLoad,
		"device_model":           values["device.model"],
		"device_vendor":          values["ups.mfr"],
		"status_text":            status,
		"battery_runtime_string": fmt.Sprintf("%.1f", float64(runtimeSeconds)/60),
	}, nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func remoteFiles(config map[string]any) map[string]string {
	timeout := intValue(config, "stop_timeout", 30)
	commands := []string{}
	if containers, ok := config["containers"].([]any); ok {
		for _, raw := range containers {
			if item, ok := raw.(map[string]any); ok {
				if !enabledValue(item) {
					continue
				}
				vmid := intValue(item, "vmid", 0)
				if vmid > 0 {
					stopCommand := "qm"
					if stringValue(item, "type", "qemu") == "lxc" {
						stopCommand = "pct"
					}
					commands = append(commands, fmt.Sprintf("%s shutdown %d --timeout %d || true", stopCommand, vmid, intValue(item, "timeout", timeout)))
					if delay := intValue(item, "delay", 0); delay > 0 {
						commands = append(commands, fmt.Sprintf("sleep %d", delay))
					}
				}
			}
		}
	}
	commands = append(commands, stringValue(config, "shutdown_command", "shutdown -h now"))
	return map[string]string{
		"/usr/local/sbin/nut-proxmox-shutdown": "#!/bin/sh\nset -eu\n" + strings.Join(commands, "\n") + "\n",
		"/usr/local/sbin/nut-upssched-cmd":     "#!/bin/sh\ncase \"$1\" in\n  onbatt|lowbatt|fsd) exec /usr/local/sbin/nut-proxmox-shutdown ;;\n  *) exit 0 ;;\nesac\n",
	}
}

func writeRemote(client *ssh.Client, files map[string]string) (string, error) {
	stamp := time.Now().UTC().Format("20060102-150405")
	backup := "/root/nut-gui-backup-" + stamp
	if _, err := runRemote(client, "mkdir -p "+shellQuote(backup)+" /usr/local/sbin"); err != nil {
		return "", err
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return "", err
	}
	defer sftpClient.Close()
	preserveIfExists := map[string]bool{
		"/etc/nut/ups.conf":    true,
		"/etc/nut/upsd.users":  true,
		"/etc/nut/upsmon.conf": true,
	}
	for path, content := range files {
		if preserveIfExists[path] {
			if _, err := sftpClient.Stat(path); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return "", err
			}
		}
		if err := sftpClient.MkdirAll(filepath.Dir(path)); err != nil {
			return "", err
		}
		_, _ = runRemote(client, "test ! -e "+shellQuote(path)+" || cp -a "+shellQuote(path)+" "+shellQuote(backup)+"/")
		file, err := sftpClient.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			return "", err
		}
		_, err = io.WriteString(file, content)
		closeErr := file.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		if strings.HasPrefix(path, "/usr/local/sbin/") {
			if _, err := runRemote(client, "chmod 755 "+shellQuote(path)); err != nil {
				return "", err
			}
		}
	}
	return backup, nil
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func decodeBody(r *http.Request) map[string]any {
	var value map[string]any
	_ = json.NewDecoder(r.Body).Decode(&value)
	return value
}
func mergeConfig(value map[string]any) map[string]any {
	config := loadConfig()
	for key, item := range value {
		config[key] = item
	}
	return config
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/config":
		if r.Method == http.MethodGet {
			jsonResponse(w, http.StatusOK, loadConfig())
			return
		}
		if r.Method == http.MethodPost {
			config := mergeConfig(decodeBody(r))
			if errorsList := validate(config); len(errorsList) > 0 {
				jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "errors": errorsList})
				return
			}
			if err := saveConfig(config); err != nil {
				jsonResponse(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "config": config})
			return
		}
	case "/api/meta":
		if r.Method == http.MethodGet {
			jsonResponse(w, http.StatusOK, map[string]any{"version": version})
			return
		}
	case "/api/settings.json":
		if r.Method == http.MethodGet {
			settings := loadConfig()["ui_settings"]
			if settings == nil {
				settings = map[string]any{}
			}
			jsonResponse(w, 200, settings)
			return
		}
	case "/api/settings":
		if r.Method == http.MethodPost {
			config := loadConfig()
			current, _ := config["ui_settings"].(map[string]any)
			if current == nil {
				current = map[string]any{}
			}
			for key, value := range decodeBody(r) {
				current[key] = value
			}
			config["ui_settings"] = current
			if err := saveConfig(config); err != nil {
				jsonResponse(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "settings": current})
			return
		}
	case "/api/test-connection":
		if r.Method == http.MethodPost {
			config := mergeConfig(decodeBody(r))
			client, err := sshClient(config)
			if err != nil {
				jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			defer client.Close()
			output, err := runRemote(client, "hostname && command -v pvesh && command -v qm && command -v pct")
			if err != nil {
				jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error() + ": " + output})
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "output": output})
			return
		}
	case "/api/proxmox/vms":
		if r.Method == http.MethodGet {
			config := loadConfig()
			client, err := sshClient(config)
			if err != nil {
				jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			defer client.Close()
			vms, err := proxmoxVMs(client)
			if err != nil {
				jsonResponse(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "vms": vms})
			return
		}
	case "/api/ups/status":
		if r.Method == http.MethodGet {
			config := loadConfig()
			client, err := sshClient(config)
			if err != nil {
				jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			defer client.Close()
			status, err := nutStatus(client, config)
			if err != nil {
				jsonResponse(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "status": status})
			return
		}
	case "/api/apply":
		if r.Method == http.MethodPost {
			config := mergeConfig(decodeBody(r))
			if errorsList := validate(config); len(errorsList) > 0 {
				jsonResponse(w, 400, map[string]any{"ok": false, "errors": errorsList})
				return
			}
			client, err := sshClient(config)
			if err != nil {
				jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			defer client.Close()
			backup, err := writeRemote(client, remoteFiles(config))
			if err != nil {
				jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if err := saveConfig(config); err != nil {
				jsonResponse(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "backup": backup})
			return
		}
	}
	http.NotFound(w, r)
}

func main() {
	static := http.FileServer(http.Dir("static"))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", apiHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			static.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, "templates/index.html")
	})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Proxmox UPS GUI %s listening on :%s", version, port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
