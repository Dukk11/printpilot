package bambu

import (
	"testing"

	"github.com/Dukk11/printpilot/internal/printer"
)

func TestParseReportRunning(t *testing.T) {
	payload := []byte(`{
		"print": {
			"gcode_state": "RUNNING",
			"mc_percent": 42,
			"mc_remaining_time": "88",
			"gcode_file": "/data/Metadata/plate_1.gcode.3mf",
			"layer_num": 55,
			"total_layers": 130,
			"nozzle_temper": 220.5,
			"bed_temper": 55
		}
	}`)
	st, err := ParseReport(payload, "x1c", "X1C", "")
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if st.State != printer.StateRunning {
		t.Errorf("state = %q, want running", st.State)
	}
	if st.Progress != 42 {
		t.Errorf("progress = %v, want 42", st.Progress)
	}
	if st.RemainingMin != 88 {
		t.Errorf("remaining = %d, want 88 (string number must parse)", st.RemainingMin)
	}
	if st.JobName != "plate_1.gcode.3mf" {
		t.Errorf("job = %q, want basename", st.JobName)
	}
	if st.Layer != 55 || st.TotalLayers != 130 {
		t.Errorf("layers = %d/%d, want 55/130", st.Layer, st.TotalLayers)
	}
	if st.NozzleTemp != 220.5 || st.BedTemp != 55 {
		t.Errorf("temps = %.1f/%.1f, want 220.5/55", st.NozzleTemp, st.BedTemp)
	}
}

func TestParseReportFinishAndFailed(t *testing.T) {
	cases := map[string]printer.State{
		`{"print":{"gcode_state":"FINISH"}}`:  printer.StateFinished,
		`{"print":{"gcode_state":"FAILED"}}`:  printer.StateFailed,
		`{"print":{"gcode_state":"PAUSE"}}`:   printer.StatePaused,
		`{"print":{"gcode_state":"IDLE"}}`:    printer.StateIdle,
		`{"print":{"gcode_state":"SLICING"}}`: printer.StateRunning,
	}
	for payload, want := range cases {
		st, err := ParseReport([]byte(payload), "id", "n", "")
		if err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		if st.State != want {
			t.Errorf("%s: state = %q, want %q", payload, st.State, want)
		}
	}
}

func TestParseReportEmptyRemaining(t *testing.T) {
	st, err := ParseReport([]byte(`{"print":{"gcode_state":"IDLE","mc_remaining_time":""}}`), "id", "n", "")
	if err != nil {
		t.Fatalf("empty mc_remaining_time must not fail: %v", err)
	}
	if st.RemainingMin != 0 {
		t.Errorf("remaining = %d, want 0", st.RemainingMin)
	}
}

func TestParseReportMalformed(t *testing.T) {
	if _, err := ParseReport([]byte(`not json`), "id", "n", ""); err == nil {
		t.Fatal("malformed payload must return an error")
	}
}

func TestClampPct(t *testing.T) {
	if got := clampPct(-5); got != 0 {
		t.Errorf("clampPct(-5) = %v", got)
	}
	if got := clampPct(150); got != 100 {
		t.Errorf("clampPct(150) = %v", got)
	}
}
