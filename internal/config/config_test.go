package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dukk11/printpilot/internal/printer"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValidBambu(t *testing.T) {
	cfg, err := Load(write(t, `{
		"port": 9000,
		"printers": [{"id": "x1c", "type": "bambu", "name": "X1C", "host": "192.168.1.42", "serial": "00M09A123456789", "access_code": "12345678"}]
	}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9000 {
		t.Errorf("port = %d", cfg.Port)
	}
	if cfg.StateFile != DefaultStateFile {
		t.Errorf("state file default not applied: %q", cfg.StateFile)
	}
}

func TestDefaultsAndIDDedup(t *testing.T) {
	cfg, err := Load(write(t, `{
		"printers": [
			{"type": "moonraker", "host": "10.0.0.8"},
			{"type": "moonraker", "host": "10.0.0.9"}
		]
	}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("port default = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.Printers[0].ID != "printer-1" || cfg.Printers[1].ID != "printer-2" {
		t.Errorf("auto ids = %q, %q", cfg.Printers[0].ID, cfg.Printers[1].ID)
	}
	if cfg.Printers[0].Port != DefaultMoonrakerPrt {
		t.Errorf("moonraker default port = %d, want %d", cfg.Printers[0].Port, DefaultMoonrakerPrt)
	}
}

func TestRejectsUnknownType(t *testing.T) {
	_, err := Load(write(t, `{"printers": [{"type": "creality", "host": "x"}]}`))
	if err == nil {
		t.Fatal("unknown printer type must be rejected")
	}
}

func TestRejectsIncompleteBambu(t *testing.T) {
	_, err := Load(write(t, `{"printers": [{"type": "bambu", "host": "x"}]}`))
	if err == nil {
		t.Fatal("bambu without serial/access_code must be rejected")
	}
}

func TestRejectsDuplicateIDs(t *testing.T) {
	_, err := Load(write(t, `{"printers": [
		{"id": "a", "type": "moonraker", "host": "x"},
		{"id": "a", "type": "moonraker", "host": "y"}
	]}`))
	if err == nil {
		t.Fatal("duplicate ids must be rejected")
	}
}

func TestAlertOn(t *testing.T) {
	cfg := &Config{Printers: []Printer{{Type: printer.TypeMoonraker, Host: "h"}}}
	if !cfg.AlertOn("failed") || !cfg.AlertOn("finished") {
		t.Error("empty events must alert on everything")
	}
	cfg.Alerts.Events = []string{"failed"}
	if !cfg.AlertOn("failed") || cfg.AlertOn("finished") {
		t.Error("event filter not applied")
	}
}

func TestRejectsBadEvent(t *testing.T) {
	_, err := Load(write(t, `{"printers": [{"type": "moonraker", "host": "x"}], "alerts": {"events": ["exploded"]}}`))
	if err == nil {
		t.Fatal("unknown alert event must be rejected")
	}
}
