package collector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/giovi321/server-status/internal/ha"
)

func TestListAttributesWrapsInObject(t *testing.T) {
	got := listAttributes("units", []string{"nginx.service", "backup.service"})
	if _, ok := got["omitted"]; ok {
		t.Fatalf("small list must not be trimmed: %v", got)
	}
	// Home Assistant needs a JSON object here, not a bare array.
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"units":["nginx.service","backup.service"]}` {
		t.Fatalf("payload: %s", b)
	}
}

func TestListAttributesEmptyIsNotNull(t *testing.T) {
	b, err := json.Marshal(listAttributes("units", []string(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"units":[]}` {
		t.Fatalf("nil list should marshal as an empty array, got %s", b)
	}
}

// The recorder drops attributes over MAX_STATE_ATTRS_BYTES, so an oversized
// list is trimmed and the loss is reported rather than silently swallowed.
func TestListAttributesTrimsToBudget(t *testing.T) {
	items := make([]string, 2000)
	for i := range items {
		items[i] = strings.Repeat("x", 40)
	}
	got := listAttributes("units", items)
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > ha.MaxAttributesBytes {
		t.Fatalf("attributes %d bytes exceed the %d-byte recorder cap", len(b), ha.MaxAttributesBytes)
	}
	omitted, ok := got["omitted"].(int)
	if !ok || omitted == 0 {
		t.Fatalf("trimmed list must report what was dropped: %v", got["omitted"])
	}
	kept, ok := got["units"].([]string)
	if !ok || len(kept)+omitted != len(items) {
		t.Fatalf("kept %d + omitted %d != %d", len(kept), omitted, len(items))
	}
}
