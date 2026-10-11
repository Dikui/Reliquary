package service

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

// CompleteMaintenanceModelState shares credential completion rules with the
// offline maintenance CLI. The caller persists the model without logging values.
func CompleteMaintenanceModelState(ctx context.Context, db *gorm.DB, model *types.Model) (bool, error) {
	return (&modelService{db: db}).restoreMaintenanceModel(ctx, model)
}

const (
	maintenanceRequiredFields    = "maintenance_required_fields"
	maintenanceOriginalStatus    = "maintenance_original_status"
	maintenanceOriginalUpdatedAt = "maintenance_original_updated_at"
)

// restoreMaintenanceModel restores only the special migration pause. Existing
// download and provider states retain their earlier behavior. Values never
// appear in the marker or in the API response.
func (s *modelService) restoreMaintenanceModel(ctx context.Context, model *types.Model) (bool, error) {
	if model.Status != types.ModelStatusNeedsConfiguration {
		return false, nil
	}
	var fields []string
	if json.Unmarshal([]byte(model.Parameters.ExtraConfig[maintenanceRequiredFields]), &fields) != nil || len(fields) == 0 {
		return false, nil
	}
	for _, field := range fields {
		field = strings.TrimPrefix(field, "parameters.")
		if field == "chatgpt_login" {
			if s.db == nil {
				return false, nil
			}
			accountID := model.Parameters.ExtraConfig["chatgpt_account_id"]
			if accountID == "" {
				return false, nil
			}
			var connected int64
			if err := s.db.WithContext(ctx).Table("chatgpt_accounts").Where("id=? AND status='connected' AND credentials<>''", accountID).Count(&connected).Error; err != nil {
				return false, err
			}
			if connected == 0 {
				return false, nil
			}
			continue
		}
		if !maintenanceFieldConfigured(model.Parameters, field) {
			return false, nil
		}
	}
	model.Status = types.ModelStatusActive
	if original := model.Parameters.ExtraConfig[maintenanceOriginalStatus]; original != "" {
		model.Status = types.ModelStatus(original)
	}
	delete(model.Parameters.ExtraConfig, maintenanceRequiredFields)
	return true, nil
}

// The portable snapshot owns its provenance markers. Ordinary settings edits
// preserve existing markers and cannot invent or remove the original state.
func preserveMaintenanceProvenance(model, existing *types.Model) {
	if existing == nil {
		return
	}
	for _, key := range []string{maintenanceOriginalStatus, maintenanceOriginalUpdatedAt} {
		if original, present := existing.Parameters.ExtraConfig[key]; present {
			if model.Parameters.ExtraConfig == nil {
				model.Parameters.ExtraConfig = map[string]string{}
			}
			model.Parameters.ExtraConfig[key] = original
		} else {
			delete(model.Parameters.ExtraConfig, key)
		}
	}
}

func maintenanceFieldConfigured(parameters types.ModelParameters, field string) bool {
	switch field {
	case "api_key":
		return strings.TrimSpace(parameters.APIKey) != ""
	case "app_secret":
		return strings.TrimSpace(parameters.AppSecret) != ""
	case "base_url.userinfo":
		parsed, err := url.Parse(parameters.BaseURL)
		return err == nil && parsed.User != nil && parsed.User.Username() != ""
	}
	if name, ok := strings.CutPrefix(field, "custom_headers."); ok {
		for key, value := range parameters.CustomHeaders {
			if strings.EqualFold(key, name) && strings.TrimSpace(value) != "" {
				return true
			}
		}
		return false
	}
	if name, ok := strings.CutPrefix(field, "extra_config."); ok {
		return strings.TrimSpace(parameters.ExtraConfig[name]) != ""
	}
	if name, ok := strings.CutPrefix(field, "base_url.query."); ok {
		parsed, err := url.Parse(parameters.BaseURL)
		if err != nil {
			return false
		}
		for key, values := range parsed.Query() {
			if strings.EqualFold(key, name) {
				for _, value := range values {
					if strings.TrimSpace(value) != "" {
						return true
					}
				}
			}
		}
	}
	return false
}
