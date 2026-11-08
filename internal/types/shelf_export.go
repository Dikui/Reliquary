package types

// ShelfExportRequest selects product lines and reuses the status table filters.
// Export always includes all matching rows, regardless of query pagination.
type ShelfExportRequest struct {
	ProductLineID string            `json:"product_line_id,omitempty"`
	Query         ShelfQueryRequest `json:"query,omitempty"`
}
