// Command server-status publishes host metrics to MQTT with Home Assistant discovery.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/giovi321/server-status/internal/command"
	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/control"
	"github.com/giovi321/server-status/internal/detect"
	"github.com/giovi321/server-status/internal/ident"
	"github.com/giovi321/server-status/internal/sink"
	"github.com/giovi321/server-status/internal/update"
	"github.com/giovi321/server-status/internal/version"
	"github.com/giovi321/server-status/internal/watchdog"
)

func main() {
	var (
		cfgPath  = flag.String("c", "", "path to YAML config file")
		once     = flag.Bool("once", false, "run one cycle then exit")
		dump     = flag.Bool("dump-detected", false, "print detected collectors and metrics as JSON, then exit")
		showVer  = flag.Bool("version", false, "print version and exit")
		loopSecs = flag.Int("interval", 60, "seconds between cycles")
		purge    = flag.Bool("purge", false, "clear this host's retained MQTT discovery and exit")
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

	cols := detect.Available(detect.All(cfg))

	if *dump {
		if err := detect.DumpJSON(os.Stdout, dev, detect.All(cfg), ctx); err != nil {
			log.Fatalf("dump: %v", err)
		}
		return
	}

	refreshCh := make(chan struct{}, 1)
	disp := command.New()
	disp.Register("refresh", func(context.Context) command.Result {
		select {
		case refreshCh <- struct{}{}:
		default:
		}
		return command.Result{OK: true, Message: "refresh queued"}
	})
	disp.Register("restart", command.RestartHandler("server-status"))
	disp.Register("update", func(ctx context.Context) command.Result {
		assetName := fmt.Sprintf("server-status-linux-%s", runtime.GOARCH)
		rel, err := update.Latest(ctx, "https://api.github.com", cfg.Update.Repo, assetName)
		if err != nil {
			return command.Result{OK: false, Message: "check failed: " + err.Error()}
		}
		if rel.Version == version.Version {
			return command.Result{OK: true, Message: "already up to date (" + rel.Version + ")"}
		}
		self, err := os.Executable()
		if err != nil {
			return command.Result{OK: false, Message: "executable path: " + err.Error()}
		}
		if err := update.Apply(ctx, nil, rel, self); err != nil {
			return command.Result{OK: false, Message: "apply failed: " + err.Error()}
		}
		go command.RestartHandler("server-status")(context.Background())
		return command.Result{OK: true, Message: "updated to " + rel.Version + ", restarting"}
	})

	var sinks []sink.Sink
	for _, sc := range cfg.Sinks {
		switch sc.Type {
		case "mqtt":
			sinks = append(sinks, sink.NewMQTT(sc, dev, disp))
		case "webhook":
			sinks = append(sinks, sink.NewWebhook(sc))
		default:
			log.Printf("unknown sink type %q, skipping", sc.Type)
		}
	}
	if len(sinks) == 0 {
		log.Fatal("no usable sinks configured")
	}
	connected := sinks[:0]
	for _, sk := range sinks {
		if err := sk.Connect(); err != nil {
			log.Printf("sink %T connect failed, dropping: %v", sk, err)
			_ = sk.Close()
			continue
		}
		connected = append(connected, sk)
	}
	sinks = connected
	if len(sinks) == 0 {
		log.Fatal("all sinks failed to connect")
	}
	defer func() {
		for _, sk := range sinks {
			_ = sk.Close()
		}
	}()

	if *purge {
		snap := detect.Snapshot(ctx, dev, cols)
		for _, sk := range sinks {
			if mq, ok := sk.(*sink.MQTT); ok {
				if err := mq.Purge(snap); err != nil {
					log.Printf("purge: %v", err)
				}
			}
		}
		log.Print("purged retained discovery; exiting")
		return
	}

	var ctrl *control.Server
	if cfg.Control.HTTP.Enabled {
		ctrl = control.NewServer(cfg.Control.HTTP, version.Version)
		if err := ctrl.Start(); err != nil {
			log.Printf("control server disabled: %v", err)
			ctrl = nil
		}
	}
	if ctrl != nil {
		ctrl.SetDispatcher(disp)
	}

	watchdog.Ready()

	cycle := func() {
		snap := detect.Snapshot(ctx, dev, cols)
		for _, sk := range sinks {
			if err := sk.Publish(snap); err != nil {
				log.Printf("publish: %v", err)
			}
		}
		if ctrl != nil {
			ctrl.Update(snap)
		}
		watchdog.Ping()
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
		case <-refreshCh:
			cycle()
		}
	}
}
