package types

import (
	"fmt"
	"slices"
	"strings"
)

// ShelfRequirement retains the submitted list metadata independently of names
// used to identify the entity. Due stages refer to stable library stage keys.
type ShelfRequirement struct {
	NameZH        string            `json:"name_zh,omitempty"`
	NameEN        string            `json:"name_en,omitempty"`
	FileCategory  string            `json:"file_category,omitempty"`
	StageLabel    string            `json:"stage_label,omitempty"`
	DraftDueStage string            `json:"draft_due_stage,omitempty"`
	FinalDueStage string            `json:"final_due_stage"`
	Markets       map[string]string `json:"markets,omitempty"`
	Owner         string            `json:"owner,omitempty"`
	Priority      string            `json:"priority,omitempty"`
	SourceList    string            `json:"source_list,omitempty"`
	SourceVersion string            `json:"source_version,omitempty"`
}

func ValidateShelfRequirement(value any) error {
	var r ShelfRequirement
	if err := DecodeShelfValue(value, &r); err != nil {
		return fmt.Errorf("invalid document requirement: %w", err)
	}
	if strings.TrimSpace(r.FinalDueStage) == "" {
		return fmt.Errorf("final due stage is required")
	}
	for _, v := range []string{r.NameZH, r.NameEN, r.FileCategory, r.StageLabel, r.Owner, r.Priority, r.SourceList, r.SourceVersion} {
		if len([]rune(v)) > 20000 {
			return fmt.Errorf("document requirement text is too long")
		}
	}
	for _, v := range r.Markets {
		if !slices.Contains([]string{"required", "optional", "not_needed"}, v) {
			return fmt.Errorf("market necessity requires required, optional or not_needed")
		}
	}
	return nil
}

type ShelfChecklistItem struct {
	Ambiguous      bool                  `json:"ambiguous,omitempty"`
	Requirement    Entity                `json:"requirement"`
	Status         string                `json:"status"`
	StatusLabel    string                `json:"status_label"`
	HasFile        bool                  `json:"has_file"`
	Document       *ShelfDocumentVersion `json:"document,omitempty"`
	OwnerID        string                `json:"owner_id,omitempty"`
	Origin         string                `json:"origin,omitempty"`
	OriginLabel    string                `json:"origin_label,omitempty"`
	Conditions     []ShelfCondition      `json:"conditions"`
	PartialOptions bool                  `json:"partial_options"`
}

type ShelfChecklistSummary struct {
	ModelCount     int                          `json:"model_count"`
	ModelsWithGaps int                          `json:"models_with_gaps"`
	Counts         map[string]int               `json:"counts"`
	Models         []ShelfChecklistModelSummary `json:"models"`
}

type ShelfChecklistModelSummary struct {
	ModelID      string         `json:"model_id"`
	CurrentStage string         `json:"current_stage,omitempty"`
	Counts       map[string]int `json:"counts"`
	HasGaps      bool           `json:"has_gaps"`
}
