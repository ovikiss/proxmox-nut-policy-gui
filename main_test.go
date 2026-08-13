package main

import (
	"strings"
	"testing"
)

func TestRemoteFilesIncludeNutShutdownLifecycle(t *testing.T) {
	files := remoteFiles(map[string]any{
		"ssh_host":         "10.0.0.5",
		"stop_timeout":    180,
		"shutdown_command": "shutdown -h now",
		"containers": []any{
			map[string]any{"vmid": 100, "type": "qemu", "enabled": true, "timeout": 120, "delay": 5},
			map[string]any{"vmid": 200, "type": "lxc", "enabled": true, "timeout": 90},
		},
	})
	shutdown := files["/usr/local/sbin/nut-proxmox-shutdown"]
	handler := files["/usr/local/sbin/nut-upssched-cmd"]
	for _, expected := range []string{
		"/var/log/nut-proxmox-shutdown.log",
		"/var/lib/nut-proxmox-running.state",
		"GRACE_PERIOD=180",
		"capture_guest qm 100",
		"capture_guest pct 200",
		"--timeout \"$timeout\" --forceStop 1",
		"restore_state",
		"NUT_HOST='10.0.0.5'",
		"shutdown -h now",
	} {
		if !strings.Contains(shutdown, expected) {
			t.Fatalf("shutdown script does not contain %q", expected)
		}
	}
	for _, expected := range []string{
		"onbatt)",
		"lowbatt|fsd)",
		"nut-proxmox-shutdown onbatt",
		"nut-proxmox-shutdown immediate",
	} {
		if !strings.Contains(handler, expected) {
			t.Fatalf("NUT handler does not contain %q", expected)
		}
	}
	upssched := files["/etc/nut/upssched.conf"]
	for _, expected := range []string{
		"CMDSCRIPT /usr/local/sbin/nut-upssched-cmd",
		"AT ONBATT * EXECUTE onbatt",
		"AT ONLINE * EXECUTE online",
		"AT LOWBATT * EXECUTE lowbatt",
		"AT FSD * EXECUTE fsd",
	} {
		if !strings.Contains(upssched, expected) {
			t.Fatalf("upssched config does not contain %q", expected)
		}
	}
}
