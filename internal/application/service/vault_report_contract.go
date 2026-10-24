package service

import (
	"encoding/json"
	"fmt"
	"reliquary/internal/types"
	"sort"
	"strings"
	"unicode/utf8"

	werrors "reliquary/internal/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReportInput supplies fields to the shared generated-material persistence path.
type ReportInput struct {
	Library         string   `json:"library,omitempty"`
	Title           string   `json:"title"`
	Markdown        string   `json:"markdown"`
	RelatedEntryIDs []string `json:"related_entry_ids,omitempty"`
	RelatedEntities []string `json:"related_entities,omitempty"`
	RequestID       string   `json:"request_id,omitempty"`
}

type ReportFieldError struct {
	Field      string                  `json:"field"`
	Message    string                  `json:"message"`
	Candidates []ReportEntityReference `json:"candidates,omitempty"`
}

type ReportEntityReference struct {
	ID         string `json:"id" yaml:"id"`
	Name       string `json:"name" yaml:"name"`
	EntityType string `json:"entity_type" yaml:"entity_type"`
}

type reportMetadata struct {
	RelatedEntryIDs []string                `json:"related_entry_ids"`
	RelatedEntities []ReportEntityReference `json:"related_entities,omitempty"`
	Fingerprint     string                  `json:"report_fingerprint,omitempty"`
	RequestID       string                  `json:"request_id,omitempty"`
}

func reportValidationError(issues []ReportFieldError) error {
	data, _ := json.Marshal(issues)
	return werrors.NewBadRequestError("invalid report fields: " + string(data)).WithDetails(map[string]any{"fields": issues})
}

func normalizedReportIDs(ids []string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, strings.TrimSpace(id))
	}
	sort.Strings(result)
	return uniqueVaultStrings(result)
}

func reportInputFingerprint(input ReportInput, metadata reportMetadata) string {
	entities := make([]string, 0, len(metadata.RelatedEntities))
	for _, entity := range metadata.RelatedEntities {
		entities = append(entities, entity.ID)
	}
	data, _ := json.Marshal(struct {
		Title    string   `json:"title"`
		Markdown string   `json:"markdown"`
		Articles []string `json:"articles"`
		Entities []string `json:"entities"`
	}{
		strings.TrimSpace(input.Title),
		strings.ReplaceAll(strings.ReplaceAll(input.Markdown,
			"\r\n",
			"\n"),
			"\r",
			"\n"),
		normalizedReportIDs(metadata.RelatedEntryIDs),
		normalizedReportIDs(entities),
	})
	return vaultContentHash(string(data))
}

