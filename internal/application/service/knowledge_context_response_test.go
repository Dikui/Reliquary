package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestKnowledgeContextEntityDetailWithoutEventsPreservesFacts(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "object", TenantID: 9, CanonicalName: "Aurora", NormalizedName: "aurora", EntityType: "model"}).Error)
	p2ContextMessage(t, db, "source", "kb", "Aurora uses Apache-2.0.")
	require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: "evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "source", Version: 1, BodyHash: "hash", Body: "Aurora uses Apache-2.0.", Status: types.KnowledgeEvidenceCurrent}).Error)
	require.NoError(t, db.Create(&types.KnowledgeFact{ID: "fact", TenantID: 9, KnowledgeBaseID: "kb", SubjectEntityID: "object", Kind: "attribute", Predicate: "license", Value: types.JSON(`"Apache-2.0"`), ValueType: "text", EvidenceID: "evidence", Quote: "Aurora uses Apache-2.0.", Status: types.KnowledgeFactCurrent}).Error)

	result, err := svc.EntityDetail(ctx, "object")
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	require.Len(t, result.Evidence, 1)
	require.NotNil(t, result.Events)
	require.Empty(t, result.Events)
}
