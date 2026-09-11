// Package mock simulates a small mixed printer fleet for demos and
// screenshot-friendly first runs. It pushes the same normalized statuses
// the real connectors produce, so the whole app is exercised.
package mock

import (
	"context"
	"time"

	"github.com/Dukk11/printpilot/internal/printer"
)

// Fleet starts four simulated printers and keeps them ticking until ctx is
// cancelled. onUpdate receives every status change.
func Fleet(ctx context.Context, onUpdate func(printer.Status)) {
	sims := []sim{
		{id: "x1c", typ: printer.TypeBambu, name: "Bambu X1 Carbon", script: benchyJob},
		{id: "p1s", typ: printer.TypeBambu, name: "Bambu P1S", script: vaseJob},
		{id: "voron", typ: printer.TypeMoonraker, name: "Voron 2.4", script: voronJob},
		{id: "prusa", typ: printer.TypeMoonraker, name: "Prusa MK4", script: failingJob},
	}
	for i := range sims {
		go sims[i].run(ctx, onUpdate)
	}
}

// script drives one simulated printer over time.
type script func(elapsed time.Duration) printer.Status

type sim struct {
	id, typ, name string
	script        script
}

func (s *sim) run(ctx context.Context, onUpdate func(printer.Status)) {
	start := time.Now()
	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	push := func() {
		st := s.script(time.Since(start))
		st.ID, st.Type, st.Name = s.id, s.typ, s.name
		st.UpdatedAt = time.Now()
		onUpdate(st)
	}
	push() // initial state immediately
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			push()
		}
	}
}

// benchyJob: idle, then a short 40% print, then finished.
func benchyJob(elapsed time.Duration) printer.Status {
	st := base()
	switch {
	case elapsed < 8*time.Second:
		st.State = printer.StateIdle
		st.NozzleTemp, st.BedTemp = 28, 24
	case elapsed < 30*time.Second:
		progress := float64(elapsed-8*time.Second) / float64(22*time.Second) * 40
		st.State = printer.StateRunning
		st.JobName = "3DBenchy.gcode.3mf"
		st.Progress = progress
		st.RemainingMin = int((40 - progress) * 0.5) // pretend ~50 min full print
		st.Layer, st.TotalLayers = int(progress/4)+1, 10
		st.NozzleTemp, st.BedTemp = 220, 55
	default:
		st.State = printer.StateFinished
		st.JobName = "3DBenchy.gcode.3mf"
		st.Progress = 100
		st.NozzleTemp, st.BedTemp = 148, 46
	}
	return st
}

// vaseJob: already deep into a long print with slow progress.
func vaseJob(elapsed time.Duration) printer.Status {
	st := base()
	st.State = printer.StateRunning
	st.JobName = "spiral_vase_planter.gcode.3mf"
	st.Progress = 62 + float64(elapsed%(90*time.Second))/float64(90*time.Second)*2
	st.RemainingMin = 87
	st.Layer, st.TotalLayers = 188, 302
	st.NozzleTemp, st.BedTemp = 215, 60
	return st
}

// voronJob: paused at 71% with the hotend hot.
func voronJob(elapsed time.Duration) printer.Status {
	st := base()
	st.State = printer.StatePaused
	st.JobName = "calibration_cube_pla.gcode"
	st.Progress = 71
	st.RemainingMin = 24
	st.NozzleTemp, st.BedTemp = 205, 60
	st.Detail = "filament runout — waiting for reload"
	return st
}

// failingJob: runs to 35%, fails with a recognizable message, then idles.
func failingJob(elapsed time.Duration) printer.Status {
	st := base()
	switch {
	case elapsed < 10*time.Second:
		st.State = printer.StateRunning
		st.JobName = "enclosure_hinge_v2.gcode"
		st.Progress = float64(elapsed) / float64(10*time.Second) * 35
		st.RemainingMin = 46
		st.NozzleTemp, st.BedTemp = 215, 60
	case elapsed < 22*time.Second:
		st.State = printer.StateFailed
		st.JobName = "enclosure_hinge_v2.gcode"
		st.Progress = 35
		st.Detail = "spaghetti detected: first layer detached"
	default:
		st.State = printer.StateIdle
		st.NozzleTemp, st.BedTemp = 24, 23
	}
	return st
}

func base() printer.Status {
	return printer.Status{State: printer.StateIdle}
}
