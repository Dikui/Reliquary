package types

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// MissingMaintenanceFields checks only credentials excluded by a portable
// snapshot. Ordinary paused sources retain their existing synchronization rules.
func (d *DataSource) MissingMaintenanceFields() ([]string, error) {
	if len(d.Config) == 0 {
		return nil, nil
	}
	var config map[string]any
	if err := json.Unmarshal(d.Config, &config); err != nil {
		if strings.Contains(string(d.Config), "_reliquary_maintenance_required_fields") {
			return nil, err
		}
		return nil, nil
	}
	marker, present := config["_reliquary_maintenance_required_fields"]
	if !present {
		return nil, nil
	}
	text, ok := marker.(string)
	var fields []string
	if !ok || json.Unmarshal([]byte(text), &fields) != nil {
		return nil, errors.New("data source maintenance configuration checklist is invalid")
	}
	parsed, err := d.ParseConfig()
	if err != nil {
		return nil, err
	}
	if parsed != nil {
		config["credentials"] = parsed.Credentials
	}
	var missing []string
	for _, field := range fields {
		if !maintenanceSourcePathConfigured(config, strings.TrimPrefix(field, "config.")) {
			missing = append(missing, field)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

func (d *DataSource) MaintenanceOriginalStatus() string {
	var config map[string]any
	if json.Unmarshal(d.Config, &config) != nil {
		return ""
	}
	status, _ := config["_reliquary_maintenance_original_status"].(string)
	return status
}

func (d *DataSource) HasMaintenanceRequirements() bool {
	var config map[string]any
	if json.Unmarshal(d.Config, &config) != nil {
		return strings.Contains(string(d.Config), "_reliquary_maintenance_required_fields")
	}
	_, present := config["_reliquary_maintenance_required_fields"]
	return present
}

func maintenanceSourcePathConfigured(value any, path string) bool {
	if path == "" {
		switch value := value.(type) {
		case nil:
			return false
		case string:
			return strings.TrimSpace(value) != ""
		case map[string]any:
			for _, child := range value {
				if maintenanceSourcePathConfigured(child, "") {
					return true
				}
			}
			return false
		default:
			return true
		}
	}
	if text, ok := value.(string); ok {
		if path == "userinfo" {
			u, err := url.Parse(text)
			return err == nil && u.User != nil && u.User.Username() != ""
		}
		if key, ok := strings.CutPrefix(path, "query."); ok {
			u, err := url.Parse(text)
			if err != nil {
				return false
			}
			for name, values := range u.Query() {
				if strings.EqualFold(name, key) {
					for _, item := range values {
						if strings.TrimSpace(item) != "" {
							return true
						}
					}
				}
			}
			return false
		}
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) != nil {
			return false
		}
		return maintenanceSourcePathConfigured(decoded, path)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	// Header names may contain periods, so prefer the longest matching key.
	key := ""
	for candidate := range object {
		if (strings.EqualFold(path, candidate) || strings.HasPrefix(strings.ToLower(path), strings.ToLower(candidate)+".")) && len(candidate) > len(key) {
			key = candidate
		}
	}
	if key == "" {
		return false
	}
	return maintenanceSourcePathConfigured(object[key], strings.TrimPrefix(path[len(key):], "."))
}
