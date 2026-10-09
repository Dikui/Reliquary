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

type workflowReportEventCoverage struct {
	StepID                   string `json:"step_id"`
	TemplateOnlyMessageCount int    `json:"template_only_message_count"`
}

type workflowReportMaterial struct {
	ExcludedMissingPublishedAtCount int                           `json:"excluded_missing_published_at_count"`
	TimeFrom                        *time.Time                    `json:"time_from,omitempty"`
	TimeTo                          *time.Time                    `json:"time_to,omitempty"`
	Evidence                        []workflowReportEvidence      `json:"evidence"`
	Structured                      []workflowReportStructure     `json:"structured"`
	Coverage                        string                        `json:"coverage"`
	EventsCoverageNotice            string                        `json:"events_coverage_notice"`
	EventCoverage                   []workflowReportEventCoverage `json:"event_coverage"`
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
	material := workflowReportMaterial{TimeFrom: from, TimeTo: to, Evidence: []workflowReportEvidence{}, Structured: []workflowReportStructure{}, Coverage: "Selected workflow results; pagination and extraction gaps may limit coverage. Not an exhaustive census.", EventsCoverageNotice: contextEventsCoverageNotice, EventCoverage: []workflowReportEventCoverage{}}
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
		if contextResult.Entities != nil || contextResult.Facts != nil || contextResult.Events != nil || contextResult.Evidence != nil {
			material.EventCoverage = append(material.EventCoverage, workflowReportEventCoverage{StepID: id, TemplateOnlyMessageCount: contextResult.TemplateOnlyMessageCount})
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

// Persist the coverage statement independently of model compliance. Counts are
// per query because overlapping query scopes must not be added together.
func workflowReportCoverageMarkdown(material workflowReportMaterial) string {
	var text strings.Builder
	text.WriteString("\n\n事件与基于事件的统计只包含收录时命中关注范围的消息。历史消息可通过手动补抽补齐；该说明不保证时间线完整。\n")
	for _, coverage := range material.EventCoverage {
		fmt.Fprintf(&text, "\n相关的最新证据版本仍为 template_only 的消息数：%d（查询步骤 %s）。\n", coverage.TemplateOnlyMessageCount, coverage.StepID)
	}
	if len(material.EventCoverage) > 1 {
		text.WriteString("\n各查询步骤的消息可能重叠，以上计数不能相加。\n")
	}
	return text.String()
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
