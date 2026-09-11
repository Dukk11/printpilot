// Package alerts fires Telegram/webhook notifications on printer state
// transitions (job failed, job finished, printer went offline) with
// per-job de-duplication so a restart or repeated reports never re-alert.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Dukk11/printpilot/internal/config"
	"github.com/Dukk11/printpilot/internal/printer"
)

// Notifier delivers one alert to a channel.
type Notifier interface {
	Name() string
	Send(ctx context.Context, msg Message) error
}

// Message is a rendered alert.
type Message struct {
	Event    string // "failed" | "finished" | "offline"
	Printer  string
	Job      string
	Progress float64
	Detail   string
}

// String renders the plain-text notification body.
func (m Message) String() string {
	head := map[string]string{
		"failed":   "Job FAILED",
		"finished": "Job finished",
		"offline":  "Printer offline",
	}[m.Event]
	if head == "" {
		head = m.Event
	}
	s := fmt.Sprintf("🖨 PrintPilot — %s\nPrinter: %s", head, m.Printer)
	if m.Job != "" {
		s += fmt.Sprintf("\nJob: %s (%.0f%%)", m.Job, m.Progress)
	}
	if m.Detail != "" {
		s += "\n" + m.Detail
	}
	return s
}

// Manager observes status transitions and dispatches to notifiers.
type Manager struct {
	mu        sync.Mutex
	notifiers []Notifier
	events    map[string]bool // allowed events
	last      map[string]string
	lastPath  string
	client    *http.Client
}

// New builds a manager from config. lastPath persists the dedup keys so a
// restart does not re-fire alerts for a job that already alerted.
func New(cfg *config.Config, lastPath string) *Manager {
	m := &Manager{
		events:   map[string]bool{"failed": true, "finished": true, "offline": true},
		last:     map[string]string{},
		lastPath: lastPath,
		client:   &http.Client{Timeout: 8 * time.Second},
	}
	if cfg != nil {
		if cfg.Alerts.Telegram.BotToken != "" && cfg.Alerts.Telegram.ChatID != "" {
			m.notifiers = append(m.notifiers, &Telegram{Token: cfg.Alerts.Telegram.BotToken, ChatID: cfg.Alerts.Telegram.ChatID, Client: m.client})
		}
		if cfg.Alerts.Webhook.URL != "" {
			m.notifiers = append(m.notifiers, &Webhook{URL: cfg.Alerts.Webhook.URL, Client: m.client})
		}
		for _, e := range cfg.Alerts.Events {
			m.events[e] = true
		}
		// Explicit event list replaces the default set for job alerts.
		if len(cfg.Alerts.Events) > 0 {
			m.events = map[string]bool{}
			for _, e := range cfg.Alerts.Events {
				m.events[e] = true
			}
			m.events["offline"] = true // offline is always signalled if notifiers exist
		}
	}
	m.loadLast()
	return m
}

// Enabled reports whether any notifier is configured.
func (m *Manager) Enabled() bool { return len(m.notifiers) > 0 }

// Observe inspects a status transition and fires alerts. Safe for
// concurrent use; delivery happens asynchronously with a timeout.
func (m *Manager) Observe(prev printer.Status, hadPrev bool, cur printer.Status) {
	if len(m.notifiers) == 0 {
		return
	}
	event := ""
	switch {
	case !hadPrev:
		// First observation after start: never alert (avoid restart storms).
		return
	case prev.State == cur.State:
		return
	case cur.State == printer.StateFailed:
		event = "failed"
	case cur.State == printer.StateFinished:
		event = "finished"
	case cur.State == printer.StateOffline:
		event = "offline"
	default:
		return
	}

	key := cur.JobName + "|" + string(cur.State)
	m.mu.Lock()
	if m.last[cur.ID] == key {
		m.mu.Unlock()
		return
	}
	m.last[cur.ID] = key
	err := m.saveLastLocked()
	m.mu.Unlock()
	if err != nil {
		log.Printf("alerts: persist dedup state: %v", err)
	}

	if !m.events[event] {
		return
	}
	msg := Message{
		Event:    event,
		Printer:  cur.Name,
		Job:      cur.JobName,
		Progress: cur.Progress,
		Detail:   cur.Detail,
	}
	for _, n := range m.notifiers {
		n := n
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if err := n.Send(ctx, msg); err != nil {
				log.Printf("alerts: %s notification for %s: %v", n.Name(), cur.Name, err)
			}
		}()
	}
}

func (m *Manager) loadLast() {
	if m.lastPath == "" {
		return
	}
	b, err := os.ReadFile(m.lastPath)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &m.last)
}

func (m *Manager) saveLastLocked() error {
	if m.lastPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.lastPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m.last, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.lastPath, b, 0o600)
}

// Telegram sends alerts through the Telegram Bot API.
type Telegram struct {
	Token  string
	ChatID string
	Client *http.Client
}

// Name implements Notifier.
func (t *Telegram) Name() string { return "telegram" }

// Send implements Notifier.
func (t *Telegram) Send(ctx context.Context, msg Message) error {
	body, _ := json.Marshal(map[string]string{
		"chat_id": t.ChatID,
		"text":    msg.String(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+t.Token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned %s", resp.Status)
	}
	return nil
}

// Webhook posts a JSON payload to a user-supplied endpoint.
type Webhook struct {
	URL    string
	Client *http.Client
}

// Name implements Notifier.
func (w *Webhook) Name() string { return "webhook" }

// Send implements Notifier.
func (w *Webhook) Send(ctx context.Context, msg Message) error {
	body, _ := json.Marshal(map[string]any{
		"event":     msg.Event,
		"printer":   msg.Printer,
		"job":       msg.Job,
		"progress":  msg.Progress,
		"detail":    msg.Detail,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PrintPilot-Event", msg.Event)
	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook endpoint returned %s", resp.Status)
	}
	return nil
}
