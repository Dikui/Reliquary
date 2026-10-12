package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryCustomMetadataTextIsStableAndSeparate(t *testing.T) {
	entry := &Entry{
		Metadata:       JSON(`{"external_id":"secret-internal-id"}`),
		CustomMetadata: JSON(`{"region":"Shanghai","department":"R&D"}`),
	}
	require.Equal(t, "department: R&D\nregion: Shanghai", entry.CustomMetadataText())
	require.NotContains(t, entry.CustomMetadataText(), "external_id")
}
