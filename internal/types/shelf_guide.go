package types

// ShelfGuideValues contains only this library's active identities and settings.
type ShelfGuideValues struct {
	Kinds            []EntityKind                           `json:"kinds"`
	ProductLines     []Entity                               `json:"product_lines"`
	Regions          []ShelfOption                          `json:"regions"`
	Markets          []ShelfOption                          `json:"markets"`
	Stages           []ShelfOption                          `json:"stages"`
	ModuleTypes      []ShelfOption                          `json:"module_types"`
	Standards        []ShelfStandard                        `json:"standards"`
	SaleStatuses     []string                               `json:"sale_statuses"`
	RequirementLists []ShelfGuideRequirementList            `json:"requirement_lists"`
	Requirements     *ShelfGuidePage[ShelfGuideRequirement] `json:"requirements,omitempty"`
	Fields           []EntityFieldDefinition                `json:"fields"`
}

// ShelfGuidePage bounds each list after counting all matching items.
type ShelfGuidePage[T any] struct {
	Items     []T   `json:"items"`
	Total     int64 `json:"total"`
	Truncated bool  `json:"truncated"`
}

func (p *ShelfGuidePage[T]) Add(item T) {
	p.Total++
	if len(p.Items) < 100 {
		p.Items = append(p.Items, item)
	} else {
		p.Truncated = true
	}
}

type ShelfGuideProductLine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ShelfGuideRequirementList struct {
	Name         string                  `json:"name"`
	Total        int64                   `json:"total"`
	ProductLines []ShelfGuideProductLine `json:"product_lines"`
}

// ShelfGuideRequirement keeps only the fields needed to classify a file.
type ShelfGuideRequirement struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ListName      string `json:"list_name"`
	NameZH        string `json:"name_zh,omitempty"`
	NameEN        string `json:"name_en,omitempty"`
	DraftDueStage string `json:"draft_due_stage,omitempty"`
	FinalDueStage string `json:"final_due_stage"`
}
