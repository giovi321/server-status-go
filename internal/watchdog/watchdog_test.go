package watchdog

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyWritesToSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "notify.sock")
	laddr := &net.UnixAddr{Name: sockPath, Net: "unixgram"}
	ln, err := net.ListenUnixgram("unixgram", laddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)

	Ready()
	buf := make([]byte, 64)
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := ln.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatalf("got %q", string(buf[:n]))
	}
}

func TestNotifyNoSocketIsNoop(t *testing.T) {
	os.Unsetenv("NOTIFY_SOCKET")
	Ping() // must not panic or block
}

func TestDeadline(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "180000000")
	d, ok := Deadline()
	if !ok || d != 180*time.Second {
		t.Fatalf("got %v ok=%v, want 180s true", d, ok)
	}
	os.Unsetenv("WATCHDOG_USEC")
	if _, ok := Deadline(); ok {
		t.Fatal("Deadline must report ok=false when WATCHDOG_USEC is unset")
	}
}
