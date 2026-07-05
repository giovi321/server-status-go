// Package ident derives the stable, human-readable host identity.
package ident

import (
	"regexp"
	"strings"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
	"github.com/giovi321/server-status/internal/version"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Sanitize turns an arbitrary label into a short slug of [a-z0-9-].
func Sanitize(s string) string {
	s = strings.ToLower(s)
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// Identify builds the host Device from config, falling back to the hostname for the node.
func Identify(cfg config.Config, hostname string) model.Device {
	node := cfg.Node
	if node == "" {
		node = Sanitize(hostname)
	} else {
		node = Sanitize(node)
	}
	name := cfg.FriendlyName
	if name == "" {
		name = node
	}
	return model.Device{
		Node:         node,
		Name:         name,
		Identifier:   "server-status-" + node,
		Parent:       Sanitize(cfg.Parent),
		Hierarchy:    cfg.Hierarchy,
		Manufacturer: "server-status",
		SWVersion:    version.Version,
	}
}
