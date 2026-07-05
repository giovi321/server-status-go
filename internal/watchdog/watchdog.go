// Package watchdog implements the systemd sd_notify protocol (no cgo).
package watchdog

import (
	"net"
	"os"
)

// Notify sends a state string to the systemd notify socket ($NOTIFY_SOCKET).
// It is a no-op when not running under a Type=notify unit.
func Notify(state string) {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

// Ready tells systemd the service finished starting.
func Ready() { Notify("READY=1") }

// Ping resets the systemd watchdog timer.
func Ping() { Notify("WATCHDOG=1") }
