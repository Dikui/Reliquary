package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Queued tasks from before the rename retain their original envelope. New
// tasks write library_id; only the queue reader accepts the historical key.
func normalizeLibraryPayload(fields map[string]json.RawMessage) error {
	var id *string
	for key, value := range fields {
		if !strings.EqualFold(key, "knowledge_base_id") && !strings.EqualFold(key, "library_id") {
			continue
		}
		var decoded string
		if err := json.Unmarshal(value, &decoded); err != nil {
			return err
		}
		if id != nil && *id != decoded {
			return fmt.Errorf("conflicting task library keys")
		}
		id = &decoded
		delete(fields, key)
	}
	if id != nil {
		fields["library_id"], _ = json.Marshal(*id)
	}
	return nil
}
