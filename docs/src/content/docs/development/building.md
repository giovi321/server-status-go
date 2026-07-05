---
title: "Building and testing"
description: "Build the binary, run the tests, and find your way around the code"
---

server-status is a single Go module, `github.com/giovi321/server-status`, with no cgo.

## Build

```bash
go build -o server-status ./cmd/server-status
```

Go 1.22 or newer. Builds are fully static (`CGO_ENABLED=0`), so the binary runs on any Linux of the target architecture without shared-library dependencies.

Cross-compile for another architecture:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o server-status-arm64 ./cmd/server-status
```

## Test

```bash
go test ./...        # unit tests
go vet ./...         # vet
gofmt -l .           # must print nothing
```

Collectors are split into a pure parser plus a thin reader, so parsing logic is tested against fixtures and runs on any platform, not only Linux.

## Layout

```
cmd/server-status/     entrypoint, flags, run loop, command wiring
internal/
  model/               Metric, Device, Snapshot
  config/              YAML load, ${ENV} expansion, defaults
  ident/               host identity (node, name, identifier)
  collector/           one file per collector + its parser and tests
  detect/              All()/Available(), the snapshot loop, collectSafe
  ha/                  discovery payloads and topic construction
  sink/                mqtt and webhook sinks
  control/             HTTP control server
  command/             command dispatcher and handlers
  update/              self-update (resolve, verify, swap)
  watchdog/            sd_notify
  version/             build version (stamped via ldflags)
```

## Adding a collector

1. Create `internal/collector/<name>.go` implementing the `Collector` interface: `Name() string`, `Available() bool`, `Collect(ctx) ([]model.Metric, error)`
2. Keep parsing pure. Read the raw source (a `/proc` file, a command's output) in one function and parse it in another so the parser can be fixture-tested
3. Emit `model.Metric` values. Set `Component`/`ComponentName` for sub-device metrics and `Instance` for multi-instance metrics
4. Register it in `detect.All()` in `internal/detect/detect.go`
5. Add a test with a fixture for the parser

`Available()` should be cheap and honest: return true only when the host has the thing the collector measures, so it does not publish empty or misleading entities.

## Local run

```bash
go run ./cmd/server-status -c config.yaml --dump-detected
go run ./cmd/server-status -c config.yaml --once
```

See [CLI flags](../../reference/cli/).
