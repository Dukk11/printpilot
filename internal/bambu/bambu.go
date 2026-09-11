// Package bambu connects to Bambu Lab printers over their LAN MQTT API
// (Developer Mode / LAN Only). No vendor cloud is contacted.
package bambu

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/Dukk11/printpilot/internal/printer"
)

// flexnum accepts both JSON numbers and numeric strings — Bambu firmware
// versions are inconsistent here (e.g. mc_remaining_time arrives as string).
type flexnum float64

func (f *flexnum) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" || s == `""` {
		*f = 0
		return nil
	}
	if len(s) > 0 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		v, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return fmt.Errorf("flexnum: %w", err)
		}
		*f = flexnum(v)
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("flexnum: %w", err)
	}
	*f = flexnum(v)
	return nil
}

// report mirrors the subset of the Bambu `device/{serial}/report` payload
// that PrintPilot consumes (see OpenBambuAPI).
type report struct {
	Print struct {
		GcodeState  string  `json:"gcode_state"`
		MCPercent   flexnum `json:"mc_percent"`
		MCRemaining flexnum `json:"mc_remaining_time"` // minutes
		GcodeFile   string  `json:"gcode_file"`
		LayerNum    flexnum `json:"layer_num"`
		TotalLayers flexnum `json:"total_layers"`
		NozzleTemp  flexnum `json:"nozzle_temper"`
		BedTemp     flexnum `json:"bed_temper"`
	} `json:"print"`
}

// PinStore implements trust-on-first-use certificate pinning for the
// printers' self-signed TLS certificates. The first seen certificate is
// stored by printer ID; any later change aborts the handshake, which
// protects against man-in-the-middle attacks on the LAN.
type PinStore struct {
	mu   sync.Mutex
	path string
	pins map[string]string // printer ID -> "sha256:<hex>"
}

// NewPinStore loads pins from path (an empty pin store is fine).
func NewPinStore(path string) (*PinStore, error) {
	p := &PinStore{path: path, pins: map[string]string{}}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &p.pins); err != nil {
			return nil, fmt.Errorf("parse pin store %s: %w", path, err)
		}
	}
	return p, nil
}

// VerifyConnection returns a tls.Config VerifyConnection callback that pins
// the leaf certificate of the printer with the given ID.
func (p *PinStore) VerifyConnection(printerID string) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("printer presented no TLS certificate")
		}
		sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
		fp := "sha256:" + hex.EncodeToString(sum[:])
		p.mu.Lock()
		defer p.mu.Unlock()
		if known, ok := p.pins[printerID]; ok {
			if known != fp {
				return fmt.Errorf("TLS certificate of printer %q changed since first use "+
					"(possible man-in-the-middle attack): pinned %s, presented %s. "+
					"If you knowingly replaced the printer or reset it, delete the pin from your pins file.",
					printerID, known, fp)
			}
			return nil
		}
		p.pins[printerID] = fp
		return p.saveLocked()
	}
}

func (p *PinStore) saveLocked() error {
	if p.path == "" {
		return nil // in-memory store (e.g. demo mode)
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p.pins, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, b, 0o600)
}

// Connector subscribes to one printer's LAN MQTT report topic and pushes
// normalized statuses into onUpdate.
type Connector struct {
	cfg      connConfig
	pins     *PinStore
	onUpdate func(printer.Status)
	client   mqtt.Client
}

// connConfig carries the connection settings, kept separate from the global
// config package so the connector is independently testable.
type connConfig struct {
	ID         string
	Name       string
	Host       string
	Serial     string
	AccessCode string
	WebcamURL  string
}

// New creates a connector; call Start to connect. pins may be nil, in which
// case default TLS verification applies (which fails on self-signed certs).
func New(id, name, host, serial, accessCode, webcamURL string, pins *PinStore, onUpdate func(printer.Status)) *Connector {
	return &Connector{
		cfg:      connConfig{ID: id, Name: name, Host: host, Serial: serial, AccessCode: accessCode, WebcamURL: webcamURL},
		pins:     pins,
		onUpdate: onUpdate,
	}
}

