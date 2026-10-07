package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"reliquary/internal/types"
)

type workflowReportEvidence struct {
	CitationID  string `json:"citation_id"`
	EvidenceID  string `json:"evidence_id"`
	Title       string `json:"title"`
	PublishedAt string `json:"published_at"`
	URL         string `json:"url"`
	Quote       string `json:"quote"`
	Role        string `json:"role"`
}

type workflowReportStructure struct {
	StepID   string                  `json:"step_id"`
	Entities []types.KnowledgeEntity `json:"entities"`
	Facts    []map[string]any        `json:"facts"`
	Events   []map[string]any        `json:"events"`
}

type workflowReportMaterial struct {
	ExcludedMissingPublishedAtCount int                       `json:"excluded_missing_published_at_count"`
	TimeFrom                        *time.Time                `json:"time_from,omitempty"`
	TimeTo                          *time.Time                `json:"time_to,omitempty"`
	Evidence                        []workflowReportEvidence  `json:"evidence"`
	Structured                      []workflowReportStructure `json:"structured"`
	Coverage                        string                    `json:"coverage"`
}

func workflowEvidenceRole(citation types.KnowledgeWorkflowCitation, from, to *time.Time, loc *time.Location) string {
	if from == nil && to == nil {
		return "current_period"
	}
	published, err := parseWorkflowTimeInLocation(citation.PublishedAt, loc, false)
	if err != nil || published == nil {
		return "unknown_date"
	}
	if from != nil && published.Before(*from) {
		return "background"
	}
	if to != nil && published.After(*to) {
		return "outside_period"
	}
	return "current_period"
}

// Keep the two input contracts explicit. Only evidence with assigned citations
// may enter structured output; raw bodies and arbitrary step data are excluded.
func buildWorkflowReportMaterial(outputs map[string]workflowStepOutput, from, to *time.Time, loc *time.Location) (workflowReportMaterial, []types.KnowledgeWorkflowCitation) {
	material := workflowReportMaterial{TimeFrom: from, TimeTo: to, Evidence: []workflowReportEvidence{}, Structured: []workflowReportStructure{}, Coverage: "Selected workflow results; pagination and extraction gaps may limit coverage. Not an exhaustive census."}
	all := gatherWorkflowCitations(outputs)
	missing := map[string]bool{}
	if from != nil || to != nil {
		for _, citation := range all {
			if workflowPublicationMissing(citation, loc) {
				missing[workflowMissingSourceID(citation)] = true
			}
		}
	}
	citations := make([]types.KnowledgeWorkflowCitation, 0, min(30, len(all)))
	// Prioritize this period before background so a busy history cannot displace
	// current evidence from the fixed report budget.
	for _, role := range []string{"current_period", "background", "unknown_date"} {
		for _, citation := range all {
			if (from != nil || to != nil) && workflowPublicationMissing(citation, loc) {
				continue
			}
			if len(citations) == 30 {
				break
			}
			if strings.TrimSpace(citation.Quote) == "" || workflowEvidenceRole(citation, from, to, loc) != role {
				continue
			}
			citation.ID = fmt.Sprintf("C%d", len(citations)+1)
			citation.Role = role
			citation.Quote = truncateWorkflowText(citation.Quote, 1200)
			citations = append(citations, citation)
			material.Evidence = append(material.Evidence, workflowReportEvidence{CitationID: citation.ID, EvidenceID: citation.EvidenceID, Title: citation.Title, PublishedAt: citation.PublishedAt, URL: citation.URL, Quote: citation.Quote, Role: role})
		}
	}
	byEvidence := map[string]string{}
	for _, citation := range citations {
		if citation.EvidenceID != "" {
			byEvidence[citation.EvidenceID] = citation.ID
		}
	}
	stepIDs := make([]string, 0, len(outputs))
	for id := range outputs {
		stepIDs = append(stepIDs, id)
	}
	sort.Strings(stepIDs)
	remainingFacts, remainingEvents := 100, 50
	for _, id := range stepIDs {
		// Persisted runs restore Data as a JSON map; decoding also handles a fresh
		// *KnowledgeContextResult without relying on Go's concrete runtime type.
		encoded, err := json.Marshal(outputs[id].Data)
		if err != nil {
			continue
		}
		var contextResult types.KnowledgeContextResult
		if json.Unmarshal(encoded, &contextResult) != nil {
			continue
		}
		for _, sourceID := range contextResult.PublishedAtMissingSourceIDs {
			missing[sourceID] = true
		}
		entry := workflowReportStructure{StepID: id, Entities: []types.KnowledgeEntity{}, Facts: []map[string]any{}, Events: []map[string]any{}}
		usedEntities := map[string]bool{}
		for _, fact := range contextResult.Facts {
			citationID := byEvidence[fact.EvidenceID]
			if citationID == "" || remainingFacts == 0 {
				continue
			}
			if len(fact.Value) > 4000 {
				continue
			}
			fact.Quote = truncateWorkflowText(fact.Quote, 1200)
			value, _ := json.Marshal(fact)
			var record map[string]any
			if json.Unmarshal(value, &record) != nil {
				continue
			}
			record["citation_id"] = citationID
			entry.Facts = append(entry.Facts, record)
			usedEntities[fact.SubjectEntityID], usedEntities[fact.ObjectEntityID] = true, true
			remainingFacts--
		}
		for _, event := range contextResult.Events {
			refs := []string{}
			for _, evidenceID := range event.EvidenceIDs {
				if citationID := byEvidence[evidenceID]; citationID != "" {
					refs = append(refs, citationID)
				}
			}
			if len(refs) == 0 || remainingEvents == 0 {
				continue
			}
			event.Description = truncateWorkflowText(event.Description, 1200)
			value, _ := json.Marshal(event)
			var record map[string]any
			if json.Unmarshal(value, &record) != nil {
				continue
			}
			record["citation_ids"] = uniqueWorkflowStrings(refs)
			entry.Events = append(entry.Events, record)
			for _, entityID := range event.EntityIDs {
				usedEntities[entityID] = true
			}
			remainingEvents--
		}
		for _, entity := range contextResult.Entities {
			if usedEntities[entity.ID] {
				entry.Entities = append(entry.Entities, entity)
			}
		}
		if len(entry.Facts) > 0 || len(entry.Events) > 0 {
			material.Structured = append(material.Structured, entry)
		}
	}
	material.ExcludedMissingPublishedAtCount = len(missing)
	return material, citations
}

func workflowMissingSourceID(citation types.KnowledgeWorkflowCitation) string {
	if citation.KnowledgeID != "" {
		return "knowledge:" + citation.KnowledgeID
	}
	if citation.EvidenceID != "" {
		return "evidence:" + citation.EvidenceID
	}
	return "citation:" + citation.Kind + ":" + citation.URL + ":" + citation.ID
}

func workflowPublicationMissing(citation types.KnowledgeWorkflowCitation, loc *time.Location) bool {
	value, err := parseWorkflowTimeInLocation(citation.PublishedAt, loc, false)
	return err != nil || value == nil
}
