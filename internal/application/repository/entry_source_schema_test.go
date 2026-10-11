package repository

import (
	"strings"
	"testing"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestEntrySourceSchemaAllowsObjectStorageURLs(t *testing.T) {
	db := pgtest.OpenFixture(t)
	if err := db.AutoMigrate(&types.Entry{}); err != nil {
		t.Fatalf("auto migrate entry: %v", err)
	}

	var column struct {
		DataType               string
		CharacterMaximumLength int
	}
	if err := db.Raw(`
		SELECT data_type, character_maximum_length
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'entries' AND column_name = 'source'
	`).Scan(&column).Error; err != nil {
		t.Fatalf("inspect entry schema: %v", err)
	}
	if column.DataType != "character varying" || column.CharacterMaximumLength != 2048 {
		t.Fatalf("entry source column type = %q(%d), want varchar(2048)", column.DataType, column.CharacterMaximumLength)
	}

	longURL := "https://example-bucket.cos.ap-beijing.myqcloud.com/test/" +
		strings.Repeat("encoded-path-segment-", 20) + ".docx"
	entry := &types.Entry{
		SpaceID:         1,
		KnowledgeBaseID: "kb-1",
		Type:            types.EntryTypeManual,
		Title:           "long source",
		Source:          longURL,
		ParseStatus:     types.ParseStatusPending,
		EnableStatus:    "enabled",
	}
	if err := db.Create(entry).Error; err != nil {
		t.Fatalf("create entry with long source: %v", err)
	}
}
