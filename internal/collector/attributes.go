package collector

import (
	"encoding/json"

	"github.com/giovi321/server-status/internal/ha"
)

// attrBudget is the byte budget for one metric's attribute object. Home
// Assistant's recorder refuses to store attributes larger than
// ha.MaxAttributesBytes; the reserve leaves room for the attributes Home
// Assistant adds itself (friendly_name, icon, entity_category, device_class).
const attrBudget = ha.MaxAttributesBytes - 1024

// listAttributes wraps items in a JSON object under key — Home Assistant needs
// an object, not a bare array — dropping trailing entries and counting them
// under "omitted" when the list would exceed attrBudget.
func listAttributes[T any](key string, items []T) map[string]any {
	size := 2 // the enclosing [ ]
	for i, it := range items {
		b, err := json.Marshal(it)
		if err != nil {
			continue
		}
		size += len(b) + 1 // the entry plus its separating comma
		if size > attrBudget {
			return map[string]any{key: items[:i], "omitted": len(items) - i}
		}
	}
	if items == nil {
		items = []T{} // marshal an empty list, not null
	}
	return map[string]any{key: items}
}
