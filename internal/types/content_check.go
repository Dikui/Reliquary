package types

import "time"

const TypeContentCheck = "content:check"

type ContentCheckSettings struct {
	SpaceID   uint64     `json:"-" gorm:"primaryKey"`
	Enabled   bool       `json:"enabled"`
	ModelID   string     `json:"model_id"`
	Weekday   int        `json:"weekday"`
	Time      string     `json:"time"`
	NextRunAt *time.Time `json:"next_run_at"`
	ScanAfter time.Time  `json:"-"`
}
type ContentCheckRun struct {
	ID         string               `json:"id" gorm:"primaryKey"`
	SpaceID    uint64               `json:"-"`
	ModelID    string               `json:"model_id"`
	PreviousID string               `json:"previous_id"`
	Status     string               `json:"status"`
	WindowFrom time.Time            `json:"window_from"`
	WindowTo   time.Time            `json:"window_to"`
	Targets    []string             `json:"targets" gorm:"serializer:json;type:jsonb"`
	Total      int                  `json:"total" gorm:"-"`
	Completed  int                  `json:"completed"`
	Results    []ContentCheckResult `json:"results" gorm:"serializer:json;type:jsonb"`
	Error      string               `json:"error"`
	CreatedAt  time.Time            `json:"created_at"`
	FinishedAt *time.Time           `json:"finished_at"`
}
type ContentCheckResult struct {
	EntryID  string                `json:"entry_id"`
	Findings []ContentCheckFinding `json:"findings"`
}
type ContentCheckFinding struct {
	Kind        string                 `json:"kind"`
	Description string                 `json:"description"`
	Evidence    []ContentCheckEvidence `json:"evidence"`
}
type ContentCheckEvidence struct {
	EntryID       string         `json:"entry_id"`
	Title         string         `json:"title"`
	LibraryID     string         `json:"library_id"`
	Quote         string         `json:"quote"`
	CurrentResult string         `json:"current_result,omitempty"`
	Location      SourceLocation `json:"location"`
}
type ContentCheckPage struct {
	Items []ContentCheckRun `json:"items"`
	Total int64             `json:"total"`
}
