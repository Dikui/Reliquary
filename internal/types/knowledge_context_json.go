package types

import (
	"bytes"
	"encoding/json"
)

// Knowledge context response lists always encode as arrays, including records
// returned directly by an endpoint. Copies keep serialization out of storage.
func contextResponseList[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func (r KnowledgeContextResult) MarshalJSON() ([]byte, error) {
	type response KnowledgeContextResult
	out := response(r)
	out.Entities = contextResponseList(out.Entities)
	out.Facts = contextResponseList(out.Facts)
	out.Events = contextResponseList(out.Events)
	out.Evidence = contextResponseList(out.Evidence)
	out.Attention = contextResponseList(out.Attention)
	out.PublishedAtMissingSourceIDs = contextResponseList(out.PublishedAtMissingSourceIDs)
	return json.Marshal(out)
}

func (e KnowledgeEntity) MarshalJSON() ([]byte, error) {
	type response KnowledgeEntity
	out := response(e)
	out.Aliases = contextResponseList(out.Aliases)
	return json.Marshal(out)
}

func (e KnowledgeEvent) MarshalJSON() ([]byte, error) {
	type response KnowledgeEvent
	out := response(e)
	out.Sources = contextResponseList(out.Sources)
	out.EntityIDs = contextResponseList(out.EntityIDs)
	out.EvidenceIDs = contextResponseList(out.EvidenceIDs)
	out.Relations = contextResponseList(out.Relations)
	return json.Marshal(out)
}

func (e KnowledgeEvidence) MarshalJSON() ([]byte, error) {
	type response KnowledgeEvidence
	out := response(e)
	out.ImageRefs = contextResponseList(out.ImageRefs)
	if len(bytes.TrimSpace(out.ModelCalls)) == 0 || bytes.Equal(bytes.TrimSpace(out.ModelCalls), []byte("null")) {
		out.ModelCalls = JSON("[]")
	}
	return json.Marshal(out)
}
