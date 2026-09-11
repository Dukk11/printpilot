package alerts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Dukk11/printpilot/internal/printer"
)

// recordingNotifier captures deliveries without any network I/O.
type recordingNotifier struct {
	mu   sync.Mutex
	got  []Message
	fail bool
}

func (r *recordingNotifier) Name() string { return "recording" }

func (r *recordingNotifier) Send(_ context.Context, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return context.DeadlineExceeded
	}
	r.got = append(r.got, m)
	return nil
}

func newTestManager(notifiers ...Notifier) *Manager {
	m := New(nil, "") // nil cfg: no built-in notifiers, dedup in memory
	m.notifiers = notifiers
	return m
}

func TestFirstObservationNeverAlerts(t *testing.T) {
	n := &recordingNotifier{}
	m := newTestManager(n)
	m.Observe(printer.Status{}, false, printer.Status{ID: "a", State: printer.StateFinished})
	if len(n.got) != 0 {
		t.Fatalf("first observation alerted: %+v", n.got)
	}
}

func TestFinishedAlertFiresOnce(t *testing.T) {
	n := &recordingNotifier{}
	m := newTestManager(n)
	m.Observe(printer.Status{State: printer.StateRunning}, true, printer.Status{ID: "a", State: printer.StateFinished, JobName: "benchy"})
	m.Observe(printer.Status{State: printer.StateFinished}, true, printer.Status{ID: "a", State: printer.StateFinished, JobName: "benchy"})
	m.Observe(printer.Status{State: printer.StateRunning}, true, printer.Status{ID: "a", State: printer.StateFinished, JobName: "benchy"})
	time.Sleep(50 * time.Millisecond) // delivery is async
	if len(n.got) != 1 {
		t.Fatalf("got %d alerts, want exactly 1 (dedup)", len(n.got))
	}
	if n.got[0].Event != "finished" || n.got[0].Job != "benchy" {
		t.Errorf("alert = %+v", n.got[0])
	}
}

func TestFailedAndFinishedAreSeparate(t *testing.T) {
	n := &recordingNotifier{}
	m := newTestManager(n)
	m.Observe(printer.Status{State: printer.StateRunning}, true, printer.Status{ID: "a", State: printer.StateFailed, JobName: "x"})
	m.Observe(printer.Status{State: printer.StateFailed}, true, printer.Status{ID: "a", State: printer.StateFinished, JobName: "x"})
	time.Sleep(50 * time.Millisecond) // delivery is async
	if len(n.got) != 2 {
		t.Fatalf("got %d alerts, want 2", len(n.got))
	}
}

func TestEventFilter(t *testing.T) {
	n := &recordingNotifier{}
	m := newTestManager(n)
	m.events = map[string]bool{"failed": true}
	m.Observe(printer.Status{State: printer.StateRunning}, true, printer.Status{ID: "a", State: printer.StateFinished, JobName: "x"})
	if len(n.got) != 0 {
		t.Fatalf("finished must be filtered, got %+v", n.got)
	}
	m.Observe(printer.Status{State: printer.StateRunning}, true, printer.Status{ID: "a", State: printer.StateFailed, JobName: "x"})
	time.Sleep(10 * time.Millisecond)
	if len(n.got) != 1 {
		t.Fatalf("failed must pass the filter, got %d", len(n.got))
	}
}

func TestWebhookPayload(t *testing.T) {
	var mu sync.Mutex
	var gotHeader string
	var gotEvent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotHeader = r.Header.Get("X-PrintPilot-Event")
		gotEvent = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := &Webhook{URL: srv.URL, Client: srv.Client()}
	err := w.Send(context.Background(), Message{Event: "failed", Printer: "X1C", Job: "a.gcode", Progress: 42})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotHeader != "failed" || gotEvent != "/" {
		t.Errorf("header=%q path=%q", gotHeader, gotEvent)
	}
}

func TestWebhookRetriesNotBuiltInButFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	w := &Webhook{URL: srv.URL, Client: srv.Client()}
	if err := w.Send(context.Background(), Message{}); err == nil {
		t.Fatal("4xx/5xx webhook responses must be an error")
	}
}
