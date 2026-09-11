// Command printpilot is a zero-cloud, single-binary dashboard for mixed
// 3D printer fleets (Bambu Lab LAN/MQTT, Klipper/Moonraker). Download,
// configure one JSON file, run — no Docker, no vendor cloud.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Dukk11/printpilot/internal/alerts"
	"github.com/Dukk11/printpilot/internal/bambu"
	"github.com/Dukk11/printpilot/internal/config"
	"github.com/Dukk11/printpilot/internal/mock"
	"github.com/Dukk11/printpilot/internal/moonraker"
	"github.com/Dukk11/printpilot/internal/printer"
	"github.com/Dukk11/printpilot/internal/store"
	"github.com/Dukk11/printpilot/internal/web"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	configPath := flag.String("config", "config.json", "path to the JSON config file")
	mockFleet := flag.Bool("mock", false, "run the built-in demo fleet instead of real printers")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("printpilot", version)
		return
	}

	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("printpilot %s starting", version)

	if *mockFleet {
		if err := runDemo(); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v\nRun `printpilot --mock` to see the dashboard with a demo fleet.", err)
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// runDemo starts the simulated fleet on 127.0.0.1 so users can preview the
// dashboard without owning a single printer.
func runDemo() error {
	st := store.New(nil)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := web.New(st, true, version)
	if err != nil {
		return err
	}
	mock.Fleet(ctx, func(p printer.Status) { _, _ = st.Update(p) })

	addr := fmt.Sprintf("127.0.0.1:%d", config.DefaultPort)
	return serve(ctx, srv, addr, "--mock demo fleet")
}

func run(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ids := make([]string, 0, len(cfg.Printers))
	for _, p := range cfg.Printers {
		ids = append(ids, p.ID)
	}
	st := store.New(ids)

	alertMgr := alerts.New(cfg, "alerts-state.json")
	st.OnOffline(func(id string) {
		if prev, ok := st.Get(id); ok {
			alertMgr.Observe(printer.Status{State: printer.StateRunning}, true, prev)
		}
	})
	if !alertMgr.Enabled() {
		log.Printf("alerts: no telegram/webhook configured — running without notifications")
	}

	pins, err := bambu.NewPinStore("printer-pins.json")
	if err != nil {
		log.Printf("bambu: pin store: %v (falling back to in-memory pins)", err)
		pins, _ = bambu.NewPinStore("")
	}

	srv, err := web.New(st, false, version)
	if err != nil {
		return err
	}

	update := func(p printer.Status) {
		prev, had := st.Update(p)
		alertMgr.Observe(prev, had, p)
	}

	for _, p := range cfg.Printers {
		switch p.Type {
		case printer.TypeBambu:
			c := bambu.New(p.ID, p.Name, p.Host, p.Serial, p.AccessCode, p.WebcamURL, pins, update)
			if err := c.Start(); err != nil {
				log.Printf("printer %s: %v (retrying in background)", p.ID, err)
			}
			defer c.Close()
		case printer.TypeMoonraker:
			c := moonraker.New(p.ID, p.Name, p.Host, p.Port, p.WebcamURL, update)
			c.Start(ctx)
		}
		log.Printf("printer %s (%s): connecting to %s", p.ID, p.Type, p.Host)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	return serve(ctx, srv, addr, fmt.Sprintf("%d printer(s)", len(cfg.Printers)))
}

func serve(ctx context.Context, srv *web.Server, addr, what string) error {
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("dashboard: http://%s (%s)", addr, what)
	err := httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
