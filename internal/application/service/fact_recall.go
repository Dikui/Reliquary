package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"sort"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
)

type recalledFact struct {
	types.Fact
	Similarity float64
}

func groupingDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	if factOccurrenceTime(value) == nil {
		return nil
	}
	date, err := time.Parse(time.DateOnly, value[:10])
	if err != nil {
		return nil
	}
	return &date
}

func factRecallText(f types.Fact) string {
	return strings.Join([]string{f.Title, f.Subject, f.Action, f.Object, f.Conditions}, "\n")
}

func cosineSimilarity(a, b []float32) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, fmt.Errorf("fact embedding dimension mismatch")
	}
	var dot, aa, bb float64
	for i, x := range a {
		y := float64(b[i])
		v := float64(x)
		if math.IsNaN(v) || math.IsInf(v, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			return 0, fmt.Errorf("fact embedding contains non-finite values")
		}
		dot += v * y
		aa += v * v
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0, fmt.Errorf("fact embedding has zero length")
	}
	return math.Max(-1, math.Min(1, dot/math.Sqrt(aa*bb))), nil
}

// Hidden facts remain eligible. The occurrence date takes precedence over source dates.
// Cache misses use the current shared embedding model, including after a profile switch.
func (s *entryService) recallFacts(
	ctx context.Context,
	library *types.Library,
	entry *types.Entry,
	text string,
	date *time.Time,
) ([]recalledFact, error) {
	facts := []types.Fact{}
	q := s.db.WithContext(ctx).
		Table("facts f").
		Select("f.*").
		Where("f.space_id=? AND f.merged_into_id=''", entry.SpaceID)
	if date != nil {
		q = q.Where(`COALESCE(substring(f.occurred_at,1,10)::date,
   (SELECT min(NULLIF(substring(e.metadata#>>'{intake,source_info,published_at}',1,10),'')::date)
    FROM entry_facts ef JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id
    WHERE ef.fact_id=f.id AND ef.space_id=f.space_id AND ef.role='source' AND e.deleted_at IS NULL),
   f.recall_date) BETWEEN ?::date AND ?::date`,
			date.AddDate(0, 0, -14).Format(time.DateOnly), date.AddDate(0, 0, 14).Format(time.DateOnly))
	}
	if err := q.Order("f.id").Scan(&facts).Error; err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return []recalledFact{}, nil
	}
	embedder, err := GetTierEmbeddingModel(ctx, s.db, s.modelService, library)
	if err != nil {
		return nil, err
	}
	ctx = types.WithLLMCallMetadata(types.WithModelCallEntry(ctx, entry.ID), "fact_embedding", "")
	vector, err := embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(vector) != embedder.GetDimensions() {
		return nil, fmt.Errorf("fact query embedding dimension mismatch")
	}
	if _, err = cosineSimilarity(vector, vector); err != nil {
		return nil, err
	}
	ids := make([]string, len(facts))
	for i, f := range facts {
		ids[i] = f.ID
	}
	var cached []struct{ FactID, ContentHash, Vector string }
	if err = s.db.WithContext(ctx).Table("fact_embeddings").Select("fact_id,content_hash,embedding::text AS vector").
		Where("space_id=? AND fact_id IN ? AND model_id=? AND dimension=?",
			entry.SpaceID, ids, embedder.GetModelID(), embedder.GetDimensions()).
		Scan(&cached).Error; err != nil {
		return nil, err
	}
	cache := map[string]struct {
		hash   string
		vector []float32
	}{}
	for _, c := range cached {
		var v []float32
		if err = json.Unmarshal([]byte(c.Vector), &v); err != nil {
			return nil, err
		}
		cache[c.FactID] = struct {
			hash   string
			vector []float32
		}{c.ContentHash, v}
	}
	result := []recalledFact{}
	threshold, _ := s.config.GroupingThresholds()
	for _, f := range facts {
		content := factRecallText(f)
		profile := ""
		if profiled, ok := embedder.(embedding.ProfiledEmbedder); ok {
			profile = profiled.GetProfileHash()
		}
		hash := vaultContentHash(content + "\n" + profile)
		c := cache[f.ID]
		if c.hash != hash {
			v, embedErr := embedder.Embed(ctx, content)
			if embedErr != nil {
				return nil, embedErr
			}
			if len(v) != embedder.GetDimensions() {
				return nil, fmt.Errorf("fact embedding dimension mismatch")
			}
			if _, err = cosineSimilarity(v, v); err != nil {
				return nil, err
			}
			if err = s.db.WithContext(ctx).Exec(`INSERT INTO fact_embeddings
    (space_id,fact_id,model_id,dimension,content_hash,embedding)
    VALUES(?,?,?,?,?,?::halfvec) ON CONFLICT(fact_id) DO UPDATE SET
    model_id=EXCLUDED.model_id,dimension=EXCLUDED.dimension,
    content_hash=EXCLUDED.content_hash,embedding=EXCLUDED.embedding`,
				entry.SpaceID, f.ID, embedder.GetModelID(), len(v), hash, pgvector.NewHalfVector(v)).Error; err != nil {
				return nil, err
			}
			c.vector = v
		}
		score, err := cosineSimilarity(vector, c.vector)
		if err != nil {
			return nil, err
		}
		if score >= threshold {
			result = append(result, recalledFact{Fact: f, Similarity: score})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Similarity == result[j].Similarity {
			return result[i].ID < result[j].ID
		}
		return result[i].Similarity > result[j].Similarity
	})
	return result, nil
}
