package types

import "time"

// ShelfImportPlan is confirmed configuration, scoped to one file kind in a library.
type ShelfImportPlan struct {
	SpaceID    uint64                `json:"space_id" gorm:"primaryKey"`
	LibraryID  string                `json:"library_id" gorm:"primaryKey"`
	Key        string                `json:"key" gorm:"primaryKey"`
	Name       string                `json:"name"`
	FileKind   string                `json:"file_kind"`
	Revision   int64                 `json:"revision"`
	Definition ShelfImportDefinition `json:"definition" gorm:"serializer:json;type:jsonb"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

type ShelfImportPlanInput struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	FileKind string `json:"file_kind"`
	Revision int64  `json:"revision"`
	ShelfImportDefinition
}

type ShelfImportDefinition struct {
	Sheets          []ShelfImportSheet             `json:"sheets"`
	ModelSeparators []string                       `json:"model_separators,omitempty"`
	MissingValues   []string                       `json:"missing_values,omitempty"`
	StatusValues    map[string]ShelfImportStatus   `json:"status_values,omitempty"`
	FillMeanings    map[string]string              `json:"fill_meanings,omitempty"`
	Deadlines       map[string]ShelfImportDeadline `json:"deadlines,omitempty"`
}

type ShelfImportDeadline struct {
	DraftStage string `json:"draft_stage,omitempty"`
	FinalStage string `json:"final_stage"`
}

type ShelfImportSheet struct {
	Worksheet     string              `json:"worksheet"`
	Ignore        bool                `json:"ignore,omitempty"`
	ProductLineID string              `json:"product_line_id,omitempty"`
	HeaderRows    int                 `json:"header_rows"`
	Columns       []ShelfImportColumn `json:"columns,omitempty"`
	OwnerKind     string              `json:"owner_kind,omitempty"`
	ModuleType    string              `json:"module_type,omitempty"`
	Progress      string              `json:"progress,omitempty"`
	CheckValues   map[string]bool     `json:"check_values,omitempty"`
}

type ShelfImportColumn struct {
	Column string `json:"column"`
	Field  string `json:"field"`
	Key    string `json:"key,omitempty"`
}

type ShelfImportStatus struct {
	Status string `json:"status"`
	Notes  string `json:"notes,omitempty"`
}

type ShelfImportLocation struct {
	Worksheet string            `json:"worksheet"`
	Cells     []string          `json:"cells"`
	Values    map[string]string `json:"values,omitempty"`
}

// ShelfImportColumnFeedback describes source content excluded by the plan.
type ShelfImportColumnFeedback struct {
	Worksheet    string   `json:"worksheet"`
	Column       string   `json:"column"`
	Header       string   `json:"header"`
	NonemptyRows int      `json:"nonempty_rows"`
	Examples     []string `json:"examples"`
}

// The caller identifies the proposed write; the model only fills the requested
// values. Models use ParentID instead of Operation to name an existing product.
type ShelfImportTextInput struct {
	Kind           string               `json:"kind"`
	Text           string               `json:"text"`
	Operation      ShelfOperation       `json:"operation"`
	ParentID       string               `json:"parent_id,omitempty"`
	Source         OperationSource      `json:"source"`
	ImportLocation *ShelfImportLocation `json:"import_location,omitempty"`
}
