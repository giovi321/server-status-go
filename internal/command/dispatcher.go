// Package command routes named control commands to handlers.
package command

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"sync"
)

// Result is the uniform outcome of a command.
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// Handler runs one command.
type Handler func(ctx context.Context) Result

// Dispatcher maps command names to handlers.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// New builds an empty dispatcher.
func New() *Dispatcher { return &Dispatcher{handlers: map[string]Handler{}} }

// Register adds or replaces a command handler.
func (d *Dispatcher) Register(name string, h Handler) {
	d.mu.Lock()
	d.handlers[name] = h
	d.mu.Unlock()
}

// Run executes a command by name.
func (d *Dispatcher) Run(ctx context.Context, name string) Result {
	d.mu.RLock()
	h := d.handlers[name]
	d.mu.RUnlock()
	if h == nil {
		return Result{OK: false, Message: "unknown command: " + name}
	}
	return h(ctx)
}

// Names returns the registered command names, sorted.
func (d *Dispatcher) Names() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	names := make([]string, 0, len(d.handlers))
	for n := range d.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RestartHandler restarts a systemd service.
func RestartHandler(service string) Handler {
	return func(ctx context.Context) Result {
		out, err := exec.CommandContext(ctx, "systemctl", "restart", service).CombinedOutput()
		if err != nil {
			return Result{OK: false, Message: fmt.Sprintf("restart failed: %v: %s", err, out)}
		}
		return Result{OK: true, Message: "restarting"}
	}
}