// Start connects and subscribes. Reconnects happen automatically in the
// background; a failed first connect is reported but does not abort.
func (c *Connector) Start() error {
	broker := fmt.Sprintf("ssl://%s:8883", c.cfg.Host)
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.pins != nil {
		// VerifyConnection replaces (not disables) standard verification:
		// the printer's self-signed certificate is pinned on first use.
		tlsCfg.VerifyConnection = c.pins.VerifyConnection(c.cfg.ID)
	}
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetUsername("bblp").
		SetPassword(c.cfg.AccessCode).
		SetTLSConfig(tlsCfg).
		SetClientID("printpilot-" + c.cfg.ID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetOrderMatters(false).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			c.push(offlineStatus(c.cfg, "connection lost: "+err.Error()))
		}).
		SetOnConnectHandler(func(_ mqtt.Client) {
			// Optimistic idle until the first report arrives (usually < 1s).
			c.push(connectedStatus(c.cfg))
		})

	c.client = mqtt.NewClient(opts)
	topic := fmt.Sprintf("device/%s/report", c.cfg.Serial)
	if tok := c.client.Subscribe(topic, 1, c.onMessage); tok.WaitTimeout(15*time.Second) && tok.Error() != nil {
		return fmt.Errorf("subscribe %s: %w", topic, tok.Error())
	}
	return nil
}

// Close unsubscribes and disconnects from the printer.
func (c *Connector) Close() {
	if c.client != nil {
		c.client.Unsubscribe(fmt.Sprintf("device/%s/report", c.cfg.Serial))
		c.client.Disconnect(250)
	}
}

func (c *Connector) onMessage(_ mqtt.Client, msg mqtt.Message) {
	st, err := ParseReport(msg.Payload(), c.cfg.ID, c.cfg.Name, c.cfg.WebcamURL)
	if err != nil {
		return // malformed/unknown payload: ignore, the next report follows
	}
	c.push(st)
}

func (c *Connector) push(st printer.Status) {
	if c.onUpdate != nil {
		c.onUpdate(st)
	}
}

func connectedStatus(cfg connConfig) printer.Status {
	return printer.Status{ID: cfg.ID, Type: printer.TypeBambu, Name: cfg.Name,
		State: printer.StateIdle, Detail: "connected", WebcamURL: cfg.WebcamURL}
}

func offlineStatus(cfg connConfig, detail string) printer.Status {
	return printer.Status{ID: cfg.ID, Type: printer.TypeBambu, Name: cfg.Name,
		State: printer.StateOffline, Detail: detail, WebcamURL: cfg.WebcamURL}
}

// ParseReport maps a raw Bambu report payload onto a normalized Status.
// Exported for tests.
func ParseReport(payload []byte, id, name, webcamURL string) (printer.Status, error) {
	var r report
	if err := json.Unmarshal(payload, &r); err != nil {
		return printer.Status{}, fmt.Errorf("parse bambu report: %w", err)
	}
	p := r.Print
	st := printer.Status{
		ID:          id,
		Type:        printer.TypeBambu,
		Name:        name,
		State:       MapGcodeState(p.GcodeState),
		Progress:    clampPct(float64(p.MCPercent)),
		JobName:     baseName(p.GcodeFile),
		Layer:       int(p.LayerNum),
		TotalLayers: int(p.TotalLayers),
		NozzleTemp:  float64(p.NozzleTemp),
		BedTemp:     float64(p.BedTemp),
		WebcamURL:   webcamURL,
	}
	st.RemainingMin = int(p.MCRemaining)
	return st, nil
}

// MapGcodeState translates Bambu gcode_state values to printer.State.
func MapGcodeState(s string) printer.State {
	switch s {
	case "RUNNING", "SLICING", "PREPARE":
		return printer.StateRunning
	case "PAUSE":
		return printer.StatePaused
	case "FINISH":
		return printer.StateFinished
	case "FAILED":
		return printer.StateFailed
	default:
		return printer.StateIdle
	}
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func baseName(p string) string {
	if p == "" {
		return ""
	}
	return path.Base(p)
}
