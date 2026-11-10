package types

// ShelfGuideValues contains only this library's active identities and settings.
type ShelfGuideValues struct {
	Kinds            []EntityKind            `json:"kinds"`
	ProductLines     []Entity                `json:"product_lines"`
	Regions          []ShelfOption           `json:"regions"`
	Markets          []ShelfOption           `json:"markets"`
	Stages           []ShelfOption           `json:"stages"`
	ModuleTypes      []ShelfOption           `json:"module_types"`
	Standards        []ShelfStandard         `json:"standards"`
	SaleStatuses     []string                `json:"sale_statuses"`
	RequirementLists []string                `json:"requirement_lists"`
	Requirements     []Entity                `json:"requirements"`
	Fields           []EntityFieldDefinition `json:"fields"`
}
