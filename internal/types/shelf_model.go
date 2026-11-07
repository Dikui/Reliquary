package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ShelfComposition groups alternatives without expanding complete configurations.
type ShelfComposition struct {
	ModuleType string   `json:"module_type"`
	Optional   bool     `json:"optional"`
	Parts      []Entity `json:"parts"`
}

type ShelfModelDetail struct {
	Certifications   []ShelfCertificationView         `json:"certifications"`
	Composition      []ShelfComposition               `json:"composition"`
	SaleStatuses     []ShelfSaleStatus                `json:"sale_statuses"`
	Parameters       map[string][]ShelfParameterValue `json:"parameters"`
	CombinationRules [][]ShelfCondition               `json:"combination_rules"`
}

// Conditions are a conjunction; each module admits one of the listed parts.
type ShelfCondition struct {
	ModuleType string   `json:"module_type"`
	PartIDs    []string `json:"part_ids"`
}

type ShelfSaleStatus struct {
	Region     string           `json:"region"`
	Status     string           `json:"status"`
	PlannedEOM bool             `json:"planned_eom"`
	Notes      string           `json:"notes"`
	Raw        string           `json:"raw"`
	Conditions []ShelfCondition `json:"conditions"`
}

type ShelfParameterValue struct {
	Value      any              `json:"value"`
	Conditions []ShelfCondition `json:"conditions"`
}

type ShelfReadOptions struct {
	AsOf        *time.Time
	Combination map[string]string
}

type ShelfWarning struct {
	Type     string   `json:"type"`
	Message  string   `json:"message"`
	ModelIDs []string `json:"model_ids"`
}

func ProductShelfFields() []EntityFieldDefinition {
	return []EntityFieldDefinition{
		{Key: "module_type", Name: "模块类型", Type: "text", Kinds: []string{"part"}},
		{Key: "pack_model", Name: "Pack 型号", Type: "text", Kinds: []string{"part"}},
		{Key: "cluster_model", Name: "簇型号", Type: "text", Kinds: []string{"part"}},
		{Key: "container_model", Name: "集装箱型号", Type: "text", Kinds: []string{"part"}},
		{Key: "sale_statuses", Name: "可售状态", Type: "shelf_statuses", Kinds: []string{"model"}},
		{Key: "combination_rules", Name: "组合规则", Type: "shelf_rules", Kinds: []string{"model"}},
		{Key: "certification", Name: "认证记录", Type: "shelf_certification", Kinds: []string{"certification"}},
	}
}

// DecodeShelfValue also rejects unknown members so a typo cannot erase meaning.
func DecodeShelfValue(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func ValidateShelfConditions(conditions []ShelfCondition) error {
	if len(conditions) > 100 {
		return fmt.Errorf("at most 100 condition modules are allowed")
	}
	modules := map[string]bool{}
	for _, c := range conditions {
		if c.ModuleType == "" || modules[c.ModuleType] || len(c.PartIDs) == 0 || len(c.PartIDs) > 200 {
			return fmt.Errorf("conditions require distinct modules and nonempty part sets")
		}
		modules[c.ModuleType] = true
		parts := map[string]bool{}
		for _, id := range c.PartIDs {
			if id == "" || len(id) > 100 || parts[id] {
				return fmt.Errorf("condition parts must be nonblank and unique")
			}
			parts[id] = true
		}
	}
	return nil
}

func ValidateShelfStatuses(value any) error {
	var rows []ShelfSaleStatus
	if err := DecodeShelfValue(value, &rows); err != nil || rows == nil || len(rows) > 500 {
		return fmt.Errorf("invalid sale statuses")
	}
	for _, row := range rows {
		if row.Region == "" {
			return fmt.Errorf("region is required")
		}
		if strings.TrimSpace(row.Status) == "" {
			return fmt.Errorf("uncertain: blank regional status must be resolved by the submitter")
		}
		if !slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, row.Status) {
			return fmt.Errorf("invalid sale status")
		}
		if len([]rune(row.Notes)) > 20000 || len([]rune(row.Raw)) > 20000 {
			return fmt.Errorf("sale status text is too long")
		}
		if err := ValidateShelfConditions(row.Conditions); err != nil {
			return err
		}
	}
	return nil
}

func validateShelfParameter(field EntityFieldDefinition, value any) error {
	var values []ShelfParameterValue
	if err := DecodeShelfValue(value, &values); err != nil || values == nil || len(values) > 200 {
		return fmt.Errorf("invalid conditional parameter values for field %s", field.Key)
	}
	field.ProductLineIDs = nil
	for _, v := range values {
		if err := ValidateShelfConditions(v.Conditions); err != nil {
			return err
		}
		if field.Type == "number" {
			if _, ok := v.Value.(map[string]any); ok {
				var bounds struct {
					Min *float64 `json:"min"`
					Max *float64 `json:"max"`
				}
				if err := DecodeShelfValue(v.Value, &bounds); err != nil || bounds.Min == nil || bounds.Max == nil || *bounds.Min > *bounds.Max {
					return fmt.Errorf("invalid numeric range for field %s", field.Key)
				}
				continue
			}
		}
		if err := ValidateFieldAttributes([]EntityFieldDefinition{field}, map[string]any{field.Key: v.Value}); err != nil {
			return err
		}
	}
	return nil
}
