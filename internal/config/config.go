// Package config loads and validates the PrintPilot JSON configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Dukk11/printpilot/internal/printer"
)

const (
	DefaultPort         = 8787
	DefaultMoonrakerPrt = 7125
	DefaultStateFile    = "state.json"
)

// Printer describes a single configured printer.
type Printer struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port,omitempty"`
	Serial     string `json:"serial,omitempty"`      // bambu only
	AccessCode string `json:"access_code,omitempty"` // bambu only
	WebcamURL  string `json:"webcam_url,omitempty"`
}

// Telegram alert settings.
type Telegram struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// Webhook alert settings.
type Webhook struct {
	URL string `json:"url"`
}

// Alerts configures outgoing notifications.
type Alerts struct {
	Telegram Telegram `json:"telegram"`
	Webhook  Webhook  `json:"webhook"`
	// Events restricts which transitions alert. Valid: "failed", "finished".
	// Empty means both.
	Events []string `json:"events"`
}

// Config is the top-level PrintPilot configuration.
type Config struct {
	Port      int       `json:"port"`
	StateFile string    `json:"state_file,omitempty"`
	Printers  []Printer `json:"printers"`
	Alerts    Alerts    `json:"alerts"`
}

// Load reads path, applies defaults and validates the result.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ApplyDefaults fills unset fields with sensible values.
func (c *Config) ApplyDefaults() {
	if c.Port <= 0 {
		c.Port = DefaultPort
	}
	if c.StateFile == "" {
		c.StateFile = DefaultStateFile
	}
	for i := range c.Printers {
		p := &c.Printers[i]
		if p.ID == "" {
			p.ID = fmt.Sprintf("printer-%d", i+1)
		}
		if p.Name == "" {
			p.Name = p.ID
		}
		if p.Type == printer.TypeMoonraker && p.Port <= 0 {
			p.Port = DefaultMoonrakerPrt
		}
	}
}

// Validate checks required fields and normalizes the alert event list.
func (c *Config) Validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d", c.Port)
	}
	if len(c.Printers) == 0 {
		return fmt.Errorf("no printers configured: add at least one entry to \"printers\"")
	}
	seen := map[string]bool{}
	for i, p := range c.Printers {
		label := fmt.Sprintf("printers[%d]", i)
		if p.ID != "" {
			if seen[p.ID] {
				return fmt.Errorf("%s: duplicate id %q", label, p.ID)
			}
			seen[p.ID] = true
		}
		switch p.Type {
		case printer.TypeBambu:
			if p.Host == "" || p.Serial == "" || p.AccessCode == "" {
				return fmt.Errorf("%s: bambu printers need \"host\", \"serial\" and \"access_code\" (the LAN access code shown on the printer screen)", label)
			}
		case printer.TypeMoonraker:
			if p.Host == "" {
				return fmt.Errorf("%s: moonraker printers need \"host\"", label)
			}
		default:
			return fmt.Errorf("%s: unknown type %q (supported: %q, %q)", label, p.Type, printer.TypeBambu, printer.TypeMoonraker)
		}
	}
	for _, e := range c.Alerts.Events {
		if e != "failed" && e != "finished" {
			return fmt.Errorf("alerts.events: unknown event %q (supported: \"failed\", \"finished\")", e)
		}
	}
	return nil
}

// AlertOn reports whether transitions into the given event should alert.
func (c *Config) AlertOn(event string) bool {
	if len(c.Alerts.Events) == 0 {
		return true
	}
	for _, e := range c.Alerts.Events {
		if strings.EqualFold(e, event) {
			return true
		}
	}
	return false
}
