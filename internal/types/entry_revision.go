package types

import (
	"bytes"
	"encoding/json"
)

// SameSourceVersion checks whether a processing snapshot can still write derived results.
// An unchanged ingest may advance the external revision without starting a new
// processing generation; that existing operation must remain usable.
func (e *Entry) SameSourceVersion(other *Entry) bool {
	if e.SourceBodyHash != other.SourceBodyHash || (e.SourceID != "" && e.SourceID != other.SourceID) {
		return false
	}
	if e.SourceID == other.SourceID && ((e.SourceRevision == nil && other.SourceRevision == nil) ||
		(e.SourceRevision != nil && other.SourceRevision != nil && *e.SourceRevision == *other.SourceRevision)) {
		return true
	}
	var before, after map[string]json.RawMessage
	if json.Unmarshal(e.Metadata, &before) != nil || json.Unmarshal(other.Metadata, &after) != nil {
		return false
	}
	return bytes.Equal(before["segment_processing_generation"], after["segment_processing_generation"])
}
