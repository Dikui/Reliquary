package repository

import (
	"strings"
	"testing"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestKnowledgeSourceSchemaAllowsObjectStorageURLs(t *testing.T) {
	db := pgtest.OpenFixture(t)
	if err := db.AutoMigrate(&types.Knowledge{}); err != nil {
		t.Fatalf("auto migrate knowledge: %v", err)
	}

	var column struct {
		DataType               string
		CharacterMaximumLength int
	}
	if err := db.Raw(`
		SELECT data_type, character_maximum_length
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'knowledges' AND column_name = 'source'
	`).Scan(&column).Error; err != nil {
		t.Fatalf("inspect knowledge schema: %v", err)
	}
	if column.DataType != "character varying" || column.CharacterMaximumLength != 2048 {
		t.Fatalf("knowledge source column type = %q(%d), want varchar(2048)", column.DataType, column.CharacterMaximumLength)
	}

	longURL := "https://example-bucket.cos.ap-beijing.myqcloud.com/test/" +
		strings.Repeat("encoded-path-segment-", 20) + ".docx"
	knowledge := &types.Knowledge{
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Type:            types.KnowledgeTypeManual,
		Title:           "long source",
		Source:          longURL,
		ParseStatus:     types.ParseStatusPending,
		EnableStatus:    "enabled",
	}
	if err := db.Create(knowledge).Error; err != nil {
		t.Fatalf("create knowledge with long source: %v", err)
	}
}
