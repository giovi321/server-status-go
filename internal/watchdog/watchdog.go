// Package watchdog implements the systemd sd_notify protocol (no cgo).
package watchdog

import (
	"net"
	"os"
	"strconv"
	"time"
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

// Deadline returns the systemd watchdog deadline from $WATCHDOG_USEC. ok is false
// when no watchdog is configured (variable unset or invalid).
func Deadline() (time.Duration, bool) {
	usec := os.Getenv("WATCHDOG_USEC")
	if usec == "" {
		return 0, false
	}
	n, err := strconv.Atoi(usec)
	if err != nil || n <= 0 {
		return 0, false
	}
	return time.Duration(n) * time.Microsecond, true
}
