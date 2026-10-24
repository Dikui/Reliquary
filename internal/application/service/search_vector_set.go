package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reliquary/internal/types"
	"runtime/debug"
	"strings"

	apperrors "reliquary/internal/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type searchVectorSet struct {
	Selector    string `json:"selector"`
	ModelID     string `json:"model_id"`
	ProfileHash string `json:"profile_hash"`
	Dimension   int    `json:"dimension"`
	RunID       string `json:"run_id,omitempty"`
}

// Lock selected rebuild metadata alongside the space lock. SwitchCandidate and
// DeleteOldVectors take exclusive locks on these rows before changing a set.
func resolveSearchVectorSet(ctx context.Context, db *gorm.DB, spaceID uint64, selector string, lock bool) (searchVectorSet, error) {
	result := searchVectorSet{Selector: selector}
	if selector == "" || selector == "active" {
		result.Selector = "active"
		return result, nil
	}
	parts := strings.SplitN(selector, ":", 2)
	if len(parts) != 2 || parts[1] == "" || (parts[0] != "candidate" && parts[0] != "archive") {
		return result, apperrors.NewBadRequestError("vector_set must be active, candidate:<run_id>, or archive:<run_id>")
	}
	var run types.EmbeddingRebuildRun
	q := db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "SHARE"})
	}
	if err := q.Where("id=? AND space_id=?", parts[1], spaceID).First(&run).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return result, apperrors.NewBadRequestError("unknown vector set")
		}
		return result, err
	}
	if !run.Candidate || run.Status != "completed" || run.CompletedCount != run.TotalCount || run.FailedCount != 0 {
		return result, apperrors.NewBadRequestError("vector set is not complete")
	}
	result.RunID = run.ID
	if parts[0] == "candidate" {
		if run.SwitchedAt != nil {
			return result, apperrors.NewBadRequestError("candidate already switched; use active")
		}
		result.ModelID = run.ModelID
		result.Dimension = run.Dimension
		result.ProfileHash = run.ProfileHash
	} else {
		var count int64
		if err := db.WithContext(ctx).Table("embedding_vector_archives").Where("run_id=? AND space_id=?", run.ID, spaceID).Count(&count).Error; err != nil {
			return result, err
		}
		if count != 1 {
			return result, apperrors.NewBadRequestError("archived vectors no longer available")
		}
		// Archives retain each old row's model ID and dimension. Require one profile
		// across all channels, rather than silently mixing incompatible spaces.
		var profiles []struct {
			ModelID   string
			Dimension int
		}
		err := db.WithContext(ctx).Raw(`SELECT DISTINCT x.model_id,x.dimension FROM embedding_vector_archives a,
   LATERAL jsonb_to_recordset(COALESCE(a.payload->'article_embeddings','[]') || COALESCE(a.payload->'embeddings','[]') || COALESCE(a.payload->'image_embeddings','[]')) x(model_id text,dimension integer)
   WHERE a.run_id=? AND a.space_id=?`, run.ID, spaceID).Scan(&profiles).Error
		if err != nil {
			return result, err
		}
		if len(profiles) != 1 || profiles[0].ModelID == "" {
			return result, apperrors.NewBadRequestError("archive requires exactly one explicit embedding profile")
		}
		result.ModelID = profiles[0].ModelID
		result.Dimension = profiles[0].Dimension
	}
	var model types.Model
	if err := db.WithContext(ctx).Where("id=? AND (space_id=? OR is_builtin=true)", result.ModelID, spaceID).First(&model).Error; err != nil {
		return result, apperrors.NewBadRequestError("vector set model no longer available")
	}
	hash := types.EmbeddingProfileHash(model.ID, model.Parameters.EmbeddingParameters.Dimension, &model)
	if result.Dimension != model.Parameters.EmbeddingParameters.Dimension || (result.ProfileHash != "" && hash != result.ProfileHash) {
		return result, apperrors.NewBadRequestError("vector set model profile changed")
	}
	result.ProfileHash = hash
	return result, nil
}

// vectorRelation returns the same aliases consumed by the ordinary recall
// lanes. Switching a set changes its data source, never the retrieval pipeline.
func vectorRelation(selector, kind, alias string) string {
	if selector == "" || selector == "active" {
		table := map[string]string{"chunk": "embeddings", "article": "article_embeddings", "image": "image_embeddings"}[kind]
		return table + " " + alias
	}
	parts := strings.SplitN(selector, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	run := strings.ReplaceAll(parts[1], "'", "''")
	if parts[0] == "candidate" {
		condition := map[string]string{"chunk": "kind LIKE '%_chunk'", "article": "kind='article'", "image": "kind='image'"}[kind]
		return fmt.Sprintf("(SELECT v.*,r.space_id,v.source_id AS chunk_id,true AS is_enabled FROM embedding_rebuild_vectors v JOIN embedding_rebuild_runs r ON r.id=v.run_id WHERE v.run_id='%s' AND %s) %s", run, condition, alias)
	}
	table := map[string]string{"chunk": "embeddings", "article": "article_embeddings", "image": "image_embeddings"}[kind]
	return fmt.Sprintf("(SELECT x.* FROM embedding_vector_archives ar, LATERAL jsonb_to_recordset(ar.payload->'%s') x(entry_id text,space_id bigint,library_id text,chunk_id text,model_id text,dimension integer,embedding halfvec,is_enabled boolean,attachment_path text) WHERE ar.run_id='%s') %s", table, run, alias)
}

func searchSnapshot(config *types.RetrievalConfig, libraries []types.Library, set searchVectorSet, request types.EntrySearchRequest, rerankID string) json.RawMessage {
	vectorWeight, keywordWeight := config.GetEffectiveRRFWeights()
	libs := []map[string]any{}
	for _, lib := range libraries {
		libs = append(libs, map[string]any{"id": lib.ID, "chunking": lib.ChunkingConfig, "indexing": lib.IndexingStrategy})
	}
	revision := "unknown"
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
			}
		}
	}
	data, _ := json.Marshal(map[string]any{
		"version": 1, "code_revision": revision, "vector_set": set, "rerank_model_id": rerankID, "libraries": libs,
		"request": request, "retrieval_config": config, "effective": map[string]any{
			"embedding_top_k": config.GetEffectiveEmbeddingTopK(), "vector_threshold": config.GetEffectiveVectorThreshold(),
			"keyword_threshold": config.GetEffectiveKeywordThreshold(), "rrf_k": config.GetEffectiveRRFK(),
			"vector_weight": vectorWeight, "keyword_weight": keywordWeight, "image_weight": config.GetEffectiveRRFImageWeight(),
		},
	})
	return data
}
