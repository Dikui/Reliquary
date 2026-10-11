package maintenance

import (
	"encoding/json"
	"fmt"
)

// New credentials are permitted only in tenants whose original keys appear in
// the immutable recovery checklist. Every original public record is retained.
func reconfiguredAPIKeyRows(expected, actual []map[string]any, requirements []Requirement) []map[string]any {
	allowed := map[string]bool{}
	for _, item := range requirements {
		if item.Kind == "tenant_api_keys" {
			allowed[item.ID] = true
		}
	}
	old := map[string]map[string]any{}
	tenants := map[string]bool{}
	for _, row := range expected {
		id := fmt.Sprint(row["id"])
		old[id] = row
		if allowed[id] {
			tenants[fmt.Sprint(row["tenant_id"])] = true
		}
	}
	result := []map[string]any{}
	for _, row := range actual {
		id := fmt.Sprint(row["id"])
		prior, exists := old[id]
		if !exists && tenants[fmt.Sprint(row["tenant_id"])] {
			continue
		}
		if exists && allowed[id] {
			row["updated_at"] = prior["updated_at"]
			if prior["revoked_at"] == nil && row["revoked_at"] != nil {
				row["revoked_at"] = nil
			}
		}
		result = append(result, row)
	}
	sortRows(result)
	return result
}

// Older manifests contain only the hash for API key metadata. Recover rows
// from the untouched package vault and prove their original hash before use.
func PopulateRecoveryAPIKeyRows(manifest *Manifest, originalVault string) error {
	entry, exists := manifest.Tables["tenant_api_keys"]
	if !exists || entry.Data != nil || entry.Rows == 0 {
		return nil
	}
	snapshot, err := Load(originalVault)
	if err != nil {
		return fmt.Errorf("read original API key metadata: %w", err)
	}
	rows := snapshot.Tables["tenant_api_keys"]
	data, err := json.Marshal(canonicalRows("tenant_api_keys", rows))
	if err != nil || int64(len(rows)) != entry.Rows || sum(data) != entry.SHA256 {
		return fmt.Errorf("original API key metadata does not match immutable manifest")
	}
	entry.Data = rows
	manifest.Tables["tenant_api_keys"] = entry
	return nil
}
