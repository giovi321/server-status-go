// Command server-status publishes host metrics to MQTT with Home Assistant discovery.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/detect"
	"github.com/giovi321/server-status/internal/ident"
	"github.com/giovi321/server-status/internal/sink"
	"github.com/giovi321/server-status/internal/version"
)

func main() {
	var (
		cfgPath  = flag.String("c", "", "path to YAML config file")
		once     = flag.Bool("once", false, "run one cycle then exit")
		dump     = flag.Bool("dump-detected", false, "print detected collectors and metrics as JSON, then exit")
		showVer  = flag.Bool("version", false, "print version and exit")
		loopSecs = flag.Int("interval", 60, "seconds between cycles")
	)
	flag.StringVar(cfgPath, "config", "", "path to YAML config file")
	flag.Parse()

	if *showVer {
		fmt.Println(version.Version)
		return
	}
	if *cfgPath == "" {
		log.Fatal("missing -c/--config")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	hostname, _ := os.Hostname()
	dev := ident.Identify(cfg, hostname)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cols := detect.Available(detect.All())

	if *dump {
		if err := detect.DumpJSON(os.Stdout, dev, cols, ctx); err != nil {
			log.Fatalf("dump: %v", err)
		}
		return
	}

	// Phase 1: one MQTT sink. Later plans generalize to a sink list.
	var sk sink.Sink
	for _, sc := range cfg.Sinks {
		if sc.Type == "mqtt" {
			sk = sink.NewMQTT(sc, dev)
			break
		}
	}
	if sk == nil {
		log.Fatal("no mqtt sink configured")
	}
	if err := sk.Connect(); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer sk.Close()

	cycle := func() {
		snap := detect.Snapshot(ctx, dev, cols)
		if err := sk.Publish(snap); err != nil {
			log.Printf("publish: %v", err)
		}
	}

	cycle()
	if *once {
		return
	}
	ticker := time.NewTicker(time.Duration(*loopSecs) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Print("shutting down")
			return
		case <-ticker.C:
			cycle()
		}
	}
}
