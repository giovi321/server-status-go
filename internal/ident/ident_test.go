package ident

import (
	"testing"

	"github.com/giovi321/server-status/internal/config"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"GC01srvr":      "gc01srvr",
		"host.lan":      "host-lan",
		"weird__Name!!": "weird-name",
		"--edges--":     "edges",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIdentifyDefaultsToHostname(t *testing.T) {
	dev := Identify(config.Config{}, "GC01srvr")
	if dev.Node != "gc01srvr" {
		t.Fatalf("node %q", dev.Node)
	}
	if dev.Identifier != "server-status-gc01srvr" {
		t.Fatalf("identifier %q", dev.Identifier)
	}
	if dev.Name != "gc01srvr" {
		t.Fatalf("name %q", dev.Name)
	}
}

func TestIdentifyOverrides(t *testing.T) {
	dev := Identify(config.Config{Node: "vm-web", FriendlyName: "Web VM", Parent: "GC01srvr"}, "ignored")
	if dev.Node != "vm-web" || dev.Name != "Web VM" {
		t.Fatalf("got %+v", dev)
	}
	if dev.Parent != "gc01srvr" {
		t.Fatalf("parent %q", dev.Parent)
	}
}
