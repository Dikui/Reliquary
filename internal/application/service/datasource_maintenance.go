package service

import (
	"encoding/json"
	"reflect"
	"strings"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Typed connector configuration excludes maintenance provenance and older
// extension fields. Preserve those stored fields through settings and credential
// edits while keeping credential encryption in the existing serialization path.
func dataSourceConfigWithStoredFields(original types.JSON, config *types.DataSourceConfig) (types.JSON, error) {
	encoded, err := config.ToJSON()
	if err != nil {
		return nil, err
	}
	var stored, result map[string]json.RawMessage
	if len(original) > 0 {
		if err := json.Unmarshal(original, &stored); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	// Preserve the raw representation of unchanged public fields, including
	// absent members, JSON null, nested extension values and empty arrays.
	var previous types.DataSourceConfig
	if len(original) > 0 {
		ds := &types.DataSource{Config: original}
		if parsed, parseErr := ds.ParseConfig(); parseErr == nil && parsed != nil {
			previous = *parsed
		}
	}
	for key, unchanged := range map[string]bool{
		"type":         config.Type == previous.Type,
		"resource_ids": reflect.DeepEqual(config.ResourceIDs, previous.ResourceIDs),
		"settings":     reflect.DeepEqual(config.Settings, previous.Settings),
	} {
		if unchanged {
			if raw, present := stored[key]; present {
				result[key] = raw
			} else {
				delete(result, key)
			}
		}
	}
	for key, value := range stored {
		switch key {
		case "type", "credentials", "resource_ids", "settings":
			continue
		}
		result[key] = value
	}
	return json.Marshal(result)
}

func checkDataSourceMaintenance(ds *types.DataSource) error {
	missing, err := ds.MissingMaintenanceFields()
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return apperrors.NewServiceUnavailableError("Data source remains paused for maintenance. Missing configuration fields: " + strings.Join(missing, ", "))
	}
	return nil
}

func reconcileDataSourceMaintenance(ds *types.DataSource) error {
	if !ds.HasMaintenanceRequirements() {
		return nil
	}
	err := checkDataSourceMaintenance(ds)
	if err != nil {
		ds.Status = types.DataSourceStatusPaused
		return err
	}
	if original := ds.MaintenanceOriginalStatus(); original != "" && ds.Status == types.DataSourceStatusPaused {
		ds.Status = original
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(ds.Config, &config); err != nil {
		return err
	}
	delete(config, "_reliquary_maintenance_required_fields")
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	ds.Config = encoded
	return nil
}
