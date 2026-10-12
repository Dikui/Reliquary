package maintenance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type portableRequirementCopy struct {
	FormatVersion int           `json:"format_version"`
	Required      []Requirement `json:"required"`
	SHA256        string        `json:"sha256"`
}

func requirementCopy(required []Requirement) ([]byte, error) {
	payload, err := json.Marshal(required)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(portableRequirementCopy{FormatVersion: FormatVersion, Required: required, SHA256: sum(payload)}, "", "  ")
	return append(data, '\n'), err
}

func decodeRequirementCopy(data []byte) ([]Requirement, bool) {
	var copy portableRequirementCopy
	if json.Unmarshal(data, &copy) != nil || !supportedFormat(copy.FormatVersion) {
		return nil, false
	}
	payload, err := json.Marshal(copy.Required)
	return currentRequirements(copy.Required), err == nil && copy.SHA256 == sum(payload)
}

func previousPortableRequirements(directory string, snapshot *Snapshot) ([]Requirement, error) {
	primary, primaryErr := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if primaryErr != nil && !errors.Is(primaryErr, os.ErrNotExist) {
		return nil, primaryErr
	}
	var manifest ReplicaManifest
	primaryValid := primaryErr == nil && json.Unmarshal(primary, &manifest) == nil && supportedFormat(manifest.FormatVersion)
	redundant, redundantErr := os.ReadFile(filepath.Join(directory, "config", "required_credentials.json"))
	if redundantErr != nil && !errors.Is(redundantErr, os.ErrNotExist) {
		return nil, redundantErr
	}
	backup, redundantValid := decodeRequirementCopy(redundant)
	redundantValid = redundantErr == nil && redundantValid
	if primaryValid || redundantValid {
		previous := []Requirement{}
		if primaryValid {
			previous = append(previous, currentRequirements(manifest.Required)...)
		}
		if redundantValid {
			previous = append(previous, backup...)
		}
		return previous, nil
	}
	if errors.Is(primaryErr, os.ErrNotExist) && errors.Is(redundantErr, os.ErrNotExist) {
		return nil, nil
	}
	// A pre-redundancy replica with its original configured database can be
	// rebuilt from that database's actual parser credential field inventory.
	if errors.Is(redundantErr, os.ErrNotExist) {
		for _, requirement := range snapshot.Required {
			if requirement.Kind == "spaces" {
				for _, field := range requirement.Fields {
					if parserCredentialRequirement(field) {
						return nil, nil
					}
				}
			}
		}
	}
	return nil, errors.New("portable credential reminders are unreadable; restore them from backup before replacing the replica")
}

// The database's current credential inventory remains separate from portable
// reminders. A sanitized space has no remaining parser secret from which to
// rediscover the original field names, so the previous portable reminder must
// survive another flush until the credentials are supplied on the new host.
func portableRequirements(directory string, snapshot *Snapshot) ([]Requirement, error) {
	previous, err := previousPortableRequirements(directory, snapshot)
	if err != nil {
		return nil, err
	}
	spaces := map[string]bool{}
	for _, row := range snapshot.Tables["spaces"] {
		spaces[fmt.Sprint(row["id"])] = true
	}
	result := make([]Requirement, len(snapshot.Required))
	bySpace := map[string]int{}
	for i, requirement := range snapshot.Required {
		result[i] = requirement
		result[i].Fields = append([]string{}, requirement.Fields...)
		if requirement.Kind == "spaces" {
			bySpace[requirement.ID] = i
		}
	}
	for _, old := range previous {
		if old.Kind != "spaces" || !spaces[old.ID] {
			continue
		}
		fields := []string{}
		for _, field := range old.Fields {
			if parserCredentialRequirement(field) {
				fields = append(fields, field)
			}
		}
		if len(fields) == 0 {
			continue
		}
		index, found := bySpace[old.ID]
		if !found {
			old.Fields = []string{}
			index = len(result)
			result = append(result, old)
			bySpace[old.ID] = index
		}
		unique := map[string]bool{}
		for _, field := range append(result[index].Fields, fields...) {
			unique[field] = true
		}
		result[index].Fields = []string{}
		for field := range unique {
			result[index].Fields = append(result[index].Fields, field)
		}
		sort.Strings(result[index].Fields)
	}
	return result, nil
}
