// Package moonraker polls Klipper printers via their Moonraker HTTP API.
package moonraker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Dukk11/printpilot/internal/printer"
)

const (
	// DefaultPoll is the polling interval for status queries.
	DefaultPoll = 2 * time.Second
	// offlineAfter is the number of consecutive failed polls before a
	// printer is reported offline.
	offlineAfter = 3
)

// queryResp mirrors the Moonraker /printer/objects/query response subset.
type queryResp struct {
	Result struct {
		Status struct {
			PrintStats struct {
				State    string  `json:"state"`
				Progress float64 `json:"progress"` // 0-1
				Filename string  `json:"filename"`
				Message  string  `json:"message"`
			} `json:"print_stats"`
			Extruder struct {
				Temperature float64 `json:"temperature"`
			} `json:"extruder"`
			HeaterBed struct {
				Temperature float64 `json:"temperature"`
			} `json:"heater_bed"`
			DisplayStatus struct {
				Progress float64 `json:"progress"` // 0-1
			} `json:"display_status"`
		} `json:"status"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"result"`
}

// Connector polls one Moonraker endpoint and pushes normalized statuses.
type Connector struct {
	id, name, host string
	port           int
	webcamURL      string
	poll           time.Duration
	client         *http.Client
	onUpdate       func(printer.Status)
}

// New creates a connector; call Start to begin polling.
func New(id, name, host string, port int, webcamURL string, onUpdate func(printer.Status)) *Connector {
	return &Connector{
		id: id, name: name, host: host, port: port, webcamURL: webcamURL,
		poll:     DefaultPoll,
		client:   &http.Client{Timeout: 5 * time.Second},
		onUpdate: onUpdate,
	}
}

// Start runs the polling loop until ctx is cancelled.
func (c *Connector) Start(ctx context.Context) {
	go c.loop(ctx)
}

func (c *Connector) loop(ctx context.Context) {
	failures := 0
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()
	c.pollOnce(&failures) // immediate first sample
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollOnce(&failures)
		}
	}
}

func (c *Connector) pollOnce(failures *int) {
	st, err := c.fetch()
	if err != nil {
		*failures++
		if *failures == offlineAfter {
			c.onUpdate(printer.Status{
				ID: c.id, Type: printer.TypeMoonraker, Name: c.name,
				State: printer.StateOffline, WebcamURL: c.webcamURL,
				Detail: "polling failed: " + err.Error(),
			})
		}
		return
	}
	*failures = 0
	c.onUpdate(st)
}

func (c *Connector) fetch() (printer.Status, error) {
	q := url.Values{}
	q.Set("print_stats", "state,progress,filename,message")
	q.Set("extruder", "temperature")
	q.Set("heater_bed", "temperature")
	q.Set("display_status", "progress")
	endpoint := fmt.Sprintf("http://%s:%d/printer/objects/query?%s", c.host, c.port, q.Encode())

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return printer.Status{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return printer.Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return printer.Status{}, fmt.Errorf("moonraker returned %s", resp.Status)
	}
	var qr queryResp
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return printer.Status{}, fmt.Errorf("decode moonraker response: %w", err)
	}
	if qr.Result.Error != nil && qr.Result.Error.Message != "" {
		return printer.Status{}, fmt.Errorf("moonraker error: %s", qr.Result.Error.Message)
	}
	return mapQuery(qr, c.id, c.name, c.webcamURL), nil
}

// mapQuery normalizes a parsed Moonraker response. Exported for tests.
func mapQuery(qr queryResp, id, name, webcamURL string) printer.Status {
	ps := qr.Result.Status.PrintStats
	st := printer.Status{
		ID: id, Type: printer.TypeMoonraker, Name: name,
		State:      MapState(ps.State),
		Progress:   clampPct(ps.Progress * 100),
		JobName:    strings.TrimPrefix(ps.Filename, "/"),
		NozzleTemp: qr.Result.Status.Extruder.Temperature,
		BedTemp:    qr.Result.Status.HeaterBed.Temperature,
		WebcamURL:  webcamURL,
	}
	if ps.Message != "" && st.State == printer.StateFailed {
		st.Detail = ps.Message
	}
	return st
}

// MapState translates Moonraker print_stats.state values to printer.State.
func MapState(s string) printer.State {
	switch s {
	case "printing":
		return printer.StateRunning
	case "paused":
		return printer.StatePaused
	case "complete":
		return printer.StateFinished
	case "error":
		return printer.StateFailed
	case "cancelled":
		return printer.StateIdle
	default: // standby and anything unknown
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
