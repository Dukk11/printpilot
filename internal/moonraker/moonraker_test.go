package moonraker

import (
	"encoding/json"
	"testing"

	"github.com/Dukk11/printpilot/internal/printer"
)

// queryRespFromJSON builds a queryResp from raw JSON for mapping tests.
func queryRespFromJSON(t *testing.T, s string) queryResp {
	t.Helper()
	var qr queryResp
	if err := json.Unmarshal([]byte(s), &qr); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return qr
}

func TestMapQueryPrinting(t *testing.T) {
	qr := queryRespFromJSON(t, `{
		"result": {
			"status": {
				"print_stats": {"state": "printing", "progress": 0.37, "filename": "benchy.gcode"},
				"extruder": {"temperature": 209.8},
				"heater_bed": {"temperature": 60.1},
				"display_status": {"progress": 0.36}
			}
		}
	}`)
	st := mapQuery(qr, "voron", "Voron", "")
	if st.State != printer.StateRunning {
		t.Errorf("state = %q, want running", st.State)
	}
	if st.Progress != 37 {
		t.Errorf("progress = %v, want 37", st.Progress)
	}
	if st.JobName != "benchy.gcode" {
		t.Errorf("job = %q", st.JobName)
	}
	if st.NozzleTemp != 209.8 || st.BedTemp != 60.1 {
		t.Errorf("temps = %.1f/%.1f", st.NozzleTemp, st.BedTemp)
	}
}

func TestMapStates(t *testing.T) {
	cases := map[string]printer.State{
		"printing":  printer.StateRunning,
		"paused":    printer.StatePaused,
		"complete":  printer.StateFinished,
		"error":     printer.StateFailed,
		"cancelled": printer.StateIdle,
		"standby":   printer.StateIdle,
	}
	for in, want := range cases {
		if got := MapState(in); got != want {
			t.Errorf("MapState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapQueryFailedMessage(t *testing.T) {
	qr := queryRespFromJSON(t, `{
		"result": {"status": {"print_stats": {"state": "error", "message": "Klipper shutdown: heater bed", "progress": 0.5}}}
	}`)
	st := mapQuery(qr, "v", "Voron", "")
	if st.State != printer.StateFailed {
		t.Errorf("state = %q, want failed", st.State)
	}
	if st.Detail != "Klipper shutdown: heater bed" {
		t.Errorf("detail = %q", st.Detail)
	}
}
