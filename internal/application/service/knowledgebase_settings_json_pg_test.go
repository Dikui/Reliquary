package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"testing"
)

func TestKnowledgeSettingsSavePreservesUneditedStoredJSON(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	require.NoError(t, db.Exec(`INSERT INTO models(id,space_id,name,type,source,status,parameters) VALUES('gen',1,'Generation','KnowledgeQA','remote','active','{}'),('embed',1,'Embedding','Embedding','remote','active','{}'),('new-embed',1,'New embedding','Embedding','remote','active','{}'); INSERT INTO knowledge_bases(id,space_id,name,description,embedding_model_id,summary_model_id,chunking_config,model_tiers,indexing_strategy) VALUES('kb',1,'Original','Retained','embed','gen','{"chunk_size":512,"chunk_overlap":0,"separators":null,"languages":null,"parser_engine_rules":null,"extension":{"array":[],"null":null}}','{"generation":"gen","embedding":"embed","light":null,"extension":null}','null')`).Error)
	svc := &knowledgeBaseService{db: db, repo: repository.NewKnowledgeBaseRepository(db)}
	read := func() map[string]any {
		var value map[string]any
		require.NoError(t, db.Table("knowledge_bases").Select("chunking_config::text AS chunking_config, model_tiers::text AS model_tiers, indexing_strategy::text AS indexing_strategy").Where("id='kb'").Take(&value).Error)
		return value
	}
	original := read()
	_, err := svc.UpdateKnowledgeBase(ctx, "kb", "Renamed", "Retained", nil)
	require.NoError(t, err)
	require.Equal(t, original, read())
	// The page changes only the embedding selection, retaining the loaded
	// model-tier object rather than inserting defaults for its other fields.
	tiers := &types.ModelTierConfig{Generation: "gen", Embedding: "new-embed"}
	_, err = svc.UpdateKnowledgeBase(ctx, "kb", "Renamed", "Retained", &types.KnowledgeBaseConfig{ModelTiers: tiers})
	require.NoError(t, err)
	updated := read()
	require.Equal(t, original["chunking_config"], updated["chunking_config"])
	require.Equal(t, original["indexing_strategy"], updated["indexing_strategy"])
	require.JSONEq(t, `{"generation":"gen","embedding":"new-embed","light":null,"extension":null}`, updated["model_tiers"].(string))
	// A real chunk-size edit must not persist the separators used only as
	// display defaults by GET /knowledge-base.
	chunks := &types.ChunkingConfig{ChunkSize: 128, ChunkOverlap: 0, ChunkOverlapSet: true, Separators: []string{"\n\n", "\n", "。"}}
	_, err = svc.UpdateKnowledgeBase(ctx, "kb", "Renamed", "Retained", &types.KnowledgeBaseConfig{ChunkingConfig: chunks})
	require.NoError(t, err)
	updated = read()
	require.JSONEq(t, `{"chunk_size":128,"chunk_overlap":0,"separators":null,"languages":null,"parser_engine_rules":null,"extension":{"array":[],"null":null}}`, updated["chunking_config"].(string))

}
