package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"gorm.io/gorm"
)

type VectorProfile struct {
	Table         string `json:"table"`
	ModelID       string `json:"model_id"`
	Dimension     int    `json:"dimension"`
	ProfileHash   string `json:"profile_hash"`
	Count         int    `json:"count"`
	SourcesSHA256 string `json:"sources_sha256"`
}

// ModelProfile hashes embedding semantics while excluding credentials,
// maintenance reminders, display fields, connection status and request limits.
func ModelProfile(model map[string]any) string {
	parameters, _ := model["parameters"].(map[string]any)
	clean := map[string]any{}
	for _, key := range []string{"base_url", "interface_type", "provider", "embedding_parameters", "extra_config"} {
		if value, ok := parameters[key]; ok {
			clean[key] = value
		}
	}
	if extra, ok := clean["extra_config"].(map[string]any); ok {
		copy := map[string]any{}
		for k, v := range extra {
			if k != "maintenance_required_fields" && k != "maintenance_original_status" && k != "maintenance_original_updated_at" {
				copy[k] = v
			}
		}
		if len(copy) == 0 {
			clean["extra_config"] = nil
		} else {
			clean["extra_config"] = copy
		}
	} else {
		clean["extra_config"] = nil
	}
	data, _ := json.Marshal(map[string]any{"id": model["id"], "name": model["name"], "source": model["source"], "type": model["type"], "parameters": clean})
	return sum(data)
}

// CheckVectorDimensions validates declared dimensions against actual pgvector
// values and the stored model configuration, including after a model change.
func CheckVectorDimensions(ctx context.Context, db *gorm.DB) ([]string, error) {
	queries := map[string]string{
		"article_embeddings": `SELECT e.entry_id AS identity,e.model_id,e.dimension,vector_dims(e.embedding) AS actual FROM article_embeddings e WHERE e.embedding IS NOT NULL`,
		"image_embeddings":   `SELECT e.entry_id||'/'||e.attachment_path AS identity,e.model_id,e.dimension,vector_dims(e.embedding) AS actual FROM image_embeddings e WHERE e.embedding IS NOT NULL`,
		"embeddings":         `SELECT e.id AS identity,e.model_id,e.dimension,vector_dims(e.embedding) AS actual FROM embeddings e WHERE e.embedding IS NOT NULL AND e.is_enabled`,
	}
	differences := []string{}
	dimensions := map[string]map[int]bool{}
	for table, query := range queries {
		var values []struct {
			Identity   string
			ModelID    string
			Dimension  int
			Actual     int
			Configured int
			Present    bool
		}
		err := db.WithContext(ctx).Raw(`SELECT v.*,m.id IS NOT NULL AND m.type='Embedding' AS present,COALESCE(NULLIF((m.parameters #>> '{embedding_parameters,dimension}')::int,0),NULLIF((m.parameters->>'dimension')::int,0),0) AS configured FROM (` + query + `) v LEFT JOIN models m ON m.id=v.model_id`).Scan(&values).Error
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			if !value.Present || value.Actual <= 0 || value.Dimension != value.Actual || (value.Configured > 0 && value.Configured != value.Actual) {
				differences = append(differences, fmt.Sprintf("%s row %s has inconsistent vector model or dimensions", table, value.Identity))
			}
			if dimensions[value.ModelID] == nil {
				dimensions[value.ModelID] = map[int]bool{}
			}
			dimensions[value.ModelID][value.Actual] = true
		}
	}
	for _, values := range dimensions {
		if len(values) > 1 {
			differences = append(differences, "one embedding model has vectors with multiple dimensions")
		}
	}
	sort.Strings(differences)
	return differences, nil
}

func VectorProfiles(ctx context.Context, db *gorm.DB) ([]VectorProfile, error) {
	models, err := Models(ctx, db)
	if err != nil {
		return nil, err
	}
	profiles := map[string]string{}
	for _, model := range models {
		profiles[fmt.Sprint(model["id"])] = ModelProfile(model)
	}
	queries := map[string]string{
		"article_embeddings": `SELECT model_id,dimension,jsonb_build_array(space_id,library_id,entry_id,content_hash)::text AS source FROM public.article_embeddings`,
		"image_embeddings":   `SELECT model_id,dimension,jsonb_build_array(space_id,library_id,entry_id,attachment_path,content_hash)::text AS source FROM public.image_embeddings`,
		"embeddings":         `SELECT e.model_id,e.dimension,jsonb_build_array(e.library_id,e.entry_id,e.source_type,c.chunk_index,c.content_hash)::text AS source FROM public.embeddings e LEFT JOIN public.chunks c ON c.id=e.chunk_id WHERE e.embedding IS NOT NULL AND e.is_enabled`,
	}
	type source struct {
		ModelID   string
		Dimension int
		Source    string
	}
	buckets := map[string][]string{}
	entries := map[string]VectorProfile{}
	for table, query := range queries {
		var rows []source
		if err := db.WithContext(ctx).Raw(query).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			key := fmt.Sprintf("%s:%s:%d", table, row.ModelID, row.Dimension)
			buckets[key] = append(buckets[key], row.Source)
			entries[key] = VectorProfile{Table: table, ModelID: row.ModelID, Dimension: row.Dimension, ProfileHash: profiles[row.ModelID]}
		}
	}
	keys := []string{}
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := []VectorProfile{}
	for _, key := range keys {
		sources := buckets[key]
		sort.Strings(sources)
		data, _ := json.Marshal(sources)
		entry := entries[key]
		entry.Count = len(sources)
		entry.SourcesSHA256 = sum(data)
		result = append(result, entry)
	}
	return result, nil
}

func CompareVectors(expected, actual *Manifest) []string {
	left, _ := json.Marshal(expected.Vectors)
	right, _ := json.Marshal(actual.Vectors)
	if string(left) == string(right) {
		return nil
	}
	return []string{"vector coverage, dimension or model profile differs"}
}
