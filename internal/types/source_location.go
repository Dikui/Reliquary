package types

// SourceLocation addresses saved Markdown body Unicode code points in [Start, End).
// Multiple locations partition a chunk across paragraph and heading boundaries.
type SourceLocation struct {
	HeadingPath []string `json:"heading_path"`
	Paragraph   int      `json:"paragraph"`
	Start       int      `json:"start"`
	End         int      `json:"end"`
	BodyHash    string   `json:"body_hash"`
	Mapping     string   `json:"mapping"`
}
