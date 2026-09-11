// Package printer defines the normalized printer model that all
// connectors (Bambu Lab, Moonraker/Klipper, mock) are mapped onto.
package printer

import "time"

// Connector types (Status.Type values).
const (
	TypeBambu     = "bambu"
	TypeMoonraker = "moonraker"
	TypeMock      = "mock"
)

// State is the normalized lifecycle state of a printer or its current job.
type State string

const (
	StateIdle     State = "idle"
	StateRunning  State = "running"
	StatePaused   State = "paused"
	StateFinished State = "finished"
	StateFailed   State = "failed"
	StateOffline  State = "offline"
)

// AllStates lists every valid State, used for validation and docs.
var AllStates = []State{StateIdle, StateRunning, StatePaused, StateFinished, StateFailed, StateOffline}

// Status is the normalized, connector-agnostic view of one printer.
type Status struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"` // "bambu" | "moonraker" | "mock"
	Name         string    `json:"name"`
	State        State     `json:"state"`
	Progress     float64   `json:"progress"` // 0-100, clamped
	RemainingMin int       `json:"remaining_min"`
	JobName      string    `json:"job_name,omitempty"`
	Layer        int       `json:"layer,omitempty"`
	TotalLayers  int       `json:"total_layers,omitempty"`
	NozzleTemp   float64   `json:"nozzle_temp,omitempty"`
	BedTemp      float64   `json:"bed_temp,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	WebcamURL    string    `json:"webcam_url,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// IsAlertable reports whether a state transition into s should trigger an
// alert event (only meaningful for terminal job states).
func (s Status) IsAlertable() bool {
	return s.State == StateFailed || s.State == StateFinished
}
