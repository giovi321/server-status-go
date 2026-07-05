# Migrating from the Python server-status

The Go agent (this repo) replaces the archived Python tool. Cut over one host at
a time:

1. Install the Go agent (see the README). It autodetects and publishes under a
   new per-host MQTT device named for the host's `node` (default hostname), so it
   will not collide with the old Python device.
2. Confirm the new device and its entities appear in Home Assistant and read
   correctly.
3. Stop and remove the old Python service on that host:
   `systemctl disable --now server-status` (the old unit), and delete its files.
4. In Home Assistant, delete the old Python MQTT device (it will be stale). If it
   used retained discovery, clear those retained topics on the broker.
5. Repeat per host.

To cleanly remove the Go agent from a host (and its Home Assistant device), run
`sudo ./scripts/install.sh --uninstall`. It stops the service, runs
`server-status --purge` (which clears the retained MQTT discovery so the Home
Assistant device disappears), and removes the systemd unit. It leaves
`/etc/server-status` and `/opt/server-status` in place; delete them by hand to
remove the config and binary too.