func validateReport(tx *gorm.DB, library *types.Library, input ReportInput) (reportMetadata, error) {
	metadata := reportMetadata{RelatedEntryIDs: normalizedReportIDs(input.RelatedEntryIDs), RequestID: strings.TrimSpace(input.RequestID)}
	var issues []ReportFieldError
	add := func(field, message string) { issues = append(issues, ReportFieldError{Field: field, Message: message}) }
	if title := strings.TrimSpace(input.Title); title == "" || !utf8.ValidString(title) || strings.ContainsRune(title, 0) || utf8.RuneCountInString(title) > 255 {
		add("title", "required valid UTF-8 string without NUL, 1 to 255 characters")
	}
	if strings.TrimSpace(input.Markdown) == "" || !utf8.ValidString(input.Markdown) || strings.ContainsRune(input.Markdown, 0) || utf8.RuneCountInString(input.Markdown) > types.MaxIngestContentRunes {
		add("markdown", "required valid UTF-8 Markdown without NUL, at most 5000000 characters")
	}
	if input.Library != "" && input.Library != library.ID && input.Library != library.Directory {
		add("library", "only the AI reports system library is accepted")
	}
	if metadata.RequestID != "" && (!utf8.ValidString(metadata.RequestID) || strings.ContainsRune(metadata.RequestID, 0) || utf8.RuneCountInString(metadata.RequestID) > 255) {
		add("request_id", "at most 255 valid UTF-8 characters without NUL")
	}
	for i, ref := range input.RelatedEntryIDs {
		if !utf8.ValidString(ref) || strings.ContainsRune(ref, 0) {
			add(fmt.Sprintf("related_entry_ids[%d]", i), "reference must be valid UTF-8 without NUL")
			continue
		}
		var article types.Entry
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("space_id=? AND id=?", library.SpaceID, strings.TrimSpace(ref)).First(&article).Error
		if err == gorm.ErrRecordNotFound {
			add(fmt.Sprintf("related_entry_ids[%d]", i), "article does not exist or is deleted: "+ref)
		} else if err != nil {
			return metadata, err
		}
	}
	resolved := map[string]ReportEntityReference{}
	for i, ref := range input.RelatedEntities {
		if !utf8.ValidString(ref) || strings.ContainsRune(ref, 0) {
			add(fmt.Sprintf("related_entities[%d]", i), "reference must be valid UTF-8 without NUL")
			continue
		}
		ref = strings.TrimSpace(ref)
		var entities []types.Entity
		// IDs take precedence over canonical names and aliases. Resolution never embeds names.
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("space_id=? AND id=? AND merged_into_id=''", library.SpaceID, ref).Find(&entities).Error; err != nil {
			return metadata, err
		}
		if len(entities) == 0 && ref != "" {
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("space_id=? AND merged_into_id='' AND (normalized_name=? OR id IN (SELECT entity_id FROM entity_aliases WHERE space_id=? AND normalized_alias=?))", library.SpaceID, normalizeEntityName(ref), library.SpaceID, normalizeEntityName(ref)).Order("id").Find(&entities).Error; err != nil {
				return metadata, err
			}
		}
		field := fmt.Sprintf("related_entities[%d]", i)
		if len(entities) == 0 {
			add(field, "entity does not exist: "+ref)
		} else if len(entities) > 1 {
			candidates := []ReportEntityReference{}
			for _, entity := range entities {
				candidates = append(candidates, ReportEntityReference{entity.ID, entity.CanonicalName, entity.EntityType})
			}
			issues = append(issues, ReportFieldError{Field: field, Message: "ambiguous entity; use an ID", Candidates: candidates})
		} else {
			entity := entities[0]
			resolved[entity.ID] = ReportEntityReference{entity.ID, entity.CanonicalName, entity.EntityType}
		}
	}
	ids := make([]string, 0, len(resolved))
	for id := range resolved {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		metadata.RelatedEntities = append(metadata.RelatedEntities, resolved[id])
	}
	if len(issues) > 0 {
		return metadata, reportValidationError(issues)
	}
	metadata.Fingerprint = reportInputFingerprint(input, metadata)
	return metadata, nil
}

func findExistingReport(tx *gorm.DB, library *types.Library, input ReportInput, metadata reportMetadata) (*types.Entry, error) {
	query := tx.Where("space_id=? AND library_id=? AND source_kind='report'", library.SpaceID, library.ID)
	var entry types.Entry
	if metadata.RequestID != "" {
		err := query.Where("metadata->>'request_id'=?", metadata.RequestID).Order("created_at,id").First(&entry).Error
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return &entry, err
	}
	err := query.Where("metadata->>'report_fingerprint'=?", metadata.Fingerprint).Order("created_at,id").First(&entry).Error
	if err == nil {
		return &entry, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	// Historical reports have no interface key. Compare canonical references
	// without changing their rows or merging previously independent reports.
	var legacy []types.Entry
	if err := tx.Where("space_id=? AND library_id=? AND source_kind='report' AND title=? AND COALESCE(metadata->>'report_fingerprint','')=''", library.SpaceID, library.ID, strings.TrimSpace(input.Title)).Order("created_at,id").Find(&legacy).Error; err != nil {
		return nil, err
	}
	for _, entry := range legacy {
		var old reportMetadata
		if json.Unmarshal(entry.Metadata, &old) != nil {
			continue
		}
		fingerprint := reportInputFingerprint(ReportInput{Title: entry.Title, Markdown: entry.VaultBody}, old)
		if fingerprint == metadata.Fingerprint {
			return &entry, nil
		}
	}
	return nil, nil
}
